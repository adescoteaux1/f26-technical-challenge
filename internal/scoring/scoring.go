// Package scoring turns the accumulated SimStats into the metrics and
// overall score reported to the client. Metrics are recomputed from the
// running totals on every request, so GET /expedition/{id} always reflects
// current progress rather than only a final snapshot.
package scoring

import (
	"math"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
)

// Weights used to combine category scores into the overall score. These are
// intentionally not exposed through the API (mirrors "the exact scoring
// formula is intentionally not published"), but are centralized here so
// they're easy to tune during Control Tower development.
const (
	weightThroughput  = 0.20
	weightUtilization = 0.15
	weightDeadline    = 0.20
	weightFairness    = 0.15
	weightReliability = 0.15
	weightSLA         = 0.15
)

// Compute derives the current Metrics + overall score from a cycle's
// accumulated stats. Safe to call at any tick, including tick 0.
func Compute(cycle *models.Cycle) (models.Metrics, float64) {
	s := cycle.Stats

	throughput := percent(s.ArrivedVoyages, s.TotalVoyages, 0)
	utilization := percent64(s.GateBusyResourceTicks, s.GateTotalResourceTicks, 0)
	arrivalSuccess := percent(s.ArrivalsOnTime, s.ArrivalsOnTime+s.ArrivalsLate, 100)
	fairness := fairnessScore(s)
	reliability := percent(s.ValidAssignments, s.ValidAssignments+s.InvalidAssignments, 100)
	slaCompliance := percent(s.PremiumArrivalsOnTime, s.PremiumArrivalsOnTime+s.PremiumArrivalsLate, 100)

	metrics := models.Metrics{
		Throughput:      round1(throughput),
		GateUtilization: round1(utilization),
		ArrivalSuccess:  round1(arrivalSuccess),
		Fairness:        round1(fairness),
		Reliability:     round1(reliability),
		SLACompliance:   round1(slaCompliance),
	}

	overall := weightThroughput*throughput +
		weightUtilization*utilization +
		weightDeadline*arrivalSuccess +
		weightFairness*fairness +
		weightReliability*reliability +
		weightSLA*slaCompliance

	return metrics, round1(overall)
}

// fairnessScore penalizes large disparities in average queue wait time across
// origin hubs: a scheduler that always favors one hub and starves another
// scores lower here even if throughput is high.
func fairnessScore(s models.SimStats) float64 {
	var avgWaits []float64
	for hub, arrivals := range s.OriginHubArrivals {
		if arrivals == 0 {
			continue
		}
		avgWaits = append(avgWaits, float64(s.OriginHubWaitTicks[hub])/float64(arrivals))
	}
	if len(avgWaits) < 2 {
		return 100
	}

	mean := 0.0
	for _, w := range avgWaits {
		mean += w
	}
	mean /= float64(len(avgWaits))
	if mean == 0 {
		return 100
	}

	variance := 0.0
	for _, w := range avgWaits {
		variance += (w - mean) * (w - mean)
	}
	variance /= float64(len(avgWaits))
	stddev := math.Sqrt(variance)

	coefficientOfVariation := stddev / mean
	// A CoV of 0 (identical average waits) -> 100. A CoV of 1.5+ -> 0.
	score := 100 * (1 - coefficientOfVariation/1.5)
	return clamp(score, 0, 100)
}

// AggregateOverall averages per-cycle scores/metrics into the
// expedition-level result, matching the "average performance across every
// cycle" scoring philosophy rather than weighting any one workload
// more heavily than another.
func AggregateOverall(cycles []*models.Cycle) (models.Metrics, float64) {
	if len(cycles) == 0 {
		return models.Metrics{}, 0
	}
	var m models.Metrics
	var overall float64
	for _, cycle := range cycles {
		m.Throughput += cycle.Metrics.Throughput
		m.GateUtilization += cycle.Metrics.GateUtilization
		m.ArrivalSuccess += cycle.Metrics.ArrivalSuccess
		m.Fairness += cycle.Metrics.Fairness
		m.Reliability += cycle.Metrics.Reliability
		m.SLACompliance += cycle.Metrics.SLACompliance
		overall += cycle.Score
	}
	n := float64(len(cycles))
	m.Throughput = round1(m.Throughput / n)
	m.GateUtilization = round1(m.GateUtilization / n)
	m.ArrivalSuccess = round1(m.ArrivalSuccess / n)
	m.Fairness = round1(m.Fairness / n)
	m.Reliability = round1(m.Reliability / n)
	m.SLACompliance = round1(m.SLACompliance / n)
	return m, round1(overall / n)
}

func percent(numerator, denominator int, defaultVal float64) float64 {
	if denominator <= 0 {
		return defaultVal
	}
	return 100 * float64(numerator) / float64(denominator)
}

func percent64(numerator, denominator int64, defaultVal float64) float64 {
	if denominator <= 0 {
		return defaultVal
	}
	return 100 * float64(numerator) / float64(denominator)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
