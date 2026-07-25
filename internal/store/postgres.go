package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adescoteaux1/generate-oracle/internal/models"
	"github.com/adescoteaux1/generate-oracle/migrations"
)

// PostgresStore is the Supabase/Postgres-backed Store implementation. Full
// simulation state is stored as a JSONB document per (evaluation, simulation
// number) row rather than normalized across many tables: the state is deeply
// nested (jobs, workers, per-project stats) and only ever read/written as a
// whole, so a document column is simpler than a join-heavy schema while
// still living in a real relational database for evaluation-level queries.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects to Postgres and runs the embedded migrations.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	for _, stmt := range migrations.All() {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) CreateEvaluation(ctx context.Context, eval *models.Evaluation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	metricsJSON, err := json.Marshal(eval.Metrics)
	if err != nil {
		return err
	}
	profilePlanJSON, err := json.Marshal(eval.ProfilePlan)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO evaluations (id, user_id, total_simulations, current_simulation, finished, overall_score, metrics, profile_plan)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		eval.ID, eval.UserID, eval.TotalSimulations, eval.CurrentSimulation, eval.Finished, eval.OverallScore, metricsJSON, profilePlanJSON)
	if err != nil {
		return fmt.Errorf("insert evaluation: %w", err)
	}

	if len(eval.Simulations) != 1 {
		return fmt.Errorf("CreateEvaluation expects exactly the first simulation attached, got %d", len(eval.Simulations))
	}
	if err := saveSimulationTx(ctx, tx, eval.Simulations[0]); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) GetEvaluation(ctx context.Context, id string) (*EvaluationRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, total_simulations, current_simulation, finished, overall_score, metrics, profile_plan
		FROM evaluations WHERE id = $1`, id)

	var r EvaluationRow
	var userID *string
	var metricsJSON, profilePlanJSON []byte
	if err := row.Scan(&r.ID, &userID, &r.TotalSimulations, &r.CurrentSimulation, &r.Finished, &r.OverallScore, &metricsJSON, &profilePlanJSON); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query evaluation: %w", err)
	}
	if userID != nil {
		r.UserID = *userID
	}
	if err := json.Unmarshal(metricsJSON, &r.Metrics); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(profilePlanJSON, &r.ProfilePlan); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *PostgresStore) GetSimulation(ctx context.Context, evaluationID string, number int) (*models.Simulation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT state FROM simulations WHERE evaluation_id = $1 AND simulation_number = $2`,
		evaluationID, number)

	var stateJSON []byte
	if err := row.Scan(&stateJSON); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query simulation: %w", err)
	}
	var sim models.Simulation
	if err := json.Unmarshal(stateJSON, &sim); err != nil {
		return nil, fmt.Errorf("unmarshal simulation state: %w", err)
	}
	return &sim, nil
}

func (s *PostgresStore) SaveSimulation(ctx context.Context, sim *models.Simulation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := saveSimulationTx(ctx, tx, sim); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func saveSimulationTx(ctx context.Context, tx pgx.Tx, sim *models.Simulation) error {
	stateJSON, err := json.Marshal(sim)
	if err != nil {
		return fmt.Errorf("marshal simulation state: %w", err)
	}
	metricsJSON, err := json.Marshal(sim.Metrics)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO simulations (evaluation_id, simulation_number, profile, seed, finished, score, metrics, state, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (evaluation_id, simulation_number)
		DO UPDATE SET finished = $5, score = $6, metrics = $7, state = $8, updated_at = now()`,
		sim.EvaluationID, sim.Number, sim.Profile, sim.Seed, sim.Finished, sim.Score, metricsJSON, stateJSON)
	if err != nil {
		return fmt.Errorf("upsert simulation: %w", err)
	}
	return nil
}

func (s *PostgresStore) AdvanceEvaluation(ctx context.Context, evaluationID string, nextSimulation int) error {
	_, err := s.pool.Exec(ctx, `UPDATE evaluations SET current_simulation = $2 WHERE id = $1`, evaluationID, nextSimulation)
	return err
}

func (s *PostgresStore) FinishEvaluation(ctx context.Context, evaluationID string, overallScore float64, metrics models.Metrics) error {
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE evaluations SET finished = TRUE, overall_score = $2, metrics = $3 WHERE id = $1`,
		evaluationID, overallScore, metricsJSON)
	return err
}

func (s *PostgresStore) SimulationScores(ctx context.Context, evaluationID string) ([]SimScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT simulation_number, score, metrics, finished
		FROM simulations WHERE evaluation_id = $1 ORDER BY simulation_number`, evaluationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SimScore
	for rows.Next() {
		var s SimScore
		var metricsJSON []byte
		if err := rows.Scan(&s.Number, &s.Score, &metricsJSON, &s.Finished); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metricsJSON, &s.Metrics); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListEvaluationsForUser(ctx context.Context, userID string) ([]EvaluationSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, finished, overall_score, metrics, created_at
		FROM evaluations WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EvaluationSummary
	for rows.Next() {
		var e EvaluationSummary
		var metricsJSON []byte
		if err := rows.Scan(&e.ID, &e.Finished, &e.OverallScore, &metricsJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metricsJSON, &e.Metrics); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateUser(ctx context.Context, user *models.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, email, nuid, token, created_at) VALUES ($1, $2, $3, $4, $5)`,
		user.ID, user.Email, user.NUID, user.Token, user.CreatedAt)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return ErrAlreadyExists
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return s.scanUser(ctx, `SELECT id, email, nuid, token, created_at FROM users WHERE email = $1`, email)
}

func (s *PostgresStore) GetUserByToken(ctx context.Context, token string) (*models.User, error) {
	return s.scanUser(ctx, `SELECT id, email, nuid, token, created_at FROM users WHERE token = $1`, token)
}

func (s *PostgresStore) scanUser(ctx context.Context, query string, arg string) (*models.User, error) {
	row := s.pool.QueryRow(ctx, query, arg)
	var u models.User
	if err := row.Scan(&u.ID, &u.Email, &u.NUID, &u.Token, &u.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &u, nil
}

func (s *PostgresStore) SetUserToken(ctx context.Context, userID, token string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET token = $2 WHERE id = $1`, userID, token)
	return err
}
