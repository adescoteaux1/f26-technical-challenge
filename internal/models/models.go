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
	RequiredPower       int `json:"requiredPower" doc:"Power this leg draws from its gate"`
	RequiredContainment int `json:"requiredContainment" doc:"Containment this leg draws from its gate"`
	EstimatedDuration   int `json:"estimatedDuration" doc:"Ticks this leg takes once assigned to a gate"`
}

// Voyage is a unit of travel requested by a traveler.
type Voyage struct {
	ID                  int    `json:"id" doc:"Unique voyage ID, used in assignments"`
	OriginHub           string `json:"originHub" doc:"Hub this voyage departs from"`
	Priority            int    `json:"priority" doc:"1 (low) to 5 (critical); not enforced by the Control Tower — what you do with it is up to your strategy"`
	EstimatedDuration   int    `json:"estimatedDuration" doc:"Ticks required while in transit for the current leg (whole trip, if single-hop)"`
	RequiredPower       int    `json:"requiredPower" doc:"Power the current leg draws from its gate"`
	RequiredContainment int    `json:"requiredContainment" doc:"Containment the current leg draws from its gate"`
	ArrivalDeadline     int    `json:"arrivalDeadline" doc:"Tick by which the whole trip should arrive; informational, not enforced by validation"`
	Prerequisites       []int  `json:"prerequisites" doc:"Other voyage IDs that must reach 'arrived' before this voyage can become 'boarding'"`
	RequestedTick       int    `json:"requestedTick" doc:"Tick at which this voyage becomes visible in the API"`

	// Legs is the full planned corridor for a multi-hop voyage (empty for a
	// normal single-hop voyage). LegIndex is which leg is current: the
	// top-level RequiredPower/RequiredContainment/EstimatedDuration/
	// RemainingDuration always describe *that* leg, so the rest of the
	// engine (validator, resource accounting, gate-outage requeueing) needs
	// no special multi-hop handling — it only ever sees "the current leg."
	Legs     []VoyageLeg `json:"legs" doc:"Full planned corridor for a multi-hop voyage; null for a normal single-hop voyage"`
	LegIndex int         `json:"legIndex" doc:"Index into legs of the current leg; 0 for a normal single-hop voyage"`

	// SLADeadline is non-nil only for voyages whose OriginHub is one of the
	// cycle's PremiumHubs: a tighter internal deadline than ArrivalDeadline,
	// used only for the slaCompliance metric (missing it doesn't reject
	// scheduling, unlike ArrivalDeadline which is purely informational too).
	SLADeadline *int `json:"slaDeadline" doc:"Tighter deadline than arrivalDeadline, only for premium-hub voyages; null otherwise. Feeds slaCompliance only — never enforced"`

	Status            VoyageStatus `json:"status" enum:"awaiting_transfer,boarding,in_transit,arrived" doc:"awaiting_transfer: prerequisites incomplete. boarding: schedulable now. in_transit: assigned to a gate. arrived: done"`
	RemainingDuration int          `json:"remainingDuration" doc:"Ticks left on the current leg"`
	AssignedGate      *int         `json:"assignedGate" doc:"Gate ID this voyage is currently assigned to; null if not in transit"`
	BoardingTick      *int         `json:"boardingTick" doc:"Tick this voyage's prerequisites were satisfied and it became schedulable"`
	DepartureTick     *int         `json:"departureTick" doc:"Tick the current/most recent leg was assigned to a gate; null if never assigned"`
	ArrivalTick       *int         `json:"arrivalTick" doc:"Tick the whole trip arrived; null until status is arrived"`
}

// Gate routes voyages and has finite power/containment capacity.
type Gate struct {
	ID                   int   `json:"id" doc:"Unique gate ID, used in assignments"`
	TotalPower           int   `json:"totalPower" doc:"Total power capacity, whether or not currently in use"`
	TotalContainment     int   `json:"totalContainment" doc:"Total containment capacity, whether or not currently in use"`
	AvailablePower       int   `json:"availablePower" doc:"Power currently free to assign"`
	AvailableContainment int   `json:"availableContainment" doc:"Containment currently free to assign"`
	ActiveVoyages        []int `json:"activeVoyages" doc:"IDs of voyages currently assigned to this gate"`
	Operational          bool  `json:"operational" doc:"False while this gate is offline; assigning to it is rejected until it recovers"`
	OfflineUntil         int   `json:"offlineUntil" doc:"Tick this gate comes back online; meaningless while operational is true"`
}

// Assignment is a single scheduling decision submitted by the client.
type Assignment struct {
	GateID   int `json:"gateId" doc:"ID of the gate to send the voyage through"`
	VoyageID int `json:"voyageId" doc:"ID of the voyage to assign"`
}

// Metrics is the set of category scores reported to the client.
type Metrics struct {
	Throughput      float64 `json:"throughput" doc:"% of voyages that arrived"`
	GateUtilization float64 `json:"gateUtilization" doc:"% of gate capacity kept busy"`
	ArrivalSuccess  float64 `json:"arrivalSuccess" doc:"% of arrived voyages that beat their arrivalDeadline"`
	Fairness        float64 `json:"fairness" doc:"Penalizes starving one origin hub's travelers to favor another"`
	Reliability     float64 `json:"reliability" doc:"% of submitted assignments that were valid"`
	SLACompliance   float64 `json:"slaCompliance" doc:"% of premium-hub voyages that beat their tighter slaDeadline"`
}

// Cycle is one independently-scored workload within an expedition.
//
// This struct is the full internal representation persisted to storage
// between HTTP requests (the Control Tower is stateless per-request). The public
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
// Control Tower. NUID (Northeastern University ID) plus email double as the
// registration credential pair; Token is the opaque bearer credential
// issued at register/login time and required on every other endpoint.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	NUID      string    `json:"nuid"`
	Token     string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}
