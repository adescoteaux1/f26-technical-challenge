// Package storetest provides an in-memory store.Store implementation shared
// by unit tests across packages (evaluation, userauth, ...) so none of them
// need a live Postgres instance to exercise orchestration or auth logic.
//
// Cycles are round-tripped through JSON on every read/write, mirroring
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

	"github.com/adescoteaux1/generate-control-tower/internal/models"
	"github.com/adescoteaux1/generate-control-tower/internal/store"
)

// FakeStore is a minimal, mutex-guarded in-memory implementation of store.Store.
type FakeStore struct {
	mu          sync.Mutex
	expeditions map[string]*store.ExpeditionRow
	cycles      map[string]map[int][]byte // JSON-encoded models.Cycle
	users       map[string]models.User    // keyed by user ID

	// expeditionLocksMu guards expeditionLocks itself, kept separate from mu
	// (which guards the data above) so WithExpeditionLock can hold a
	// per-expedition lock for the duration of fn without deadlocking against
	// the data methods fn calls back into (sync.Mutex isn't reentrant).
	expeditionLocksMu sync.Mutex
	expeditionLocks   map[string]*sync.Mutex
}

func New() *FakeStore {
	return &FakeStore{
		expeditions: map[string]*store.ExpeditionRow{},
		cycles:      map[string]map[int][]byte{},
		users:       map[string]models.User{},
	}
}

func (f *FakeStore) CreateExpedition(ctx context.Context, exp *models.Expedition) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.expeditions[exp.ID] = &store.ExpeditionRow{
		ID: exp.ID, UserID: exp.UserID, TotalCycles: exp.TotalCycles, CurrentCycle: exp.CurrentCycle,
		Finished: exp.Finished, OverallScore: exp.OverallScore, Metrics: exp.Metrics, ProfilePlan: exp.ProfilePlan,
	}
	return f.saveCycleLocked(exp.Cycles[0])
}

func (f *FakeStore) GetExpedition(ctx context.Context, id string) (*store.ExpeditionRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	row, ok := f.expeditions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyRow := *row
	return &copyRow, nil
}

func (f *FakeStore) GetCycle(ctx context.Context, expeditionID string, number int) (*models.Cycle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, ok := f.cycles[expeditionID][number]
	if !ok {
		return nil, store.ErrNotFound
	}
	var cycle models.Cycle
	if err := json.Unmarshal(data, &cycle); err != nil {
		return nil, err
	}
	return &cycle, nil
}

func (f *FakeStore) SaveCycle(ctx context.Context, cycle *models.Cycle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveCycleLocked(cycle)
}

func (f *FakeStore) saveCycleLocked(cycle *models.Cycle) error {
	data, err := json.Marshal(cycle)
	if err != nil {
		return err
	}
	if f.cycles[cycle.ExpeditionID] == nil {
		f.cycles[cycle.ExpeditionID] = map[int][]byte{}
	}
	f.cycles[cycle.ExpeditionID][cycle.Number] = data
	return nil
}

func (f *FakeStore) AdvanceExpedition(ctx context.Context, expeditionID string, nextCycle int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.expeditions[expeditionID].CurrentCycle = nextCycle
	return nil
}

func (f *FakeStore) FinishExpedition(ctx context.Context, expeditionID string, overallScore float64, metrics models.Metrics) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	row := f.expeditions[expeditionID]
	row.Finished = true
	row.OverallScore = overallScore
	row.Metrics = metrics
	return nil
}

func (f *FakeStore) CycleScores(ctx context.Context, expeditionID string) ([]store.CycleScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []store.CycleScore
	for number, data := range f.cycles[expeditionID] {
		var cycle models.Cycle
		if err := json.Unmarshal(data, &cycle); err != nil {
			return nil, err
		}
		out = append(out, store.CycleScore{Number: number, Score: cycle.Score, Metrics: cycle.Metrics, Finished: cycle.Finished})
	}
	return out, nil
}

func (f *FakeStore) ListExpeditionsForUser(ctx context.Context, userID string) ([]store.ExpeditionSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []store.ExpeditionSummary
	for _, row := range f.expeditions {
		if row.UserID != userID {
			continue
		}
		out = append(out, store.ExpeditionSummary{ID: row.ID, Finished: row.Finished, OverallScore: row.OverallScore, Metrics: row.Metrics})
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

// WithExpeditionLock mirrors PostgresStore's advisory-lock behavior: fn runs
// while holding an exclusive lock scoped to expeditionID, so tests can
// exercise the same serialization guarantee without a live database. Locks
// for different expedition IDs never contend with each other.
func (f *FakeStore) WithExpeditionLock(ctx context.Context, expeditionID string, fn func(ctx context.Context) error) error {
	lock := f.expeditionLock(expeditionID)
	lock.Lock()
	defer lock.Unlock()
	return fn(ctx)
}

func (f *FakeStore) expeditionLock(expeditionID string) *sync.Mutex {
	f.expeditionLocksMu.Lock()
	defer f.expeditionLocksMu.Unlock()

	if f.expeditionLocks == nil {
		f.expeditionLocks = map[string]*sync.Mutex{}
	}
	lock, ok := f.expeditionLocks[expeditionID]
	if !ok {
		lock = &sync.Mutex{}
		f.expeditionLocks[expeditionID] = lock
	}
	return lock
}

func (f *FakeStore) Close() {}
