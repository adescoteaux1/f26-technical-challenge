// Package api exposes the Oracle's three REST endpoints. It translates
// between the wire format described in the challenge spec and the internal
// evaluation/model types — no scheduling or simulation logic lives here.
package api

import (
	"time"

	"github.com/adescoteaux1/generate-oracle/internal/engine"
	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/store"
)

// registerRequest / loginRequest are both {email, nuid}: registration
// creates the account, login re-authenticates and rotates the token.
type registerRequest struct {
	Email string `json:"email"`
	NUID  string `json:"nuid"`
}

type loginRequest struct {
	Email string `json:"email"`
	NUID  string `json:"nuid"`
}

// authResponse is returned by both /register and /login: the token must be
// stored by the client and sent as `Authorization: Bearer <token>` on every
// other request.
type authResponse struct {
	UserID string `json:"userId"`
	Token  string `json:"token"`
}

func toAuthResponse(user *models.User) authResponse {
	return authResponse{UserID: user.ID, Token: user.Token}
}

// historyItem is one row of a user's evaluation history.
type historyItem struct {
	EvaluationID string         `json:"evaluationId"`
	Finished     bool           `json:"finished"`
	OverallScore float64        `json:"overallScore"`
	Metrics      models.Metrics `json:"metrics"`
	CreatedAt    time.Time      `json:"createdAt"`
}

func toHistoryItem(s store.EvaluationSummary) historyItem {
	return historyItem{
		EvaluationID: s.ID,
		Finished:     s.Finished,
		OverallScore: s.OverallScore,
		Metrics:      s.Metrics,
		CreatedAt:    s.CreatedAt,
	}
}

// createEvaluationResponse is returned by POST /evaluation.
type createEvaluationResponse struct {
	EvaluationID     string `json:"evaluationId"`
	Simulation       int    `json:"simulation"`
	TotalSimulations int    `json:"totalSimulations"`
}

// finishedResponse is returned once every simulation in the evaluation has
// completed, matching the spec's finished-evaluation shape exactly.
type finishedResponse struct {
	Finished     bool           `json:"finished"`
	OverallScore float64        `json:"overallScore"`
	Metrics      models.Metrics `json:"metrics"`
}

// rejectedAssignmentView surfaces why a submitted assignment was skipped,
// so applicants can debug their scheduler ("descriptive errors" per spec).
type rejectedAssignmentView struct {
	WorkerID int    `json:"workerId"`
	JobID    int    `json:"jobId"`
	Reason   string `json:"reason"`
}

// simulationStateResponse is returned by GET /evaluation/{id} and
// POST /simulation/{id}/schedule whenever the evaluation is still running.
// Jobs that have not yet arrived are omitted entirely (see models.Simulation.VisibleJobs).
type simulationStateResponse struct {
	Finished         bool                     `json:"finished"`
	EvaluationID     string                   `json:"evaluationId"`
	Simulation       int                      `json:"simulation"`
	TotalSimulations int                      `json:"totalSimulations"`
	Profile          string                   `json:"profile"`
	Tick             int                      `json:"tick"`
	MaxTicks         int                      `json:"maxTicks"`
	Workers          []*models.Worker         `json:"workers"`
	Jobs             []*models.Job            `json:"jobs"`
	Score            float64                  `json:"score"`
	Metrics          models.Metrics           `json:"metrics"`
	Rejected         []rejectedAssignmentView `json:"rejected,omitempty"`
}

func toSimulationStateResponse(totalSimulations int, sim *models.Simulation, rejected []engine.RejectedAssignment) simulationStateResponse {
	resp := simulationStateResponse{
		Finished:         false,
		EvaluationID:     sim.EvaluationID,
		Simulation:       sim.Number,
		TotalSimulations: totalSimulations,
		Profile:          sim.Profile,
		Tick:             sim.Tick,
		MaxTicks:         sim.MaxTicks,
		Workers:          sim.Workers,
		Jobs:             sim.VisibleJobs(),
		Score:            sim.Score,
		Metrics:          sim.Metrics,
	}
	for _, r := range rejected {
		resp.Rejected = append(resp.Rejected, rejectedAssignmentView{
			WorkerID: r.WorkerID, JobID: r.JobID, Reason: r.Reason,
		})
	}
	return resp
}

func toFinishedResponse(row *store.EvaluationRow) finishedResponse {
	return finishedResponse{
		Finished:     true,
		OverallScore: row.OverallScore,
		Metrics:      row.Metrics,
	}
}
