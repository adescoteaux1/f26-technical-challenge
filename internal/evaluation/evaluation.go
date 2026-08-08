// Package evaluation is the Expedition Engine: it orchestrates the
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

// DefaultTotalCycles is how many cycles make up one expedition. With 6
// workload profiles, 16 gives two full guaranteed passes through every
// profile plus a partial third pass (see sampleProfileOrder) — enough
// repetition that a single unlucky/lucky profile draw doesn't dominate the
// averaged overallScore the way it could at lower cycle counts.
const DefaultTotalCycles = 16

// ErrForbidden is returned when the authenticated caller does not own the
// expedition they're trying to read or schedule against.
var ErrForbidden = errors.New("you do not own this expedition")

// Create starts a new expedition owned by userID: it samples a profile order
// across the 6 workload profiles, generates the first cycle, and
// persists both.
func Create(ctx context.Context, st store.Store, userID string) (*models.Expedition, error) {
	id := uuid.NewString()
	profiles := sampleProfileOrder(DefaultTotalCycles)

	exp := &models.Expedition{
		ID:           id,
		UserID:       userID,
		TotalCycles:  len(profiles),
		CurrentCycle: 1,
		ProfilePlan:  profiles,
	}

	cycle, err := buildCycle(id, 1, profiles[0])
	if err != nil {
		return nil, err
	}
	exp.Cycles = []*models.Cycle{cycle}

	if err := st.CreateExpedition(ctx, exp); err != nil {
		return nil, fmt.Errorf("persist new expedition: %w", err)
	}
	return exp, nil
}

// GetState loads the expedition summary and, if not yet finished, the full
// state of its current cycle. Returns ErrForbidden if userID does not
// own the expedition.
func GetState(ctx context.Context, st store.Store, expeditionID, userID string) (*store.ExpeditionRow, *models.Cycle, error) {
	row, err := st.GetExpedition(ctx, expeditionID)
	if err != nil {
		return nil, nil, err
	}
	if row.UserID != userID {
		return nil, nil, ErrForbidden
	}
	if row.Finished {
		return row, nil, nil
	}
	cycle, err := st.GetCycle(ctx, expeditionID, row.CurrentCycle)
	if err != nil {
		return nil, nil, err
	}
	return row, cycle, nil
}

// SubmitResult carries everything the API layer needs to build a response.
type SubmitResult struct {
	Expedition *store.ExpeditionRow
	Cycle      *models.Cycle // nil once the whole expedition is finished
	Rejected   []engine.RejectedAssignment
}

// Submit validates and applies one batch of scheduling decisions against the
// expedition's current cycle, advances the cycle clock by one
// tick, and — when that cycle finishes — either rolls over to the next
// profile in the sequence or finalizes the expedition's aggregate score.
//
// The whole read-modify-write runs under st.WithExpeditionLock: it spans
// several independent store calls (GetExpedition, GetCycle, SaveCycle,
// AdvanceExpedition/FinishExpedition), and without a lock scoped to this
// expedition, two overlapping submissions (e.g. a retried request racing
// the original) could both read the same tick and the later save would
// silently clobber the earlier one's result.
func Submit(ctx context.Context, st store.Store, expeditionID, userID string, assignments []models.Assignment) (*SubmitResult, error) {
	var result *SubmitResult
	err := st.WithExpeditionLock(ctx, expeditionID, func(ctx context.Context) error {
		row, err := st.GetExpedition(ctx, expeditionID)
		if err != nil {
			return err
		}
		if row.UserID != userID {
			return ErrForbidden
		}
		if row.Finished {
			result = &SubmitResult{Expedition: row}
			return nil
		}

		cycle, err := st.GetCycle(ctx, expeditionID, row.CurrentCycle)
		if err != nil {
			return err
		}

		scheduleResult := engine.ValidateAndApply(cycle, assignments)
		engine.AdvanceTick(cycle)
		cycle.Metrics, cycle.Score = scoring.Compute(cycle)

		if err := st.SaveCycle(ctx, cycle); err != nil {
			return fmt.Errorf("persist cycle: %w", err)
		}

		if !cycle.Finished {
			row.CurrentCycle = cycle.Number
			result = &SubmitResult{Expedition: row, Cycle: cycle, Rejected: scheduleResult.Rejected}
			return nil
		}

		result, err = advancePastFinishedCycle(ctx, st, row, cycle, scheduleResult.Rejected)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func advancePastFinishedCycle(
	ctx context.Context, st store.Store, row *store.ExpeditionRow, finishedCycle *models.Cycle, rejected []engine.RejectedAssignment,
) (*SubmitResult, error) {
	if row.CurrentCycle >= row.TotalCycles {
		scores, err := st.CycleScores(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		metrics, overall := aggregate(scores)
		if err := st.FinishExpedition(ctx, row.ID, overall, metrics); err != nil {
			return nil, err
		}
		row.Finished = true
		row.OverallScore = overall
		row.Metrics = metrics
		return &SubmitResult{Expedition: row, Rejected: rejected}, nil
	}

	nextNumber := row.CurrentCycle + 1
	profile := row.ProfilePlan[nextNumber-1]
	nextCycle, err := buildCycle(row.ID, nextNumber, profile)
	if err != nil {
		return nil, err
	}
	if err := st.SaveCycle(ctx, nextCycle); err != nil {
		return nil, err
	}
	if err := st.AdvanceExpedition(ctx, row.ID, nextNumber); err != nil {
		return nil, err
	}
	row.CurrentCycle = nextNumber
	return &SubmitResult{Expedition: row, Cycle: nextCycle, Rejected: rejected}, nil
}

func aggregate(scores []store.CycleScore) (models.Metrics, float64) {
	cycles := make([]*models.Cycle, 0, len(scores))
	for _, s := range scores {
		cycles = append(cycles, &models.Cycle{Score: s.Score, Metrics: s.Metrics})
	}
	return scoring.AggregateOverall(cycles)
}

func buildCycle(expeditionID string, number int, profile string) (*models.Cycle, error) {
	seed := rand.New(rand.NewSource(time.Now().UnixNano() + int64(number)*7919)).Int63()
	cycle, err := generator.Generate(profile, seed)
	if err != nil {
		return nil, err
	}
	cycle.ExpeditionID = expeditionID
	cycle.Number = number
	cycle.Metrics, cycle.Score = scoring.Compute(cycle)
	return cycle, nil
}

// sampleProfileOrder spreads `total` draws as evenly as possible across the
// 6 profiles, then shuffles the order so applicants can't infer what's
// coming. Full passes through every profile are repeated as many times as
// fit, and only the leftover remainder is drawn (without replacement, so it
// can't double up) from a shuffled partial pass. This is deliberately not
// "6 guaranteed + N uniform-random draws": pure random draws for the extras
// let one profile occasionally get 3-4x the representation of another by
// chance, which is exactly the kind of profile-draw noise that made two
// single evaluations hard to compare fairly.
func sampleProfileOrder(total int) []string {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	profiles := generator.AllProfiles

	order := make([]string, 0, total)
	for len(order)+len(profiles) <= total {
		order = append(order, profiles...)
	}

	if remaining := total - len(order); remaining > 0 {
		partial := append([]string{}, profiles...)
		rng.Shuffle(len(partial), func(i, j int) { partial[i], partial[j] = partial[j], partial[i] })
		order = append(order, partial[:remaining]...)
	}

	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}
