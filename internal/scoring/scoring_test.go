package scoring

import (
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

func TestCompute_ThroughputAndUtilization(t *testing.T) {
	cycle := &models.Cycle{
		Stats: models.SimStats{
			TotalVoyages:           10,
			ArrivedVoyages:         5,
			GateBusyResourceTicks:  30,
			GateTotalResourceTicks: 60,
			OriginHubArrivals:      map[string]int{},
			OriginHubWaitTicks:     map[string]int64{},
		},
	}

	metrics, _ := Compute(cycle)

	if metrics.Throughput != 50 {
		t.Errorf("expected throughput 50, got %v", metrics.Throughput)
	}
	if metrics.GateUtilization != 50 {
		t.Errorf("expected utilization 50, got %v", metrics.GateUtilization)
	}
}

func TestCompute_DefaultsWhenNoDataYet(t *testing.T) {
	cycle := &models.Cycle{
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{},
			OriginHubWaitTicks: map[string]int64{},
		},
	}

	metrics, _ := Compute(cycle)

	if metrics.Throughput != 0 {
		t.Errorf("expected throughput 0 with no voyages generated, got %v", metrics.Throughput)
	}
	if metrics.ArrivalSuccess != 100 {
		t.Errorf("expected arrivalSuccess to default to 100 before any arrivals, got %v", metrics.ArrivalSuccess)
	}
	if metrics.Reliability != 100 {
		t.Errorf("expected reliability to default to 100 before any assignments, got %v", metrics.Reliability)
	}
	if metrics.Fairness != 100 {
		t.Errorf("expected fairness to default to 100 with fewer than 2 origin hubs, got %v", metrics.Fairness)
	}
}

func TestCompute_ReliabilityPenalizesInvalidAssignments(t *testing.T) {
	cycle := &models.Cycle{
		Stats: models.SimStats{
			ValidAssignments:   1,
			InvalidAssignments: 3,
			OriginHubArrivals:  map[string]int{},
			OriginHubWaitTicks: map[string]int64{},
		},
	}

	metrics, _ := Compute(cycle)

	if metrics.Reliability != 25 {
		t.Errorf("expected reliability 25 (1 of 4 valid), got %v", metrics.Reliability)
	}
}

func TestFairness_PerfectEqualityScoresMax(t *testing.T) {
	cycle := &models.Cycle{
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{"a": 2, "b": 2},
			OriginHubWaitTicks: map[string]int64{"a": 20, "b": 20},
		},
	}

	metrics, _ := Compute(cycle)

	if metrics.Fairness != 100 {
		t.Errorf("expected fairness 100 for identical avg wait times, got %v", metrics.Fairness)
	}
}

func TestFairness_SkewedWaitTimesScoresLower(t *testing.T) {
	equal := &models.Cycle{
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{"a": 2, "b": 2},
			OriginHubWaitTicks: map[string]int64{"a": 20, "b": 20},
		},
	}
	skewed := &models.Cycle{
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{"a": 2, "b": 2},
			OriginHubWaitTicks: map[string]int64{"a": 2, "b": 200}, // hub b starved
		},
	}

	equalMetrics, _ := Compute(equal)
	skewedMetrics, _ := Compute(skewed)

	if skewedMetrics.Fairness >= equalMetrics.Fairness {
		t.Errorf("expected skewed wait times to score lower fairness: equal=%v skewed=%v", equalMetrics.Fairness, skewedMetrics.Fairness)
	}
}

func TestAggregateOverall_AveragesAcrossCycles(t *testing.T) {
	cycles := []*models.Cycle{
		{Score: 80, Metrics: models.Metrics{Throughput: 100, GateUtilization: 100, ArrivalSuccess: 100, Fairness: 100, Reliability: 100}},
		{Score: 60, Metrics: models.Metrics{Throughput: 0, GateUtilization: 0, ArrivalSuccess: 0, Fairness: 0, Reliability: 0}},
	}

	metrics, overall := AggregateOverall(cycles)

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
