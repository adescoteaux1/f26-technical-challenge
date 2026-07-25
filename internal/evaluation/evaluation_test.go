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
	eval, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if eval.TotalSimulations != DefaultTotalSimulations {
		t.Fatalf("expected %d total simulations, got %d", DefaultTotalSimulations, eval.TotalSimulations)
	}
	if len(eval.ProfilePlan) != DefaultTotalSimulations {
		t.Fatalf("expected profile plan of length %d, got %d", DefaultTotalSimulations, len(eval.ProfilePlan))
	}

	seen := map[string]bool{}
	for _, p := range eval.ProfilePlan {
		seen[p] = true
	}
	for _, want := range []string{"dependency_chains", "burst_traffic", "heavy_compute", "deadline_critical", "resource_constrained", "balanced"} {
		if !seen[want] {
			t.Errorf("expected profile plan to include %q at least once, plan=%v", want, eval.ProfilePlan)
		}
	}
}

func TestSubmit_AdvancesTickAndPersistsState(t *testing.T) {
	st := storetest.New()
	eval, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, eval.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Simulation == nil {
		t.Fatalf("expected simulation still in progress")
	}
	if result.Simulation.Tick != 1 {
		t.Fatalf("expected tick 1 after one submit, got %d", result.Simulation.Tick)
	}
}

func TestSubmit_RollsOverToNextProfileWhenSimulationFinishes(t *testing.T) {
	st := storetest.New()
	eval, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Force the current simulation to look finished so the very next Submit
	// call has to roll over into simulation 2's profile.
	sim, err := st.GetSimulation(context.Background(), eval.ID, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sim.MaxTicks = 0 // AdvanceTick will finish it immediately
	if err := st.SaveSimulation(context.Background(), sim); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, eval.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Simulation == nil {
		t.Fatalf("expected a new simulation to have started")
	}
	if result.Simulation.Number != 2 {
		t.Fatalf("expected rollover to simulation 2, got %d", result.Simulation.Number)
	}
	if result.Simulation.Profile != eval.ProfilePlan[1] {
		t.Fatalf("expected simulation 2 to use planned profile %q, got %q", eval.ProfilePlan[1], result.Simulation.Profile)
	}
	if result.Evaluation.CurrentSimulation != 2 {
		t.Fatalf("expected evaluation.CurrentSimulation advanced to 2, got %d", result.Evaluation.CurrentSimulation)
	}
}

func TestSubmit_FinishesEvaluationAfterLastSimulation(t *testing.T) {
	st := storetest.New()
	eval, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Fast-forward straight to the final simulation slot.
	if err := st.AdvanceEvaluation(context.Background(), eval.ID, eval.TotalSimulations); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lastSim, err := buildSimulation(eval.ID, eval.TotalSimulations, eval.ProfilePlan[eval.TotalSimulations-1])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lastSim.MaxTicks = 0 // finishes on the next tick
	if err := st.SaveSimulation(context.Background(), lastSim); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, eval.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Simulation != nil {
		t.Fatalf("expected no simulation in response once the evaluation is finished")
	}
	if !result.Evaluation.Finished {
		t.Fatalf("expected evaluation to be finished")
	}
}

func TestSubmit_NoOpAfterEvaluationAlreadyFinished(t *testing.T) {
	st := storetest.New()
	eval, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := st.FinishEvaluation(context.Background(), eval.ID, 88, models.Metrics{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := Submit(context.Background(), st, eval.ID, testUserID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Simulation != nil {
		t.Fatalf("expected nil simulation for an already-finished evaluation")
	}
	if result.Evaluation.OverallScore != 88 {
		t.Fatalf("expected finished evaluation's score preserved, got %v", result.Evaluation.OverallScore)
	}
}

func TestSubmit_RejectsWrongOwner(t *testing.T) {
	st := storetest.New()
	eval, err := Create(context.Background(), st, testUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := Submit(context.Background(), st, eval.ID, "someone-else", nil); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for a non-owner, got %v", err)
	}
	if _, _, err := GetState(context.Background(), st, eval.ID, "someone-else"); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for a non-owner, got %v", err)
	}
}
