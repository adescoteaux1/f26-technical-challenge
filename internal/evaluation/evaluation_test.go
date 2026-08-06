package evaluation

import (
	"context"
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/storetest"
)

const testUserID = "test-user-1"

func TestCreate_SamplesEveryProfileAtLeastOnce(t *testing.T) {
	st := storetest.New()
	exp, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exp.TotalCycles != DefaultTotalCycles {
		t.Fatalf("expected %d total cycles, got %d", DefaultTotalCycles, exp.TotalCycles)
	}
	if len(exp.ProfilePlan) != DefaultTotalCycles {
		t.Fatalf("expected profile plan of length %d, got %d", DefaultTotalCycles, len(exp.ProfilePlan))
	}

	seen := map[string]bool{}
	for _, p := range exp.ProfilePlan {
		seen[p] = true
	}
	for _, want := range []string{"transfer_chains", "surge_arrivals", "deep_rift", "narrow_window", "gate_congestion", "mixed_traffic"} {
		if !seen[want] {
			t.Errorf("expected profile plan to include %q at least once, plan=%v", want, exp.ProfilePlan)
		}
	}
}

func TestSubmit_AdvancesTickAndPersistsState(t *testing.T) {
	st := storetest.New()
	exp, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, exp.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Cycle == nil {
		t.Fatalf("expected cycle still in progress")
	}
	if result.Cycle.Tick != 1 {
		t.Fatalf("expected tick 1 after one submit, got %d", result.Cycle.Tick)
	}
}

func TestSubmit_RollsOverToNextProfileWhenCycleFinishes(t *testing.T) {
	st := storetest.New()
	exp, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Force the current cycle to look finished so the very next Submit
	// call has to roll over into cycle 2's profile.
	cycle, err := st.GetCycle(context.Background(), exp.ID, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cycle.MaxTicks = 0 // AdvanceTick will finish it immediately
	if err := st.SaveCycle(context.Background(), cycle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, exp.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Cycle == nil {
		t.Fatalf("expected a new cycle to have started")
	}
	if result.Cycle.Number != 2 {
		t.Fatalf("expected rollover to cycle 2, got %d", result.Cycle.Number)
	}
	if result.Cycle.Profile != exp.ProfilePlan[1] {
		t.Fatalf("expected cycle 2 to use planned profile %q, got %q", exp.ProfilePlan[1], result.Cycle.Profile)
	}
	if result.Expedition.CurrentCycle != 2 {
		t.Fatalf("expected expedition.CurrentCycle advanced to 2, got %d", result.Expedition.CurrentCycle)
	}
}

func TestSubmit_FinishesExpeditionAfterLastCycle(t *testing.T) {
	st := storetest.New()
	exp, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Fast-forward straight to the final cycle slot.
	if err := st.AdvanceExpedition(context.Background(), exp.ID, exp.TotalCycles); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lastCycle, err := buildCycle(exp.ID, exp.TotalCycles, exp.ProfilePlan[exp.TotalCycles-1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lastCycle.MaxTicks = 0 // finishes on the next tick
	if err := st.SaveCycle(context.Background(), lastCycle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, exp.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Cycle != nil {
		t.Fatalf("expected no cycle in response once the expedition is finished")
	}
	if !result.Expedition.Finished {
		t.Fatalf("expected expedition to be finished")
	}
}

func TestSubmit_NoOpAfterExpeditionAlreadyFinished(t *testing.T) {
	st := storetest.New()
	exp, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := st.FinishExpedition(context.Background(), exp.ID, 88, models.Metrics{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, exp.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Cycle != nil {
		t.Fatalf("expected nil cycle for an already-finished expedition")
	}
	if result.Expedition.OverallScore != 88 {
		t.Fatalf("expected finished expedition's score preserved, got %v", result.Expedition.OverallScore)
	}
}

func TestSubmit_RejectsWrongOwner(t *testing.T) {
	st := storetest.New()
	exp, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := Submit(context.Background(), st, exp.ID, "someone-else", nil); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for a non-owner, got %v", err)
	}
	if _, _, err := GetState(context.Background(), st, exp.ID, "someone-else"); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for a non-owner, got %v", err)
	}
}
