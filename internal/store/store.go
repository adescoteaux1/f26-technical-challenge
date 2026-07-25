// Package store persists evaluation, simulation, and user state between HTTP
// requests. The Oracle is stateless per-request, so every tick's state must
// round-trip through here.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// ErrNotFound is returned when an evaluation, simulation, or user does not exist.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists is returned when registering an email that's already taken.
var ErrAlreadyExists = errors.New("already exists")

// EvaluationRow is the evaluation-level summary row (excludes full simulation state).
type EvaluationRow struct {
	ID                string
	UserID            string
	TotalSimulations  int
	CurrentSimulation int
	Finished          bool
	OverallScore      float64
	Metrics           models.Metrics
	ProfilePlan       []string
}

// SimScore is the lightweight per-simulation result used for aggregation.
type SimScore struct {
	Number   int
	Score    float64
	Metrics  models.Metrics
	Finished bool
}

// EvaluationSummary is one row of a user's evaluation history.
type EvaluationSummary struct {
	ID           string
	Finished     bool
	OverallScore float64
	Metrics      models.Metrics
	CreatedAt    time.Time
}

// Store is the persistence boundary the evaluation engine depends on. Kept
// as an interface so the Postgres implementation can be swapped (e.g. for an
// in-memory fake in tests) without touching orchestration logic.
type Store interface {
	// CreateEvaluation persists a brand new evaluation row and its first simulation.
	CreateEvaluation(ctx context.Context, eval *models.Evaluation) error

	// GetEvaluation returns the evaluation-level summary row.
	GetEvaluation(ctx context.Context, id string) (*EvaluationRow, error)

	// GetSimulation loads the full state of one simulation.
	GetSimulation(ctx context.Context, evaluationID string, number int) (*models.Simulation, error)

	// SaveSimulation upserts a simulation's full state plus its score/metrics summary.
	SaveSimulation(ctx context.Context, sim *models.Simulation) error

	// AdvanceEvaluation moves the evaluation to the next simulation number.
	AdvanceEvaluation(ctx context.Context, evaluationID string, nextSimulation int) error

	// FinishEvaluation marks the evaluation complete with its final aggregate score.
	FinishEvaluation(ctx context.Context, evaluationID string, overallScore float64, metrics models.Metrics) error

	// SimulationScores lists per-simulation results for aggregation.
	SimulationScores(ctx context.Context, evaluationID string) ([]SimScore, error)

	// ListEvaluationsForUser returns a user's evaluation history, newest first.
	ListEvaluationsForUser(ctx context.Context, userID string) ([]EvaluationSummary, error)

	// CreateUser persists a new user. Returns ErrAlreadyExists if the email is taken.
	CreateUser(ctx context.Context, user *models.User) error

	// GetUserByEmail looks up a user for login (email + NUID verification).
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)

	// GetUserByToken looks up the caller for an authenticated request.
	GetUserByToken(ctx context.Context, token string) (*models.User, error)

	// SetUserToken rotates a user's bearer token (used on login).
	SetUserToken(ctx context.Context, userID, token string) error

	Close()
}
