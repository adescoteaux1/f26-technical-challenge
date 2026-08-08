package engine

import (
	"strings"
	"testing"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
)

func newTestCycle() *models.Cycle {
	return &models.Cycle{
		Tick: 0,
		Gates: []*models.Gate{
			{ID: 1, TotalPower: 4, TotalContainment: 8, AvailablePower: 4, AvailableContainment: 8, Operational: true},
		},
		Voyages: []*models.Voyage{
			{ID: 1, Status: models.VoyageBoarding, RequiredPower: 2, RequiredContainment: 2, RequestedTick: 0},
		},
	}
}

func TestValidateAndApply_AcceptsValidAssignment(t *testing.T) {
	cycle := newTestCycle()

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})

	if len(result.Rejected) != 0 {
		t.Fatalf("expected no rejections, got %+v", result.Rejected)
	}
	if len(result.Accepted) != 1 {
		t.Fatalf("expected 1 accepted assignment, got %d", len(result.Accepted))
	}

	voyage := cycle.Voyages[0]
	if voyage.Status != models.VoyageInTransit {
		t.Errorf("expected voyage status in transit, got %s", voyage.Status)
	}
	if voyage.AssignedGate == nil || *voyage.AssignedGate != 1 {
		t.Errorf("expected voyage assigned to gate 1, got %v", voyage.AssignedGate)
	}

	gate := cycle.Gates[0]
	if gate.AvailablePower != 2 {
		t.Errorf("expected gate availablePower 2, got %d", gate.AvailablePower)
	}
	if gate.AvailableContainment != 6 {
		t.Errorf("expected gate availableContainment 6, got %d", gate.AvailableContainment)
	}
	if len(gate.ActiveVoyages) != 1 || gate.ActiveVoyages[0] != 1 {
		t.Errorf("expected gate.ActiveVoyages = [1], got %v", gate.ActiveVoyages)
	}
}

func TestValidateAndApply_RejectsUnknownGate(t *testing.T) {
	cycle := newTestCycle()
	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 99, VoyageID: 1}})
	requireSingleRejection(t, result, "does not exist")
}

func TestValidateAndApply_RejectsUnknownVoyage(t *testing.T) {
	cycle := newTestCycle()
	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 99}})
	requireSingleRejection(t, result, "does not exist")
}

func TestValidateAndApply_RejectsAwaitingTransferVoyage(t *testing.T) {
	cycle := newTestCycle()
	cycle.Voyages[0].Status = models.VoyageAwaitingTransfer
	cycle.Voyages[0].Prerequisites = []int{42}

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "awaiting prerequisite")
}

func TestValidateAndApply_RejectsAlreadyInTransitVoyage(t *testing.T) {
	cycle := newTestCycle()
	inTransit := 1
	cycle.Voyages[0].Status = models.VoyageInTransit
	cycle.Voyages[0].AssignedGate = &inTransit

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "already in transit")
}

func TestValidateAndApply_RejectsArrivedVoyage(t *testing.T) {
	cycle := newTestCycle()
	cycle.Voyages[0].Status = models.VoyageArrived

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "already arrived")
}

func TestValidateAndApply_RejectsInsufficientPower(t *testing.T) {
	cycle := newTestCycle()
	cycle.Voyages[0].RequiredPower = 100

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "insufficient power")
}

func TestValidateAndApply_RejectsInsufficientContainment(t *testing.T) {
	cycle := newTestCycle()
	cycle.Voyages[0].RequiredContainment = 100

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "insufficient containment")
}

func TestValidateAndApply_RejectsOfflineGate(t *testing.T) {
	cycle := newTestCycle()
	cycle.Gates[0].Operational = false

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "offline")
}

func TestValidateAndApply_RejectsVoyageNotYetRequested(t *testing.T) {
	cycle := newTestCycle()
	cycle.Voyages[0].RequestedTick = 5
	cycle.Tick = 0

	result := ValidateAndApply(cycle, []models.Assignment{{GateID: 1, VoyageID: 1}})
	requireSingleRejection(t, result, "has not been requested")
}

func TestValidateAndApply_RejectsDuplicateVoyageInSameBatch(t *testing.T) {
	cycle := newTestCycle()
	cycle.Gates = append(cycle.Gates, &models.Gate{ID: 2, TotalPower: 4, TotalContainment: 8, AvailablePower: 4, AvailableContainment: 8, Operational: true})

	result := ValidateAndApply(cycle, []models.Assignment{
		{GateID: 1, VoyageID: 1},
		{GateID: 2, VoyageID: 1},
	})

	if len(result.Accepted) != 1 {
		t.Fatalf("expected exactly 1 accepted assignment, got %d", len(result.Accepted))
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected assignment, got %d", len(result.Rejected))
	}
	if got := result.Rejected[0].Reason; !strings.Contains(got, "already assigned earlier in this same request") {
		t.Errorf("unexpected rejection reason: %s", got)
	}
}

func TestValidateAndApply_ResourceAccumulationWithinBatch(t *testing.T) {
	// A single gate with capacity for exactly one of two voyages submitted in
	// the same batch: the second must be rejected for insufficient resources
	// even though the gate started with enough capacity for either voyage
	// individually.
	cycle := &models.Cycle{
		Gates: []*models.Gate{
			{ID: 1, TotalPower: 4, TotalContainment: 8, AvailablePower: 4, AvailableContainment: 8, Operational: true},
		},
		Voyages: []*models.Voyage{
			{ID: 1, Status: models.VoyageBoarding, RequiredPower: 3, RequiredContainment: 3},
			{ID: 2, Status: models.VoyageBoarding, RequiredPower: 3, RequiredContainment: 3},
		},
	}

	result := ValidateAndApply(cycle, []models.Assignment{
		{GateID: 1, VoyageID: 1},
		{GateID: 1, VoyageID: 2},
	})

	if len(result.Accepted) != 1 {
		t.Fatalf("expected exactly 1 accepted assignment, got %d: %+v", len(result.Accepted), result.Accepted)
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected assignment, got %d", len(result.Rejected))
	}
}

func requireSingleRejection(t *testing.T, result ScheduleResult, substr string) {
	t.Helper()
	if len(result.Accepted) != 0 {
		t.Fatalf("expected no accepted assignments, got %+v", result.Accepted)
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected assignment, got %d", len(result.Rejected))
	}
	if !strings.Contains(result.Rejected[0].Reason, substr) {
		t.Errorf("expected rejection reason to contain %q, got %q", substr, result.Rejected[0].Reason)
	}
}
