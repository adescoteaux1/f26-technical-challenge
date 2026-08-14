// Package api exposes the Control Tower's endpoints via Huma, which generates an
// OpenAPI 3.1 spec (and Scalar docs UI) directly from the Input/Output
// types and struct tags below — no hand-written spec to keep in sync.
package api

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/adescoteaux1/generate-control-tower/internal/engine"
	"github.com/adescoteaux1/generate-control-tower/internal/models"
	"github.com/adescoteaux1/generate-control-tower/internal/portals"
	"github.com/adescoteaux1/generate-control-tower/internal/slots"
	"github.com/adescoteaux1/generate-control-tower/internal/store"
)

// --- Request/response bodies ---

// registerRequest / loginRequest are both {email, nuid}: registration
// creates the account, login re-authenticates and rotates the token.
type registerRequest struct {
	Email string `json:"email" format:"email" minLength:"1" doc:"Registrant's email address"`
	NUID  string `json:"nuid" minLength:"1" doc:"Northeastern University ID"`
}

type loginRequest struct {
	Email string `json:"email" format:"email" minLength:"1" doc:"Registrant's email address"`
	NUID  string `json:"nuid" minLength:"1" doc:"Northeastern University ID"`
}

// authResponse is returned by both /register and /login: the token must be
// stored by the client and sent as `Authorization: Bearer <token>` on every
// other request.
type authResponse struct {
	UserID string `json:"userId" doc:"Your account ID"`
	Token  string `json:"token" doc:"Bearer token — send as 'Authorization: Bearer <token>' on every other request"`
}

func toAuthResponse(user *models.User) authResponse {
	return authResponse{UserID: user.ID, Token: user.Token}
}

// historyItem is one row of a user's expedition history.
type historyItem struct {
	ExpeditionID string         `json:"expeditionId" doc:"Expedition ID, usable with GET /expedition/{id}"`
	Finished     bool           `json:"finished" doc:"Whether all 16 cycles have completed"`
	OverallScore float64        `json:"overallScore" doc:"Average overall score across all 16 cycles; 0 until finished is true"`
	Metrics      models.Metrics `json:"metrics" doc:"Average of each metric across all 16 cycles; zero-valued until finished is true"`
	CreatedAt    time.Time      `json:"createdAt" doc:"When this expedition was started"`
}

func toHistoryItem(s store.ExpeditionSummary) historyItem {
	return historyItem{
		ExpeditionID: s.ID,
		Finished:     s.Finished,
		OverallScore: s.OverallScore,
		Metrics:      s.Metrics,
		CreatedAt:    s.CreatedAt,
	}
}

// createExpeditionResponse is returned by POST /expedition.
type createExpeditionResponse struct {
	ExpeditionID string `json:"expeditionId" doc:"ID for this expedition; use it in GET /expedition/{id} and POST /cycle/{id}/schedule for its whole 16-cycle run"`
	Cycle        int    `json:"cycle" doc:"Current cycle number, 1-indexed"`
	TotalCycles  int    `json:"totalCycles" doc:"Total number of cycles in this expedition (16)"`
}

// rejectedVoyageView surfaces why a submitted assignment was skipped,
// so applicants can debug their scheduler ("descriptive errors" per spec).
type rejectedVoyageView struct {
	GateID   int    `json:"gateId" doc:"Gate ID from the rejected assignment"`
	VoyageID int    `json:"voyageId" doc:"Voyage ID from the rejected assignment"`
	Reason   string `json:"reason" doc:"Human-readable reason this specific assignment was not applied"`
}

// expeditionStateResponse is returned by GET /expedition/{id} and
// POST /cycle/{id}/schedule. While the expedition is still running most
// fields are populated and `overallScore` is omitted; once every cycle has
// finished, only `finished`, `overallScore`, and `metrics` are populated.
// Voyages that have not yet been requested are omitted entirely (see
// models.Cycle.VisibleVoyages).
type expeditionStateResponse struct {
	Finished     bool                 `json:"finished" doc:"True once all 16 cycles are complete; when true, only finished/overallScore/metrics are populated"`
	ExpeditionID string               `json:"expeditionId,omitempty" doc:"Expedition ID (omitted once finished)"`
	Cycle        int                  `json:"cycle,omitempty" doc:"Current cycle number, 1-indexed (omitted once finished)"`
	TotalCycles  int                  `json:"totalCycles,omitempty" doc:"Total number of cycles in this expedition (omitted once finished)"`
	Profile      string               `json:"profile,omitempty" doc:"Current cycle's workload profile, e.g. narrow_window (omitted once finished)"`
	Tick         int                  `json:"tick" doc:"Current tick within the cycle"`
	MaxTicks     int                  `json:"maxTicks,omitempty" doc:"Tick at which the current cycle force-finishes regardless of remaining voyages (omitted once finished)"`
	Gates        []*models.Gate       `json:"gates,omitempty" doc:"Every gate in the current cycle (omitted once finished)"`
	Voyages      []*models.Voyage     `json:"voyages,omitempty" doc:"Voyages requested so far in the current cycle; voyages not yet requested are omitted entirely (omitted once finished)"`
	PremiumHubs  []string             `json:"premiumHubs,omitempty" doc:"Origin hubs with a tighter SLA this cycle (omitted once finished)"`
	Score        float64              `json:"score,omitempty" doc:"Current cycle's live score (omitted once finished)"`
	Metrics      models.Metrics       `json:"metrics" doc:"Current cycle's live metrics, or the expedition-wide average once finished"`
	Rejected     []rejectedVoyageView `json:"rejected,omitempty" doc:"Assignments from the last schedule submission that were not applied, and why"`
	OverallScore float64              `json:"overallScore,omitempty" doc:"Average overall score across all 16 cycles; only populated once finished"`
}

func toCycleStateResponse(totalCycles int, cycle *models.Cycle, rejected []engine.RejectedAssignment) expeditionStateResponse {
	resp := expeditionStateResponse{
		Finished:     false,
		ExpeditionID: cycle.ExpeditionID,
		Cycle:        cycle.Number,
		TotalCycles:  totalCycles,
		Profile:      cycle.Profile,
		Tick:         cycle.Tick,
		MaxTicks:     cycle.MaxTicks,
		Gates:        cycle.Gates,
		Voyages:      cycle.VisibleVoyages(),
		PremiumHubs:  cycle.PremiumHubs,
		Score:        cycle.Score,
		Metrics:      cycle.Metrics,
	}
	for _, r := range rejected {
		resp.Rejected = append(resp.Rejected, rejectedVoyageView{
			GateID: r.GateID, VoyageID: r.VoyageID, Reason: r.Reason,
		})
	}
	return resp
}

func toFinishedResponse(row *store.ExpeditionRow) expeditionStateResponse {
	return expeditionStateResponse{
		Finished:     true,
		OverallScore: row.OverallScore,
		Metrics:      row.Metrics,
	}
}

// applyRequest / applyResponse back POST /apply: an applicant gives their
// name and GitHub username and gets a private repo under the org, titled
// with their name (plus their username, to guarantee uniqueness even if two
// applicants share a name), with push access already granted.
type applyRequest struct {
	GitHubUsername string `json:"githubUsername" minLength:"1" doc:"Your GitHub username (not email or display name)"`
	FirstName      string `json:"firstName" minLength:"1" doc:"Your first name, used to title your repo"`
	LastName       string `json:"lastName" minLength:"1" doc:"Your last name, used to title your repo"`
}

type applyResponse struct {
	RepoURL string `json:"repoUrl" doc:"URL of your challenge repo; you now have push access to it"`
}

// portalStatusItem is one row of the console's Portal Network Status panel.
type portalStatusItem struct {
	Name   string `json:"name" doc:"Portal display name"`
	Status string `json:"status" enum:"nominal,unstable,offline" doc:"offline when the portal's containment field is down, unstable at 80 load or above, nominal otherwise"`
	Load   int    `json:"load" minimum:"0" maximum:"100" doc:"Current load percentage. Always 0 when offline, but a nominal portal with nothing going through it also reports 0"`
}

func toPortalStatusItem(p portals.Portal) portalStatusItem {
	return portalStatusItem{
		Name:   p.Name,
		Status: p.Status,
		Load:   p.Load,
	}
}

// bookingItem is one card in the console's "My Upcoming Transits" panel.
type bookingItem struct {
	Reference    string    `json:"reference" doc:"Booking reference, e.g. BK-4417"`
	DepartsAt    time.Time `json:"departsAt" doc:"Scheduled departure time"`
	Destination  string    `json:"destination" doc:"Destination designation, e.g. Alpha-7"`
	Portal       string    `json:"portal" doc:"Portal this transit routes through; unrelated to GET /frontend/portals"`
	Load         *int      `json:"load" doc:"Corridor load percentage recorded for this booking; null when the corridor was offline"`
	Status       string    `json:"status" enum:"cleared,queued,held,canceled" doc:"Stored reservation status"`
	StatusDetail string    `json:"statusDetail,omitempty" doc:"Secondary reason line, e.g. Corridor Unstable; omitted when there is none"`
}

func toBookingItem(b models.Booking) bookingItem {
	return bookingItem{
		Reference:    b.Reference,
		DepartsAt:    b.DepartsAt,
		Destination:  b.Destination,
		Portal:       b.Portal,
		Load:         b.LoadPercent,
		Status:       string(b.Status),
		StatusDetail: b.StatusDetail,
	}
}

// transitSlotItem is one bookable departure. The fields are intentionally
// plain facts about a slot, not a prescribed layout — how a console groups,
// filters or sequences them is the challenge.
type transitSlotItem struct {
	ID              string    `json:"id" doc:"Slot ID, submit this to book it"`
	Destination     string    `json:"destination" doc:"Destination designation, e.g. Alpha-7"`
	Portal          string    `json:"portal" doc:"Origin portal this departure leaves from"`
	DepartsAt       time.Time `json:"departsAt" doc:"Scheduled departure time"`
	DurationMinutes int       `json:"durationMinutes" doc:"Transit duration in minutes"`
	SeatsAvailable  int       `json:"seatsAvailable" doc:"Seats still open on this departure"`
	FareCredits     int       `json:"fareCredits" doc:"Fare per traveler, in credits"`
}

func toTransitSlotItem(s slots.Slot) transitSlotItem {
	return transitSlotItem{
		ID:              s.ID,
		Destination:     s.Destination,
		Portal:          s.Portal,
		DepartsAt:       s.DepartsAt,
		DurationMinutes: s.DurationMinutes,
		SeatsAvailable:  s.SeatsAvailable,
		FareCredits:     s.FareCredits,
	}
}

// submitBookingRequest carries whatever a console collected. Only the traveler
// count is structurally required; Metadata is a free-form bag so a form can
// gather extra fields without this endpoint dictating what they are.
type submitBookingRequest struct {
	TravelerName  string            `json:"travelerName,omitempty" maxLength:"120" doc:"Lead traveler's name"`
	TravelerCount int               `json:"travelerCount" minimum:"1" default:"1" doc:"Seats to reserve"`
	ContactEmail  string            `json:"contactEmail,omitempty" doc:"Where to send the confirmation"`
	Notes         string            `json:"notes,omitempty" maxLength:"500" doc:"Free-text requests"`
	Metadata      map[string]string `json:"metadata,omitempty" doc:"Any additional key/value pairs your booking form collects"`
}

// submitBookingResponse reports the outcome as data, not as an HTTP error: a
// slot being taken is an answer, not a broken request. The booking fields are
// populated only when status is confirmed.
type submitBookingResponse struct {
	Status        string     `json:"status" enum:"confirmed,slot_taken,corridor_unstable,insufficient_seats" doc:"Outcome of this submission"`
	SlotID        string     `json:"slotId" doc:"Slot this submission targeted"`
	Detail        string     `json:"detail" doc:"Human-readable explanation, safe to show a traveler"`
	Retryable     bool       `json:"retryable" doc:"True when resubmitting the identical request could succeed"`
	Reference     string     `json:"reference,omitempty" doc:"Confirmation reference; only when confirmed"`
	TravelerCount int        `json:"travelerCount,omitempty" doc:"Seats reserved; only when confirmed"`
	TotalCredits  int        `json:"totalCredits,omitempty" doc:"Fare per traveler times traveler count; only when confirmed"`
	ConfirmedAt   *time.Time `json:"confirmedAt,omitempty" doc:"When the booking was confirmed; null otherwise"`
}

type bookingsPage struct {
	Items      []bookingItem `json:"items" doc:"Bookings in this batch, soonest departure first"`
	NextCursor string        `json:"nextCursor,omitempty" doc:"Pass as ?cursor= to fetch the next batch; omitted on the last one"`
	HasMore    bool          `json:"hasMore" doc:"True when another batch follows this one"`
	Total      int           `json:"total" doc:"Total bookings on file, not just in this batch"`
}

// Cursors are base64 only to keep the encoding opaque to clients; the payload
// is just the ORDER BY tuple.
const bookingCursorSeparator = "|"

func encodeBookingCursor(booking models.Booking) string {
	tuple := booking.DepartsAt.UTC().Format(time.RFC3339Nano) + bookingCursorSeparator + booking.Reference
	return base64.RawURLEncoding.EncodeToString([]byte(tuple))
}

func decodeBookingCursor(encoded string) (*store.BookingCursor, error) {
	tuple, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("cursor is not valid base64: %w", err)
	}
	departure, reference, separated := strings.Cut(string(tuple), bookingCursorSeparator)
	if !separated || reference == "" {
		return nil, fmt.Errorf("cursor is missing its reference half")
	}
	departsAt, err := time.Parse(time.RFC3339Nano, departure)
	if err != nil {
		return nil, fmt.Errorf("cursor has an unparseable timestamp: %w", err)
	}
	return &store.BookingCursor{DepartsAt: departsAt, Reference: reference}, nil
}

// --- Huma operation Input/Output wrappers ---
//
// Huma generates the OpenAPI request/response schemas (and therefore the
// Scalar docs) directly from these types, so path parameters and bodies are
// declared here via struct tags rather than parsed by hand in handlers.go.

type registerInput struct {
	Body registerRequest
}

type loginInput struct {
	Body loginRequest
}

type authOutput struct {
	Body authResponse
}

type historyInput struct{}

type historyOutput struct {
	Body []historyItem
}

type createExpeditionInput struct{}

type createExpeditionOutput struct {
	Body createExpeditionResponse
}

type getExpeditionInput struct {
	ID string `path:"id" doc:"Expedition ID returned by POST /expedition"`
}

type scheduleInput struct {
	ID   string              `path:"id" doc:"Expedition ID returned by POST /expedition"`
	Body []models.Assignment `doc:"Gate/voyage assignments to apply this tick. An empty array is valid and just advances the clock."`
}

type expeditionStateOutput struct {
	Body expeditionStateResponse
}

// chaosProbeInput controls a deterministic, stateless failure simulation —
// see handlers.go's chaosProbeHandler. Every field has a sane default so
// GET /chaos/probe with no query params at all just succeeds.
type chaosProbeInput struct {
	Mode      string `query:"mode" enum:"success,error,timeout,flaky" default:"success" doc:"Which failure mode to simulate"`
	Attempt   int    `query:"attempt" default:"1" doc:"Which retry attempt this is; you increment it yourself across retries. Only used by mode=flaky."`
	FailUntil int    `query:"failUntil" default:"3" doc:"For mode=flaky: fails while attempt < failUntil, succeeds once attempt >= failUntil."`
	DelayMs   int    `query:"delayMs" default:"3000" doc:"For mode=timeout: how long to delay the response, in milliseconds."`
}

type chaosProbeResponse struct {
	Attempt int    `json:"attempt" doc:"Echoes the attempt query param that was sent"`
	Outcome string `json:"outcome" doc:"Always 'success' when this response is returned; failures come back as an HTTP error instead"`
	Message string `json:"message" doc:"Human-readable detail about why this attempt succeeded"`
}

type chaosProbeOutput struct {
	Body chaosProbeResponse
}

type applyInput struct {
	Body applyRequest
}

type applyOutput struct {
	Body applyResponse
}

type portalStatusInput struct{}

type portalStatusOutput struct {
	Body []portalStatusItem
}

// bookingsInput's maximum on Limit is what forces clients to paginate: there
// are far more bookings on file than any single request can return, and Huma
// rejects an over-sized limit with a 422 rather than clamping it.
type bookingsInput struct {
	Cursor string `query:"cursor" doc:"nextCursor from the previous response; omit to start at the first batch"`
	Limit  int    `query:"limit" default:"10" minimum:"1" maximum:"10" doc:"Bookings per batch; 10 is the hard maximum"`
}

type bookingsOutput struct {
	Body bookingsPage
}

type transitSlotsInput struct{}

type transitSlotsOutput struct {
	Body []transitSlotItem
}

type submitBookingInput struct {
	SlotID string `path:"slotId" doc:"Slot ID from GET /frontend/slots"`
	Body   submitBookingRequest
}

// Status lets one handler answer with different codes: outcomes that are real
// answers come back 200, while a corridor failure reports 503 because the
// operation genuinely did not complete. The body is identical either way, so a
// client can still branch on Body.Status alone.
type submitBookingOutput struct {
	Status int
	Body   submitBookingResponse
}
