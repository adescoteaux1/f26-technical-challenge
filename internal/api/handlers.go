package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/adescoteaux1/generate-oracle/internal/evaluation"
	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/store"
	"github.com/adescoteaux1/generate-oracle/internal/userauth"
)

// Server holds the dependencies HTTP handlers need.
type Server struct {
	Store store.Store
	Log   *slog.Logger
}

// register handles POST /register.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.NUID == "" {
		writeError(w, http.StatusBadRequest, "request body must include non-empty \"email\" and \"nuid\"")
		return
	}

	user, err := userauth.Register(r.Context(), s.Store, req.Email, req.NUID)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "an account with that email already exists; use /login")
			return
		}
		s.Log.Error("register failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to register")
		return
	}
	writeJSON(w, http.StatusCreated, toAuthResponse(user))
}

// login handles POST /login.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.NUID == "" {
		writeError(w, http.StatusBadRequest, "request body must include non-empty \"email\" and \"nuid\"")
		return
	}

	user, err := userauth.Login(r.Context(), s.Store, req.Email, req.NUID)
	if err != nil {
		if errors.Is(err, userauth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "email/nuid combination not found")
			return
		}
		s.Log.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to log in")
		return
	}
	writeJSON(w, http.StatusOK, toAuthResponse(user))
}

// history handles GET /me/evaluations.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	summaries, err := s.Store.ListEvaluationsForUser(r.Context(), user.ID)
	if err != nil {
		s.Log.Error("list history failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]historyItem, 0, len(summaries))
	for _, sum := range summaries {
		items = append(items, toHistoryItem(sum))
	}
	writeJSON(w, http.StatusOK, items)
}

// createEvaluation handles POST /evaluation.
func (s *Server) createEvaluation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)

	eval, err := evaluation.Create(r.Context(), s.Store, user.ID)
	if err != nil {
		s.Log.Error("create evaluation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create evaluation")
		return
	}
	writeJSON(w, http.StatusCreated, createEvaluationResponse{
		EvaluationID:     eval.ID,
		Simulation:       eval.CurrentSimulation,
		TotalSimulations: eval.TotalSimulations,
	})
}

// getEvaluation handles GET /evaluation/{id}.
func (s *Server) getEvaluation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user := userFromContext(r)

	row, sim, err := evaluation.GetState(r.Context(), s.Store, id, user.ID)
	if err != nil {
		s.handleLookupError(w, err)
		return
	}
	if row.Finished {
		writeJSON(w, http.StatusOK, toFinishedResponse(row))
		return
	}
	writeJSON(w, http.StatusOK, toSimulationStateResponse(row.TotalSimulations, sim, nil))
}

// submitSchedule handles POST /simulation/{id}/schedule.
func (s *Server) submitSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user := userFromContext(r)

	var assignments []models.Assignment
	if err := json.NewDecoder(r.Body).Decode(&assignments); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON array of {workerId, jobId} assignments")
		return
	}

	result, err := evaluation.Submit(r.Context(), s.Store, id, user.ID, assignments)
	if err != nil {
		s.handleLookupError(w, err)
		return
	}

	if result.Simulation == nil {
		writeJSON(w, http.StatusOK, toFinishedResponse(result.Evaluation))
		return
	}
	writeJSON(w, http.StatusOK, toSimulationStateResponse(result.Evaluation.TotalSimulations, result.Simulation, result.Rejected))
}

func (s *Server) handleLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "evaluation not found")
		return
	}
	if errors.Is(err, evaluation.ErrForbidden) {
		writeError(w, http.StatusForbidden, "you do not own this evaluation")
		return
	}
	s.Log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
