// Package store persists expedition, cycle, and user state between HTTP
// requests. The Oracle is stateless per-request, so every tick's state must
// round-trip through here.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// ErrNotFound is returned when an expedition, cycle, or user does not exist.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists is returned when registering an email that's already taken.
var ErrAlreadyExists = errors.New("already exists")

// ExpeditionRow is the expedition-level summary row (excludes full cycle state).
type ExpeditionRow struct {
	ID           string
	UserID       string
	TotalCycles  int
	CurrentCycle int
	Finished     bool
	OverallScore float64
	Metrics      models.Metrics
	ProfilePlan  []string
}

// CycleScore is the lightweight per-cycle result used for aggregation.
type CycleScore struct {
	Number   int
	Score    float64
	Metrics  models.Metrics
	Finished bool
}

// ExpeditionSummary is one row of a user's expedition history.
type ExpeditionSummary struct {
	ID           string
	Finished     bool
	OverallScore float64
	Metrics      models.Metrics
	CreatedAt    time.Time
}

// Store is the persistence boundary the expedition engine depends on. Kept
// as an interface so the Postgres implementation can be swapped (e.g. for an
// in-memory fake in tests) without touching orchestration logic.
type Store interface {
	// CreateExpedition persists a brand new expedition row and its first cycle.
	CreateExpedition(ctx context.Context, exp *models.Expedition) error

	// GetExpedition returns the expedition-level summary row.
	GetExpedition(ctx context.Context, id string) (*ExpeditionRow, error)

	// GetCycle loads the full state of one cycle.
	GetCycle(ctx context.Context, expeditionID string, number int) (*models.Cycle, error)

	// SaveCycle upserts a cycle's full state plus its score/metrics summary.
	SaveCycle(ctx context.Context, cycle *models.Cycle) error

	// AdvanceExpedition moves the expedition to the next cycle number.
	AdvanceExpedition(ctx context.Context, expeditionID string, nextCycle int) error

	// FinishExpedition marks the expedition complete with its final aggregate score.
	FinishExpedition(ctx context.Context, expeditionID string, overallScore float64, metrics models.Metrics) error

	// CycleScores lists per-cycle results for aggregation.
	CycleScores(ctx context.Context, expeditionID string) ([]CycleScore, error)

	// ListExpeditionsForUser returns a user's expedition history, newest first.
	ListExpeditionsForUser(ctx context.Context, userID string) ([]ExpeditionSummary, error)

	// CreateUser persists a new user. Returns ErrAlreadyExists if the email is taken.
	CreateUser(ctx context.Context, user *models.User) error

	// GetUserByEmail looks up a user for login (email + NUID verification).
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)

	// GetUserByToken looks up the caller for an authenticated request.
	GetUserByToken(ctx context.Context, token string) (*models.User, error)

	// SetUserToken rotates a user's bearer token (used on login).
	SetUserToken(ctx context.Context, userID, token string) error

	// WithExpeditionLock runs fn while holding an exclusive lock scoped to
	// expeditionID. Submit's read-modify-write (GetExpedition, GetCycle,
	// SaveCycle, AdvanceExpedition/FinishExpedition) spans several store
	// calls with no single transaction tying them together, so two
	// overlapping schedule submissions for the same expedition (a retried
	// request racing the original, for example) could otherwise both read
	// the same tick and the second's save would silently clobber the
	// first's. Locks for different expeditions never contend with each
	// other.
	WithExpeditionLock(ctx context.Context, expeditionID string, fn func(ctx context.Context) error) error

	Close()
}
