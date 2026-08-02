// Package api exposes the Oracle's endpoints via Huma, which generates an
// OpenAPI 3.1 spec (and Scalar docs UI) directly from the Input/Output
// types and struct tags below — no hand-written spec to keep in sync.
package api

import (
	"time"

	"github.com/adescoteaux1/generate-oracle/internal/engine"
	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/store"
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
	ExpeditionID string         `json:"expeditionId"`
	Finished     bool           `json:"finished"`
	OverallScore float64        `json:"overallScore"`
	Metrics      models.Metrics `json:"metrics"`
	CreatedAt    time.Time      `json:"createdAt"`
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
	ExpeditionID string `json:"expeditionId"`
	Cycle        int    `json:"cycle"`
	TotalCycles  int    `json:"totalCycles"`
}

// rejectedVoyageView surfaces why a submitted assignment was skipped,
// so applicants can debug their scheduler ("descriptive errors" per spec).
type rejectedVoyageView struct {
	GateID   int    `json:"gateId"`
	VoyageID int    `json:"voyageId"`
	Reason   string `json:"reason"`
}

// expeditionStateResponse is returned by GET /expedition/{id} and
// POST /cycle/{id}/schedule. While the expedition is still running most
// fields are populated and `overallScore` is omitted; once every cycle has
// finished, only `finished`, `overallScore`, and `metrics` are populated.
// Voyages that have not yet been requested are omitted entirely (see
// models.Cycle.VisibleVoyages).
type expeditionStateResponse struct {
	Finished     bool                 `json:"finished"`
	ExpeditionID string               `json:"expeditionId,omitempty"`
	Cycle        int                  `json:"cycle,omitempty"`
	TotalCycles  int                  `json:"totalCycles,omitempty"`
	Profile      string               `json:"profile,omitempty"`
	Tick         int                  `json:"tick,omitempty"`
	MaxTicks     int                  `json:"maxTicks,omitempty"`
	Gates        []*models.Gate       `json:"gates,omitempty"`
	Voyages      []*models.Voyage     `json:"voyages,omitempty"`
	Score        float64              `json:"score,omitempty"`
	Metrics      models.Metrics       `json:"metrics"`
	Rejected     []rejectedVoyageView `json:"rejected,omitempty"`
	OverallScore float64              `json:"overallScore,omitempty"`
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
