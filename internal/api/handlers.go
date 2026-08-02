package api

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"github.com/adescoteaux1/generate-oracle/internal/evaluation"
	"github.com/adescoteaux1/generate-oracle/internal/store"
	"github.com/adescoteaux1/generate-oracle/internal/userauth"
)

// Server holds the dependencies operation handlers need.
type Server struct {
	Store store.Store
	Log   *slog.Logger
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
