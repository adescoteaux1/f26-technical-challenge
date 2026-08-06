package engine

import (
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

func inTransitVoyageCycle(duration, power, containment int) (*models.Cycle, *models.Voyage, *models.Gate) {
	gate := &models.Gate{ID: 1, TotalPower: 4, TotalContainment: 8, AvailablePower: 4 - power, AvailableContainment: 8 - containment, Operational: true, ActiveVoyages: []int{1}}
	voyage := &models.Voyage{
		ID: 1, Status: models.VoyageInTransit, RequiredPower: power, RequiredContainment: containment,
		EstimatedDuration: duration, RemainingDuration: duration,
	}
	gateID := 1
	voyage.AssignedGate = &gateID
	cycle := &models.Cycle{
		Gates:    []*models.Gate{gate},
		Voyages:  []*models.Voyage{voyage},
		MaxTicks: 1000,
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{},
			OriginHubWaitTicks: map[string]int64{},
		},
	}
	return cycle, voyage, gate
}

func TestAdvanceTick_VoyageArrivesAfterDurationElapses(t *testing.T) {
	cycle, voyage, _ := inTransitVoyageCycle(2, 2, 2)

	AdvanceTick(cycle)
	if voyage.Status != models.VoyageInTransit {
		t.Fatalf("expected voyage still in transit after 1 of 2 ticks, got %s", voyage.Status)
	}
	if voyage.RemainingDuration != 1 {
		t.Fatalf("expected remaining duration 1, got %d", voyage.RemainingDuration)
	}

	AdvanceTick(cycle)
	if voyage.Status != models.VoyageArrived {
		t.Fatalf("expected voyage arrived after 2 ticks, got %s", voyage.Status)
	}
	if voyage.ArrivalTick == nil || *voyage.ArrivalTick != 2 {
		t.Fatalf("expected arrival tick 2, got %v", voyage.ArrivalTick)
	}
}

func TestAdvanceTick_ArrivalFreesGateResources(t *testing.T) {
	cycle, _, gate := inTransitVoyageCycle(1, 3, 3)

	AdvanceTick(cycle)

	if gate.AvailablePower != gate.TotalPower {
		t.Errorf("expected gate power fully freed, got %d/%d", gate.AvailablePower, gate.TotalPower)
	}
	if gate.AvailableContainment != gate.TotalContainment {
		t.Errorf("expected gate containment fully freed, got %d/%d", gate.AvailableContainment, gate.TotalContainment)
	}
	if len(gate.ActiveVoyages) != 0 {
		t.Errorf("expected gate.ActiveVoyages empty, got %v", gate.ActiveVoyages)
	}
}

func TestAdvanceTick_DependentVoyageUnlocksOnlyWhenAllPrerequisitesArrive(t *testing.T) {
	dep1 := &models.Voyage{ID: 1, Status: models.VoyageInTransit, RemainingDuration: 1}
	g1 := 10
	dep1.AssignedGate = &g1
	dep2 := &models.Voyage{ID: 2, Status: models.VoyageAwaitingTransfer, Prerequisites: []int{1}}
	// dep2 itself never gets assigned; used purely to seed the dependent's prerequisite list
	dependent := &models.Voyage{ID: 3, Status: models.VoyageAwaitingTransfer, Prerequisites: []int{1, 2}}

	gate := &models.Gate{ID: 10, TotalPower: 4, TotalContainment: 8, AvailablePower: 4, AvailableContainment: 8, Operational: true, ActiveVoyages: []int{1}}
	cycle := &models.Cycle{
		Gates:    []*models.Gate{gate},
		Voyages:  []*models.Voyage{dep1, dep2, dependent},
		MaxTicks: 1000,
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{},
			OriginHubWaitTicks: map[string]int64{},
		},
	}

	AdvanceTick(cycle) // dep1 arrives; dep2 still awaiting transfer (depends on dep1... wait dep2 has no assignment)

	if dependent.Status != models.VoyageAwaitingTransfer {
		t.Fatalf("expected dependent still awaiting transfer while dep2 not arrived, got %s", dependent.Status)
	}
	if dep2.Status != models.VoyageBoarding {
		t.Fatalf("expected dep2 to unlock once dep1 arrived, got %s", dep2.Status)
	}

	// Manually mark dep2 arrived and re-run unlock logic via another tick.
	dep2.Status = models.VoyageArrived
	tick := cycle.Tick
	dep2.ArrivalTick = &tick

	AdvanceTick(cycle)

	if dependent.Status != models.VoyageBoarding {
		t.Fatalf("expected dependent boarding once both prerequisites arrived, got %s", dependent.Status)
	}
}

func TestAdvanceTick_InTransitVoyagePausesWhileGateDown(t *testing.T) {
	cycle, voyage, gate := inTransitVoyageCycle(3, 2, 2)
	gate.Operational = false
	gate.OfflineUntil = 1000 // stays down for the whole test

	AdvanceTick(cycle)

	if voyage.RemainingDuration != 3 {
		t.Fatalf("expected voyage progress paused while gate down, remaining=%d", voyage.RemainingDuration)
	}
}

func TestAdvanceTick_GateOutageRequeuesInTransitVoyage(t *testing.T) {
	cycle, voyage, gate := inTransitVoyageCycle(5, 2, 2)
	cycle.OutageRate = 1.0 // force outage on the next eligible tick
	cycle.OutageTicksMin = 3
	cycle.OutageTicksMax = 3

	AdvanceTick(cycle)

	if gate.Operational {
		t.Fatalf("expected gate to have gone offline")
	}
	if voyage.Status != models.VoyageBoarding {
		t.Fatalf("expected in-transit voyage requeued to boarding after gate outage, got %s", voyage.Status)
	}
	if voyage.AssignedGate != nil {
		t.Fatalf("expected voyage unassigned after gate outage")
	}
	if gate.AvailablePower != gate.TotalPower {
		t.Fatalf("expected gate resources reset on outage")
	}

	// Gate should recover once its outage expires.
	AdvanceTick(cycle)
	AdvanceTick(cycle)
	if gate.Operational {
		t.Fatalf("expected gate still down before outage expires")
	}
	AdvanceTick(cycle)
	if !gate.Operational {
		t.Fatalf("expected gate to recover after outage duration elapses")
	}
}

func TestAdvanceTick_FinishesWhenAllVoyagesArrive(t *testing.T) {
	cycle, _, _ := inTransitVoyageCycle(1, 1, 1)

	AdvanceTick(cycle)

	if !cycle.Finished {
		t.Fatalf("expected cycle finished once all voyages arrived")
	}
}

func TestAdvanceTick_FinishesAtMaxTicks(t *testing.T) {
	cycle, _, _ := inTransitVoyageCycle(1000, 1, 1)
	cycle.MaxTicks = 1

	AdvanceTick(cycle)

	if !cycle.Finished {
		t.Fatalf("expected cycle finished at MaxTicks even with incomplete voyages")
	}
}

func TestAdvanceTick_MultiLegVoyageAdvancesThroughLegsBeforeArriving(t *testing.T) {
	gate := &models.Gate{ID: 1, TotalPower: 4, TotalContainment: 8, AvailablePower: 2, AvailableContainment: 6, Operational: true, ActiveVoyages: []int{1}}
	gateID := 1
	voyage := &models.Voyage{
		ID: 1, Status: models.VoyageInTransit, RequiredPower: 2, RequiredContainment: 2,
		EstimatedDuration: 1, RemainingDuration: 1, AssignedGate: &gateID,
		Legs: []models.VoyageLeg{
			{RequiredPower: 2, RequiredContainment: 2, EstimatedDuration: 1},
			{RequiredPower: 3, RequiredContainment: 3, EstimatedDuration: 1},
		},
		LegIndex: 0,
	}
	cycle := &models.Cycle{
		Gates: []*models.Gate{gate}, Voyages: []*models.Voyage{voyage}, MaxTicks: 1000,
		Stats: models.SimStats{OriginHubArrivals: map[string]int{}, OriginHubWaitTicks: map[string]int64{}},
	}

	AdvanceTick(cycle) // leg 1 of 2 completes

	if voyage.Status != models.VoyageBoarding {
		t.Fatalf("expected voyage back to boarding for its next leg, got %s", voyage.Status)
	}
	if voyage.LegIndex != 1 {
		t.Fatalf("expected LegIndex advanced to 1, got %d", voyage.LegIndex)
	}
	if voyage.RequiredPower != 3 || voyage.RemainingDuration != 1 {
		t.Fatalf("expected voyage's current requirement to reflect leg 2, got power=%d remaining=%d", voyage.RequiredPower, voyage.RemainingDuration)
	}
	if cycle.Stats.ArrivedVoyages != 0 {
		t.Fatalf("expected the voyage NOT counted as arrived after only 1 of 2 legs, got ArrivedVoyages=%d", cycle.Stats.ArrivedVoyages)
	}
	if voyage.AssignedGate != nil {
		t.Fatalf("expected voyage unassigned between legs")
	}

	// Assign leg 2 to the gate (mirrors what the validator would do for a
	// scheduler's next assignment) and let it complete.
	voyage.Status = models.VoyageInTransit
	voyage.AssignedGate = &gateID
	departureTick := cycle.Tick
	voyage.DepartureTick = &departureTick
	gate.ActiveVoyages = []int{1}
	gate.AvailablePower -= voyage.RequiredPower
	gate.AvailableContainment -= voyage.RequiredContainment

	AdvanceTick(cycle) // leg 2 (final) completes

	if voyage.Status != models.VoyageArrived {
		t.Fatalf("expected voyage arrived after its final leg, got %s", voyage.Status)
	}
	if cycle.Stats.ArrivedVoyages != 1 {
		t.Fatalf("expected exactly 1 arrival counted for the whole 2-leg voyage, got %d", cycle.Stats.ArrivedVoyages)
	}
}

func TestAdvanceTick_PremiumVoyageMeetingSLACounted(t *testing.T) {
	cycle, voyage, _ := inTransitVoyageCycle(1, 1, 1)
	voyage.ArrivalDeadline = 100
	sla := 5
	voyage.SLADeadline = &sla

	AdvanceTick(cycle) // arrives at tick 1, well within the SLA deadline of 5

	if cycle.Stats.PremiumArrivalsOnTime != 1 {
		t.Fatalf("expected 1 premium on-time arrival, got %d", cycle.Stats.PremiumArrivalsOnTime)
	}
	if cycle.Stats.PremiumArrivalsLate != 0 {
		t.Fatalf("expected 0 premium late arrivals, got %d", cycle.Stats.PremiumArrivalsLate)
	}
}

func TestAdvanceTick_PremiumVoyageMissingSLACounted(t *testing.T) {
	cycle, voyage, _ := inTransitVoyageCycle(1, 1, 1)
	voyage.ArrivalDeadline = 100 // generous overall deadline...
	sla := 0                     // ...but SLA already blown by the time it arrives at tick 1
	voyage.SLADeadline = &sla

	AdvanceTick(cycle)

	if cycle.Stats.PremiumArrivalsLate != 1 {
		t.Fatalf("expected 1 premium late arrival, got %d", cycle.Stats.PremiumArrivalsLate)
	}
	if cycle.Stats.ArrivalsOnTime != 1 {
		t.Fatalf("expected the (separate) overall deadline to still be met, got ArrivalsOnTime=%d", cycle.Stats.ArrivalsOnTime)
	}
}

func TestAdvanceTick_NonPremiumVoyageDoesNotAffectSLAStats(t *testing.T) {
	cycle, voyage, _ := inTransitVoyageCycle(1, 1, 1)
	voyage.ArrivalDeadline = 100
	voyage.SLADeadline = nil // not a premium-hub voyage

	AdvanceTick(cycle)

	if cycle.Stats.PremiumArrivalsOnTime != 0 || cycle.Stats.PremiumArrivalsLate != 0 {
		t.Fatalf("expected no SLA stats touched for a non-premium voyage, got onTime=%d late=%d",
			cycle.Stats.PremiumArrivalsOnTime, cycle.Stats.PremiumArrivalsLate)
	}
}
