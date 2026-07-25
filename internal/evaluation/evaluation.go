// Package evaluation is the Evaluation Engine: it orchestrates the
// generator, simulation engine, validator, and scoring engine behind the
// three REST endpoints. Handlers in internal/api call into this package and
// never touch the generator/engine/scoring packages directly.
package evaluation

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/adescoteaux1/generate-oracle/internal/engine"
	"github.com/adescoteaux1/generate-oracle/internal/generator"
	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/scoring"
	"github.com/adescoteaux1/generate-oracle/internal/store"
)

// DefaultTotalSimulations mirrors the 8-simulation example in the Oracle
// spec. With 6 workload profiles, this guarantees every profile appears at
// least once per evaluation, with 2 extra draws for additional variety.
const DefaultTotalSimulations = 8

// ErrForbidden is returned when the authenticated caller does not own the
// evaluation they're trying to read or schedule against.
var ErrForbidden = errors.New("you do not own this evaluation")

// Create starts a new evaluation owned by userID: it samples a profile order
// across the 6 workload profiles, generates the first simulation, and
// persists both.
func Create(ctx context.Context, st store.Store, userID string) (*models.Evaluation, error) {
	id := uuid.NewString()
	profiles := sampleProfileOrder(DefaultTotalSimulations)

	eval := &models.Evaluation{
		ID:                id,
		UserID:            userID,
		TotalSimulations:  len(profiles),
		CurrentSimulation: 1,
		ProfilePlan:       profiles,
	}

	sim, err := buildSimulation(id, 1, profiles[0])
	if err != nil {
		return nil, err
	}
	eval.Simulations = []*models.Simulation{sim}

	if err := st.CreateEvaluation(ctx, eval); err != nil {
		return nil, fmt.Errorf("persist new evaluation: %w", err)
	}
	return eval, nil
}

// GetState loads the evaluation summary and, if not yet finished, the full
// state of its current simulation. Returns ErrForbidden if userID does not
// own the evaluation.
func GetState(ctx context.Context, st store.Store, evaluationID, userID string) (*store.EvaluationRow, *models.Simulation, error) {
	row, err := st.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return nil, nil, err
	}
	if row.UserID != userID {
		return nil, nil, ErrForbidden
	}
	if row.Finished {
		return row, nil, nil
	}
	sim, err := st.GetSimulation(ctx, evaluationID, row.CurrentSimulation)
	if err != nil {
		return nil, nil, err
	}
	return row, sim, nil
}

// SubmitResult carries everything the API layer needs to build a response.
type SubmitResult struct {
	Evaluation *store.EvaluationRow
	Simulation *models.Simulation // nil once the whole evaluation is finished
	Rejected   []engine.RejectedAssignment
}

// Submit validates and applies one batch of scheduling decisions against the
// evaluation's current simulation, advances the simulation clock by one
// tick, and — when that simulation finishes — either rolls over to the next
// profile in the sequence or finalizes the evaluation's aggregate score.
func Submit(ctx context.Context, st store.Store, evaluationID, userID string, assignments []models.Assignment) (*SubmitResult, error) {
	row, err := st.GetEvaluation(ctx, evaluationID)
	if err != nil {
		return nil, err
	}
	if row.UserID != userID {
		return nil, ErrForbidden
	}
	if row.Finished {
		return &SubmitResult{Evaluation: row}, nil
	}

	sim, err := st.GetSimulation(ctx, evaluationID, row.CurrentSimulation)
	if err != nil {
		return nil, err
	}

	scheduleResult := engine.ValidateAndApply(sim, assignments)
	engine.AdvanceTick(sim)
	sim.Metrics, sim.Score = scoring.Compute(sim)

	if err := st.SaveSimulation(ctx, sim); err != nil {
		return nil, fmt.Errorf("persist simulation: %w", err)
	}

	if !sim.Finished {
		row.CurrentSimulation = sim.Number
		return &SubmitResult{Evaluation: row, Simulation: sim, Rejected: scheduleResult.Rejected}, nil
	}

	return advancePastFinishedSimulation(ctx, st, row, sim, scheduleResult.Rejected)
}

func advancePastFinishedSimulation(
	ctx context.Context, st store.Store, row *store.EvaluationRow, finishedSim *models.Simulation, rejected []engine.RejectedAssignment,
) (*SubmitResult, error) {
	if row.CurrentSimulation >= row.TotalSimulations {
		scores, err := st.SimulationScores(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		metrics, overall := aggregate(scores)
		if err := st.FinishEvaluation(ctx, row.ID, overall, metrics); err != nil {
			return nil, err
		}
		row.Finished = true
		row.OverallScore = overall
		row.Metrics = metrics
		return &SubmitResult{Evaluation: row, Rejected: rejected}, nil
	}

	nextNumber := row.CurrentSimulation + 1
	profile := row.ProfilePlan[nextNumber-1]
	nextSim, err := buildSimulation(row.ID, nextNumber, profile)
	if err != nil {
		return nil, err
	}
	if err := st.SaveSimulation(ctx, nextSim); err != nil {
		return nil, err
	}
	if err := st.AdvanceEvaluation(ctx, row.ID, nextNumber); err != nil {
		return nil, err
	}
	row.CurrentSimulation = nextNumber
	return &SubmitResult{Evaluation: row, Simulation: nextSim, Rejected: rejected}, nil
}

func aggregate(scores []store.SimScore) (models.Metrics, float64) {
	sims := make([]*models.Simulation, 0, len(scores))
	for _, s := range scores {
		sims = append(sims, &models.Simulation{Score: s.Score, Metrics: s.Metrics})
	}
	return scoring.AggregateOverall(sims)
}

func buildSimulation(evaluationID string, number int, profile string) (*models.Simulation, error) {
	seed := rand.New(rand.NewSource(time.Now().UnixNano() + int64(number)*7919)).Int63()
	sim, err := generator.Generate(profile, seed)
	if err != nil {
		return nil, err
	}
	sim.EvaluationID = evaluationID
	sim.Number = number
	sim.Metrics, sim.Score = scoring.Compute(sim)
	return sim, nil
}

// sampleProfileOrder guarantees every one of the 6 profiles is drawn at
// least once, then fills any remaining slots with additional random draws,
// and finally shuffles the order so applicants can't infer what's coming.
func sampleProfileOrder(total int) []string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	order := append([]string{}, generator.AllProfiles...)
	for len(order) < total {
		order = append(order, generator.AllProfiles[rng.Intn(len(generator.AllProfiles))])
	}
	order = order[:total]

	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}
