package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/adescoteaux1/generate-control-tower/internal/evaluation"
	"github.com/google/uuid"

	ghub "github.com/adescoteaux1/generate-control-tower/internal/github"
	"github.com/adescoteaux1/generate-control-tower/internal/portals"
	"github.com/adescoteaux1/generate-control-tower/internal/slots"
	"github.com/adescoteaux1/generate-control-tower/internal/store"
	"github.com/adescoteaux1/generate-control-tower/internal/userauth"
)

// applicantRepoProvisioner is the subset of *github.Client that POST /apply
// needs, so handler tests can supply a fake instead of hitting real GitHub.
type applicantRepoProvisioner interface {
	CreateApplicantRepo(ctx context.Context, username string) (string, error)
}

// Server holds the dependencies operation handlers need.
type Server struct {
	Store store.Store
	Log   *slog.Logger

	// GitHub is nil when GITHUB_TOKEN/GITHUB_ORG aren't configured, in which
	// case POST /apply reports itself unavailable rather than the whole
	// server failing to start over an optional feature.
	GitHub applicantRepoProvisioner
}

func (s *Server) registerHandler(ctx context.Context, input *registerInput) (*authOutput, error) {
	user, err := userauth.Register(ctx, s.Store, input.Body.Email, input.Body.NUID)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			return nil, huma.Error409Conflict("an account with that email already exists; use /login")
		}
		s.Log.Error("register failed", "error", err)
		return nil, huma.Error500InternalServerError("failed to register")
	}
	resp := &authOutput{}
	resp.Body = toAuthResponse(user)
	return resp, nil
}

func (s *Server) loginHandler(ctx context.Context, input *loginInput) (*authOutput, error) {
	user, err := userauth.Login(ctx, s.Store, input.Body.Email, input.Body.NUID)
	if err != nil {
		if errors.Is(err, userauth.ErrInvalidCredentials) {
			return nil, huma.Error401Unauthorized("email/nuid combination not found")
		}
		s.Log.Error("login failed", "error", err)
		return nil, huma.Error500InternalServerError("failed to log in")
	}
	resp := &authOutput{}
	resp.Body = toAuthResponse(user)
	return resp, nil
}

func (s *Server) historyHandler(ctx context.Context, _ *historyInput) (*historyOutput, error) {
	user := userFromContext(ctx)

	summaries, err := s.Store.ListExpeditionsForUser(ctx, user.ID)
	if err != nil {
		s.Log.Error("list history failed", "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}

	resp := &historyOutput{Body: make([]historyItem, 0, len(summaries))}
	for _, sum := range summaries {
		resp.Body = append(resp.Body, toHistoryItem(sum))
	}
	return resp, nil
}

func (s *Server) createExpeditionHandler(ctx context.Context, _ *createExpeditionInput) (*createExpeditionOutput, error) {
	user := userFromContext(ctx)

	exp, err := evaluation.Create(ctx, s.Store, user.ID)
	if err != nil {
		s.Log.Error("create expedition failed", "error", err)
		return nil, huma.Error500InternalServerError("failed to create expedition")
	}
	resp := &createExpeditionOutput{}
	resp.Body = createExpeditionResponse{
		ExpeditionID: exp.ID,
		Cycle:        exp.CurrentCycle,
		TotalCycles:  exp.TotalCycles,
	}
	return resp, nil
}

func (s *Server) getExpeditionHandler(ctx context.Context, input *getExpeditionInput) (*expeditionStateOutput, error) {
	user := userFromContext(ctx)

	row, cycle, err := evaluation.GetState(ctx, s.Store, input.ID, user.ID)
	if err != nil {
		return nil, s.lookupError(err)
	}

	resp := &expeditionStateOutput{}
	if row.Finished {
		resp.Body = toFinishedResponse(row)
	} else {
		resp.Body = toCycleStateResponse(row.TotalCycles, cycle, nil)
	}
	return resp, nil
}

func (s *Server) submitCycleHandler(ctx context.Context, input *scheduleInput) (*expeditionStateOutput, error) {
	user := userFromContext(ctx)

	result, err := evaluation.Submit(ctx, s.Store, input.ID, user.ID, input.Body)
	if err != nil {
		return nil, s.lookupError(err)
	}

	resp := &expeditionStateOutput{}
	if result.Cycle == nil {
		resp.Body = toFinishedResponse(result.Expedition)
	} else {
		resp.Body = toCycleStateResponse(result.Expedition.TotalCycles, result.Cycle, result.Rejected)
	}
	return resp, nil
}

// chaosProbeHandler simulates a flaky network dependency, deterministically
// and without any server-side session state, so a scheduler client can
// write a real, reproducible test for its own retry/timeout/backoff logic
// instead of just hoping it works. See dto.go's chaosProbeInput for the
// available modes.
func (s *Server) chaosProbeHandler(ctx context.Context, input *chaosProbeInput) (*chaosProbeOutput, error) {
	switch input.Mode {
	case "error":
		return nil, huma.Error503ServiceUnavailable("simulated transient failure — retry me")

	case "timeout":
		delay := time.Duration(input.DelayMs) * time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp := &chaosProbeOutput{}
		resp.Body = chaosProbeResponse{
			Attempt: input.Attempt, Outcome: "success",
			Message: fmt.Sprintf("responded after a %dms delay", input.DelayMs),
		}
		return resp, nil

	case "flaky":
		if input.Attempt < input.FailUntil {
			return nil, huma.Error503ServiceUnavailable(fmt.Sprintf(
				"simulated failure on attempt %d (will succeed once attempt >= %d)", input.Attempt, input.FailUntil))
		}
		resp := &chaosProbeOutput{}
		resp.Body = chaosProbeResponse{
			Attempt: input.Attempt, Outcome: "success",
			Message: fmt.Sprintf("succeeded on attempt %d", input.Attempt),
		}
		return resp, nil

	default:
		resp := &chaosProbeOutput{}
		resp.Body = chaosProbeResponse{Attempt: input.Attempt, Outcome: "success", Message: "ok"}
		return resp, nil
	}
}

// applyHandler creates a private repo under the org for input.Body.GitHubUsername
// and adds them as a push collaborator, so applicants don't need to create
// and share their own repo — see internal/github for the actual GitHub calls.
func (s *Server) applyHandler(ctx context.Context, input *applyInput) (*applyOutput, error) {
	if s.GitHub == nil {
		return nil, huma.Error503ServiceUnavailable("repo creation isn't configured on this server yet")
	}

	username := strings.TrimSpace(input.Body.GitHubUsername)
	repoURL, err := s.GitHub.CreateApplicantRepo(ctx, username)
	if err != nil {
		switch {
		case errors.Is(err, ghub.ErrInvalidUsername):
			return nil, huma.Error422UnprocessableEntity("that doesn't look like a valid GitHub username")
		case errors.Is(err, ghub.ErrUserNotFound):
			return nil, huma.Error404NotFound("no GitHub user with that username exists")
		default:
			s.Log.Error("create applicant repo failed", "error", err, "username", username)
			return nil, huma.Error500InternalServerError("failed to create your repo — contact the team")
		}
	}

	resp := &applyOutput{}
	resp.Body = applyResponse{RepoURL: repoURL}
	return resp, nil
}

func (s *Server) portalStatusHandler(_ context.Context, _ *portalStatusInput) (*portalStatusOutput, error) {
	snapshot := portals.Snapshot()

	resp := &portalStatusOutput{Body: make([]portalStatusItem, 0, len(snapshot))}
	for _, p := range snapshot {
		resp.Body = append(resp.Body, toPortalStatusItem(p))
	}
	return resp, nil
}

func (s *Server) bookingsHandler(ctx context.Context, input *bookingsInput) (*bookingsOutput, error) {
	var cursor *store.BookingCursor
	if input.Cursor != "" {
		parsed, err := decodeBookingCursor(input.Cursor)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid cursor — pass back a nextCursor verbatim", err)
		}
		cursor = parsed
	}

	// The extra row is never returned: its presence is what proves another
	// batch follows, without a second query against the keyset window.
	withLookahead, totalOnFile, err := s.Store.ListBookings(ctx, cursor, input.Limit+1)
	if err != nil {
		s.Log.Error("list bookings failed", "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}

	hasMore := len(withLookahead) > input.Limit
	batch := withLookahead
	if hasMore {
		batch = withLookahead[:input.Limit]
	}

	page := bookingsPage{
		Items:   make([]bookingItem, 0, len(batch)),
		HasMore: hasMore,
		Total:   totalOnFile,
	}
	for _, booking := range batch {
		page.Items = append(page.Items, toBookingItem(booking))
	}
	if hasMore {
		page.NextCursor = encodeBookingCursor(batch[len(batch)-1])
	}

	resp := &bookingsOutput{}
	resp.Body = page
	return resp, nil
}

func (s *Server) transitSlotsHandler(_ context.Context, _ *transitSlotsInput) (*transitSlotsOutput, error) {
	available := slots.Available()

	resp := &transitSlotsOutput{Body: make([]transitSlotItem, 0, len(available))}
	for _, slot := range available {
		resp.Body = append(resp.Body, toTransitSlotItem(slot))
	}
	return resp, nil
}

func (s *Server) submitBookingHandler(_ context.Context, input *submitBookingInput) (*submitBookingOutput, error) {
	slot, found := slots.Find(input.SlotID)
	if !found {
		return nil, huma.Error404NotFound("no such slot — re-read GET /frontend/slots")
	}

	outcome := slots.OutcomeInsufficientSeats
	if input.Body.TravelerCount <= slot.SeatsAvailable {
		outcome = slots.SimulateOutcome()
	}

	resp := &submitBookingOutput{Status: statusForOutcome(outcome)}
	resp.Body = submitBookingResponse{
		Status:    string(outcome),
		SlotID:    slot.ID,
		Detail:    outcome.Detail(),
		Retryable: outcome.Retryable(),
	}
	if outcome == slots.OutcomeConfirmed {
		confirmedAt := time.Now().UTC()
		resp.Body.Reference = newBookingReference()
		resp.Body.TravelerCount = input.Body.TravelerCount
		resp.Body.TotalCredits = slot.FareCredits * input.Body.TravelerCount
		resp.Body.ConfirmedAt = &confirmedAt
	}
	return resp, nil
}

// statusForOutcome keeps HTTP honest: a slot being taken is an answer to a
// well-formed request, but a corridor failure means the booking never
// completed, and reporting that as 200 would hide it from every client,
// proxy and dashboard above the JSON body.
func statusForOutcome(outcome slots.Outcome) int {
	if outcome == slots.OutcomeCorridorUnstable {
		return http.StatusServiceUnavailable
	}
	return http.StatusOK
}

func newBookingReference() string {
	return "BK-" + strings.ToUpper(uuid.NewString()[:6])
}

func (s *Server) lookupError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return huma.Error404NotFound("expedition not found")
	}
	if errors.Is(err, evaluation.ErrForbidden) {
		return huma.Error403Forbidden("you do not own this expedition")
	}
	s.Log.Error("request failed", "error", err)
	return huma.Error500InternalServerError("internal error")
}
