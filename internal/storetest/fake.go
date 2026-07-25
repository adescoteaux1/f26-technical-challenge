// Package storetest provides an in-memory store.Store implementation shared
// by unit tests across packages (evaluation, userauth, ...) so none of them
// need a live Postgres instance to exercise orchestration or auth logic.
//
// Simulations are round-tripped through JSON on every read/write, mirroring
// how PostgresStore actually persists them (as JSONB) rather than sharing
// live pointers. Without this, a test holding a reference returned by one
// call could observe mutations made by an unrelated later call — something
// that can't happen against the real database, so the fake shouldn't allow
// it either. Users have no nested pointers/slices, so plain value copies
// give the same isolation without needing JSON.
package storetest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/internal/store"
)

// FakeStore is a minimal, mutex-guarded in-memory implementation of store.Store.
type FakeStore struct {
	mu          sync.Mutex
	evaluations map[string]*store.EvaluationRow
	simulations map[string]map[int][]byte // JSON-encoded models.Simulation
	users       map[string]models.User    // keyed by user ID
}

func New() *FakeStore {
	return &FakeStore{
		evaluations: map[string]*store.EvaluationRow{},
		simulations: map[string]map[int][]byte{},
		users:       map[string]models.User{},
	}
}

func (f *FakeStore) CreateEvaluation(ctx context.Context, eval *models.Evaluation) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.evaluations[eval.ID] = &store.EvaluationRow{
		ID: eval.ID, UserID: eval.UserID, TotalSimulations: eval.TotalSimulations, CurrentSimulation: eval.CurrentSimulation,
		Finished: eval.Finished, OverallScore: eval.OverallScore, Metrics: eval.Metrics, ProfilePlan: eval.ProfilePlan,
	}
	return f.saveSimulationLocked(eval.Simulations[0])
}

func (f *FakeStore) GetEvaluation(ctx context.Context, id string) (*store.EvaluationRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	row, ok := f.evaluations[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyRow := *row
	return &copyRow, nil
}

func (f *FakeStore) GetSimulation(ctx context.Context, evaluationID string, number int) (*models.Simulation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.simulations[evaluationID][number]
	if !ok {
		return nil, store.ErrNotFound
	}
	var sim models.Simulation
	if err := json.Unmarshal(data, &sim); err != nil {
		return nil, err
	}
	return &sim, nil
}

func (f *FakeStore) SaveSimulation(ctx context.Context, sim *models.Simulation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveSimulationLocked(sim)
}

func (f *FakeStore) saveSimulationLocked(sim *models.Simulation) error {
	data, err := json.Marshal(sim)
	if err != nil {
		return err
	}
	if f.simulations[sim.EvaluationID] == nil {
		f.simulations[sim.EvaluationID] = map[int][]byte{}
	}
	f.simulations[sim.EvaluationID][sim.Number] = data
	return nil
}

func (f *FakeStore) AdvanceEvaluation(ctx context.Context, evaluationID string, nextSimulation int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.evaluations[evaluationID].CurrentSimulation = nextSimulation
	return nil
}

func (f *FakeStore) FinishEvaluation(ctx context.Context, evaluationID string, overallScore float64, metrics models.Metrics) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	row := f.evaluations[evaluationID]
	row.Finished = true
	row.OverallScore = overallScore
	row.Metrics = metrics
	return nil
}

func (f *FakeStore) SimulationScores(ctx context.Context, evaluationID string) ([]store.SimScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []store.SimScore
	for number, data := range f.simulations[evaluationID] {
		var sim models.Simulation
		if err := json.Unmarshal(data, &sim); err != nil {
			return nil, err
		}
		out = append(out, store.SimScore{Number: number, Score: sim.Score, Metrics: sim.Metrics, Finished: sim.Finished})
	}
	return out, nil
}

func (f *FakeStore) ListEvaluationsForUser(ctx context.Context, userID string) ([]store.EvaluationSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []store.EvaluationSummary
	for _, row := range f.evaluations {
		if row.UserID != userID {
			continue
		}
		out = append(out, store.EvaluationSummary{ID: row.ID, Finished: row.Finished, OverallScore: row.OverallScore, Metrics: row.Metrics})
	}
	return out, nil
}

func (f *FakeStore) CreateUser(ctx context.Context, user *models.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, u := range f.users {
		if u.Email == user.Email {
			return store.ErrAlreadyExists
		}
	}
	f.users[user.ID] = *user
	return nil
}

func (f *FakeStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, u := range f.users {
		if u.Email == email {
			copyUser := u
			return &copyUser, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *FakeStore) GetUserByToken(ctx context.Context, token string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, u := range f.users {
		if u.Token == token {
			copyUser := u
			return &copyUser, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *FakeStore) SetUserToken(ctx context.Context, userID, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	u, ok := f.users[userID]
	if !ok {
		return store.ErrNotFound
	}
	u.Token = token
	f.users[userID] = u
	return nil
}

func (f *FakeStore) Close() {}
