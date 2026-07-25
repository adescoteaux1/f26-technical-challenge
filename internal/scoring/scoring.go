// Package scoring turns the accumulated SimStats into the metrics and
// overall score reported to the client. Metrics are recomputed from the
// running totals on every request, so GET /evaluation/{id} always reflects
// current progress rather than only a final snapshot.
package scoring

import (
	"math"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// Weights used to combine category scores into the overall score. These are
// intentionally not exposed through the API (mirrors "the exact scoring
// formula is intentionally not published"), but are centralized here so
// they're easy to tune during Oracle development.
const (
	weightThroughput  = 0.25
	weightUtilization = 0.20
	weightDeadline    = 0.25
	weightFairness    = 0.15
	weightReliability = 0.15
)

// Compute derives the current Metrics + overall score from a simulation's
// accumulated stats. Safe to call at any tick, including tick 0.
func Compute(sim *models.Simulation) (models.Metrics, float64) {
	s := sim.Stats

	throughput := percent(s.CompletedJobs, s.TotalJobs, 0)
	utilization := percent64(s.WorkerBusyResourceTicks, s.WorkerTotalResourceTicks, 0)
	deadlineSuccess := percent(s.DeadlinesMet, s.DeadlinesMet+s.DeadlinesMissed, 100)
	fairness := fairnessScore(s)
	reliability := percent(s.ValidAssignments, s.ValidAssignments+s.InvalidAssignments, 100)

	metrics := models.Metrics{
		Throughput:        round1(throughput),
		WorkerUtilization: round1(utilization),
		DeadlineSuccess:   round1(deadlineSuccess),
		Fairness:          round1(fairness),
		Reliability:       round1(reliability),
	}

	overall := weightThroughput*throughput +
		weightUtilization*utilization +
		weightDeadline*deadlineSuccess +
		weightFairness*fairness +
		weightReliability*reliability

	return metrics, round1(overall)
}

// fairnessScore penalizes large disparities in average queue wait time across
// projects: a scheduler that always favors one project and starves another
// scores lower here even if throughput is high.
func fairnessScore(s models.SimStats) float64 {
	var avgWaits []float64
	for project, completions := range s.ProjectCompletions {
		if completions == 0 {
			continue
		}
		avgWaits = append(avgWaits, float64(s.ProjectWaitTicks[project])/float64(completions))
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

// AggregateOverall averages per-simulation scores/metrics into the
// evaluation-level result, matching the "average performance across every
// simulation" scoring philosophy rather than weighting any one workload
// more heavily than another.
func AggregateOverall(sims []*models.Simulation) (models.Metrics, float64) {
	if len(sims) == 0 {
		return models.Metrics{}, 0
	}
	var m models.Metrics
	var overall float64
	for _, sim := range sims {
		m.Throughput += sim.Metrics.Throughput
		m.WorkerUtilization += sim.Metrics.WorkerUtilization
		m.DeadlineSuccess += sim.Metrics.DeadlineSuccess
		m.Fairness += sim.Metrics.Fairness
		m.Reliability += sim.Metrics.Reliability
		overall += sim.Score
	}
	n := float64(len(sims))
	m.Throughput = round1(m.Throughput / n)
	m.WorkerUtilization = round1(m.WorkerUtilization / n)
	m.DeadlineSuccess = round1(m.DeadlineSuccess / n)
	m.Fairness = round1(m.Fairness / n)
	m.Reliability = round1(m.Reliability / n)
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
