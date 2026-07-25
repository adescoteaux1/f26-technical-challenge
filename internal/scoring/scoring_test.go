package scoring

import (
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

func TestCompute_ThroughputAndUtilization(t *testing.T) {
	sim := &models.Simulation{
		Stats: models.SimStats{
			TotalJobs:                10,
			CompletedJobs:            5,
			WorkerBusyResourceTicks:  30,
			WorkerTotalResourceTicks: 60,
			ProjectCompletions:       map[string]int{},
			ProjectWaitTicks:         map[string]int64{},
		},
	}

	metrics, _ := Compute(sim)

	if metrics.Throughput != 50 {
		t.Errorf("expected throughput 50, got %v", metrics.Throughput)
	}
	if metrics.WorkerUtilization != 50 {
		t.Errorf("expected utilization 50, got %v", metrics.WorkerUtilization)
	}
}

func TestCompute_DefaultsWhenNoDataYet(t *testing.T) {
	sim := &models.Simulation{
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{},
			ProjectWaitTicks:   map[string]int64{},
		},
	}

	metrics, _ := Compute(sim)

	if metrics.Throughput != 0 {
		t.Errorf("expected throughput 0 with no jobs generated, got %v", metrics.Throughput)
	}
	if metrics.DeadlineSuccess != 100 {
		t.Errorf("expected deadlineSuccess to default to 100 before any completions, got %v", metrics.DeadlineSuccess)
	}
	if metrics.Reliability != 100 {
		t.Errorf("expected reliability to default to 100 before any assignments, got %v", metrics.Reliability)
	}
	if metrics.Fairness != 100 {
		t.Errorf("expected fairness to default to 100 with fewer than 2 projects, got %v", metrics.Fairness)
	}
}

func TestCompute_ReliabilityPenalizesInvalidAssignments(t *testing.T) {
	sim := &models.Simulation{
		Stats: models.SimStats{
			ValidAssignments:   1,
			InvalidAssignments: 3,
			ProjectCompletions: map[string]int{},
			ProjectWaitTicks:   map[string]int64{},
		},
	}

	metrics, _ := Compute(sim)

	if metrics.Reliability != 25 {
		t.Errorf("expected reliability 25 (1 of 4 valid), got %v", metrics.Reliability)
	}
}

func TestFairness_PerfectEqualityScoresMax(t *testing.T) {
	sim := &models.Simulation{
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{"a": 2, "b": 2},
			ProjectWaitTicks:   map[string]int64{"a": 20, "b": 20},
		},
	}

	metrics, _ := Compute(sim)

	if metrics.Fairness != 100 {
		t.Errorf("expected fairness 100 for identical avg wait times, got %v", metrics.Fairness)
	}
}

func TestFairness_SkewedWaitTimesScoresLower(t *testing.T) {
	equal := &models.Simulation{
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{"a": 2, "b": 2},
			ProjectWaitTicks:   map[string]int64{"a": 20, "b": 20},
		},
	}
	skewed := &models.Simulation{
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{"a": 2, "b": 2},
			ProjectWaitTicks:   map[string]int64{"a": 2, "b": 200}, // project b starved
		},
	}

	equalMetrics, _ := Compute(equal)
	skewedMetrics, _ := Compute(skewed)

	if skewedMetrics.Fairness >= equalMetrics.Fairness {
		t.Errorf("expected skewed wait times to score lower fairness: equal=%v skewed=%v", equalMetrics.Fairness, skewedMetrics.Fairness)
	}
}

func TestAggregateOverall_AveragesAcrossSimulations(t *testing.T) {
	sims := []*models.Simulation{
		{Score: 80, Metrics: models.Metrics{Throughput: 100, WorkerUtilization: 100, DeadlineSuccess: 100, Fairness: 100, Reliability: 100}},
		{Score: 60, Metrics: models.Metrics{Throughput: 0, WorkerUtilization: 0, DeadlineSuccess: 0, Fairness: 0, Reliability: 0}},
	}

	metrics, overall := AggregateOverall(sims)

	if overall != 70 {
		t.Errorf("expected overall average 70, got %v", overall)
	}
	if metrics.Throughput != 50 {
		t.Errorf("expected throughput average 50, got %v", metrics.Throughput)
	}
}

func TestAggregateOverall_EmptyReturnsZero(t *testing.T) {
	metrics, overall := AggregateOverall(nil)
	if overall != 0 || metrics != (models.Metrics{}) {
		t.Errorf("expected zero value for empty input, got overall=%v metrics=%+v", overall, metrics)
	}
}
