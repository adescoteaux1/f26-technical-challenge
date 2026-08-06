// Package models defines the core domain types shared by the generator,
// simulation engine, validator, and scoring engine.
package models

import "time"

// VoyageStatus tracks where a voyage is in its lifecycle.
type VoyageStatus string

const (
	VoyageAwaitingTransfer VoyageStatus = "awaiting_transfer" // waiting on prerequisites
	VoyageBoarding         VoyageStatus = "boarding"          // prerequisites satisfied, unassigned
	VoyageInTransit        VoyageStatus = "in_transit"        // assigned to a gate, executing
	VoyageArrived          VoyageStatus = "arrived"
)

// VoyageLeg is one hop of a multi-hop corridor voyage. A voyage with no
// Legs is a normal single-hop trip; a voyage with N legs must complete them
// in order, through separate gate assignments, before it counts as arrived.
type VoyageLeg struct {
	RequiredPower       int `json:"requiredPower"`
	RequiredContainment int `json:"requiredContainment"`
	EstimatedDuration   int `json:"estimatedDuration"`
}

// Voyage is a unit of travel requested by a traveler.
type Voyage struct {
	ID                  int    `json:"id"`
	OriginHub           string `json:"originHub"`
	Priority            int    `json:"priority"`          // 1 (low) - 5 (critical)
	EstimatedDuration   int    `json:"estimatedDuration"` // ticks required while in transit (current leg, if multi-hop)
	RequiredPower       int    `json:"requiredPower"`
	RequiredContainment int    `json:"requiredContainment"`
	ArrivalDeadline     int    `json:"arrivalDeadline"` // tick by which the voyage should arrive (whole trip)
	Prerequisites       []int  `json:"prerequisites"`
	RequestedTick       int    `json:"requestedTick"` // tick at which the voyage becomes visible

	// Legs is the full planned corridor for a multi-hop voyage (empty for a
	// normal single-hop voyage). LegIndex is which leg is current: the
	// top-level RequiredPower/RequiredContainment/EstimatedDuration/
	// RemainingDuration always describe *that* leg, so the rest of the
	// engine (validator, resource accounting, gate-outage requeueing) needs
	// no special multi-hop handling — it only ever sees "the current leg."
	Legs     []VoyageLeg `json:"legs,omitempty"`
	LegIndex int         `json:"legIndex,omitempty"`

	// SLADeadline is non-nil only for voyages whose OriginHub is one of the
	// cycle's PremiumHubs: a tighter internal deadline than ArrivalDeadline,
	// used only for the slaCompliance metric (missing it doesn't reject
	// scheduling, unlike ArrivalDeadline which is purely informational too).
	SLADeadline *int `json:"slaDeadline,omitempty"`

	Status            VoyageStatus `json:"status"`
	RemainingDuration int          `json:"remainingDuration"`
	AssignedGate      *int         `json:"assignedGate,omitempty"`
	BoardingTick      *int         `json:"boardingTick,omitempty"` // tick prerequisites were satisfied
	DepartureTick     *int         `json:"departureTick,omitempty"`
	ArrivalTick       *int         `json:"arrivalTick,omitempty"`
}

// Gate routes voyages and has finite power/containment capacity.
type Gate struct {
	ID                   int   `json:"id"`
	TotalPower           int   `json:"totalPower"`
	TotalContainment     int   `json:"totalContainment"`
	AvailablePower       int   `json:"availablePower"`
	AvailableContainment int   `json:"availableContainment"`
	ActiveVoyages        []int `json:"activeVoyages"`
	Operational          bool  `json:"operational"`
	OfflineUntil         int   `json:"offlineUntil"` // tick at which the gate comes back online; internal bookkeeping
}

// Assignment is a single scheduling decision submitted by the client.
type Assignment struct {
	GateID   int `json:"gateId" doc:"ID of the gate to send the voyage through"`
	VoyageID int `json:"voyageId" doc:"ID of the voyage to assign"`
}

// Metrics is the set of category scores reported to the client.
type Metrics struct {
	Throughput      float64 `json:"throughput"`
	GateUtilization float64 `json:"gateUtilization"`
	ArrivalSuccess  float64 `json:"arrivalSuccess"`
	Fairness        float64 `json:"fairness"`
	Reliability     float64 `json:"reliability"`
	SLACompliance   float64 `json:"slaCompliance"`
}

// Cycle is one independently-scored workload within an expedition.
//
// This struct is the full internal representation persisted to storage
// between HTTP requests (the Oracle is stateless per-request). The public
// API response shape is a separate DTO built by the api package, which
// exposes only the fields the spec calls for (and hides voyages that haven't
// been requested yet).
type Cycle struct {
	ExpeditionID string    `json:"expeditionId"`
	Number       int       `json:"cycle"`
	Profile      string    `json:"profile"`
	Seed         int64     `json:"seed"`
	Tick         int       `json:"tick"`
	MaxTicks     int       `json:"maxTicks"`
	Gates        []*Gate   `json:"gates"`
	Voyages      []*Voyage `json:"voyages"` // all voyages, including ones not yet requested
	Finished     bool      `json:"finished"`
	Score        float64   `json:"score"`
	Metrics      Metrics   `json:"metrics"`

	// PremiumHubs are the origin hubs paying for a tighter SLA this cycle
	// (see Voyage.SLADeadline and Metrics.SLACompliance).
	PremiumHubs []string `json:"premiumHubs,omitempty"`

	OutageRate     float64 `json:"outageRate"`
	OutageTicksMin int     `json:"outageTicksMin"`
	OutageTicksMax int     `json:"outageTicksMax"`

	// Stats accumulates running totals used by the scoring engine. It is
	// internal bookkeeping, not part of the public API response.
	Stats SimStats `json:"stats"`
}

// SimStats accumulates running totals used to compute metrics at any point in time.
type SimStats struct {
	TotalVoyages           int              `json:"totalVoyages"`
	ArrivedVoyages         int              `json:"arrivedVoyages"`
	ArrivalsOnTime         int              `json:"arrivalsOnTime"`
	ArrivalsLate           int              `json:"arrivalsLate"`
	TotalWaitTicks         int64            `json:"totalWaitTicks"` // queue wait time summed across arrived voyages
	GateBusyResourceTicks  int64            `json:"gateBusyResourceTicks"`
	GateTotalResourceTicks int64            `json:"gateTotalResourceTicks"`
	InvalidAssignments     int              `json:"invalidAssignments"`
	ValidAssignments       int              `json:"validAssignments"`
	OriginHubArrivals      map[string]int   `json:"originHubArrivals"`
	OriginHubWaitTicks     map[string]int64 `json:"originHubWaitTicks"`
	PremiumArrivalsOnTime  int              `json:"premiumArrivalsOnTime"`
	PremiumArrivalsLate    int              `json:"premiumArrivalsLate"`
}

// VisibleVoyages is the subset of a cycle's voyages the scheduler is allowed
// to see: voyages that have not yet been requested are omitted entirely from
// state responses.
func (c *Cycle) VisibleVoyages() []*Voyage {
	visible := make([]*Voyage, 0, len(c.Voyages))
	for _, v := range c.Voyages {
		if v.RequestedTick <= c.Tick {
			visible = append(visible, v)
		}
	}
	return visible
}

// Expedition groups multiple independent cycles sampled across workload profiles.
type Expedition struct {
	ID           string   `json:"expeditionId"`
	UserID       string   `json:"-"` // owner; never returned to the client directly
	TotalCycles  int      `json:"totalCycles"`
	CurrentCycle int      `json:"cycle"` // 1-indexed
	Cycles       []*Cycle `json:"-"`
	Finished     bool     `json:"finished"`
	OverallScore float64  `json:"overallScore"`
	Metrics      Metrics  `json:"metrics"`

	// ProfilePlan is the full sequence of workload profiles sampled at
	// creation time (see evaluation.sampleProfileOrder). It is internal
	// bookkeeping, never returned to the client.
	ProfilePlan []string `json:"-"`
}

// User is an applicant who has registered to run expeditions against the
// Oracle. NUID (Northeastern University ID) plus email double as the
// registration credential pair; Token is the opaque bearer credential
// issued at register/login time and required on every other endpoint.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	NUID      string    `json:"nuid"`
	Token     string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}
