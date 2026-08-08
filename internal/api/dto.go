// Package api exposes the Control Tower's endpoints via Huma, which generates an
// OpenAPI 3.1 spec (and Scalar docs UI) directly from the Input/Output
// types and struct tags below — no hand-written spec to keep in sync.
package api

import (
	"time"

	"github.com/adescoteaux1/generate-control-tower/internal/engine"
	"github.com/adescoteaux1/generate-control-tower/internal/models"
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
