package engine

import (
	"fmt"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
)

// RejectedAssignment pairs a submitted assignment with why it was rejected,
// so applicants can debug their scheduler against descriptive errors.
type RejectedAssignment struct {
	models.Assignment
	Reason string `json:"reason"`
}

// ScheduleResult is the outcome of validating a batch of assignments.
type ScheduleResult struct {
	Accepted []models.Assignment
	Rejected []RejectedAssignment
}

// ValidateAndApply checks every submitted assignment against the current
// cycle state (including resource usage from earlier assignments in the
// same batch) and applies the ones that pass. Invalid assignments are
// reported back with a reason but do not abort the whole batch.
func ValidateAndApply(cycle *models.Cycle, assignments []models.Assignment) ScheduleResult {
	result := ScheduleResult{}

	gatesByID := make(map[int]*models.Gate, len(cycle.Gates))
	for _, g := range cycle.Gates {
		gatesByID[g.ID] = g
	}
	voyagesByID := make(map[int]*models.Voyage, len(cycle.Voyages))
	for _, v := range cycle.Voyages {
		voyagesByID[v.ID] = v
	}

	claimedVoyages := make(map[int]bool) // voyages already assigned earlier in this same batch

	for _, a := range assignments {
		reason := validateOne(cycle, gatesByID, voyagesByID, claimedVoyages, a)
		if reason != "" {
			result.Rejected = append(result.Rejected, RejectedAssignment{Assignment: a, Reason: reason})
			cycle.Stats.InvalidAssignments++
			continue
		}

		gate := gatesByID[a.GateID]
		voyage := voyagesByID[a.VoyageID]

		gate.AvailablePower -= voyage.RequiredPower
		gate.AvailableContainment -= voyage.RequiredContainment
		gate.ActiveVoyages = append(gate.ActiveVoyages, voyage.ID)

		voyage.Status = models.VoyageInTransit
		voyage.AssignedGate = &a.GateID
		start := cycle.Tick
		voyage.DepartureTick = &start

		claimedVoyages[voyage.ID] = true
		result.Accepted = append(result.Accepted, a)
		cycle.Stats.ValidAssignments++
	}

	return result
}

func validateOne(
	cycle *models.Cycle,
	gatesByID map[int]*models.Gate,
	voyagesByID map[int]*models.Voyage,
	claimedVoyages map[int]bool,
	a models.Assignment,
) string {
	gate, ok := gatesByID[a.GateID]
	if !ok {
		return fmt.Sprintf("gate %d does not exist", a.GateID)
	}
	voyage, ok := voyagesByID[a.VoyageID]
	if !ok {
		return fmt.Sprintf("voyage %d does not exist", a.VoyageID)
	}
	if voyage.RequestedTick > cycle.Tick {
		return fmt.Sprintf("voyage %d has not been requested yet (requested tick %d, current tick %d)", voyage.ID, voyage.RequestedTick, cycle.Tick)
	}
	if claimedVoyages[voyage.ID] {
		return fmt.Sprintf("voyage %d was already assigned earlier in this same request", voyage.ID)
	}
	switch voyage.Status {
	case models.VoyageArrived:
		return fmt.Sprintf("voyage %d has already arrived", voyage.ID)
	case models.VoyageInTransit:
		return fmt.Sprintf("voyage %d is already in transit via gate %d", voyage.ID, *voyage.AssignedGate)
	case models.VoyageAwaitingTransfer:
		return fmt.Sprintf("voyage %d is awaiting prerequisite voyages %v", voyage.ID, voyage.Prerequisites)
	}
	if !gate.Operational {
		return fmt.Sprintf("gate %d is currently offline", gate.ID)
	}
	if voyage.RequiredPower > gate.AvailablePower {
		return fmt.Sprintf("gate %d has insufficient power for voyage %d (needs %d, has %d)", gate.ID, voyage.ID, voyage.RequiredPower, gate.AvailablePower)
	}
	if voyage.RequiredContainment > gate.AvailableContainment {
		return fmt.Sprintf("gate %d has insufficient containment for voyage %d (needs %d, has %d)", gate.ID, voyage.ID, voyage.RequiredContainment, gate.AvailableContainment)
	}
	return ""
}
