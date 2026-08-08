package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
	"github.com/adescoteaux1/generate-control-tower/internal/supabase"
)

// PostgresStore is the Supabase/Postgres-backed Store implementation. Full
// cycle state is stored as a JSONB document per (expedition, cycle
// number) row rather than normalized across many tables: the state is deeply
// nested (voyages, gates, per-hub stats) and only ever read/written as a
// whole, so a document column is simpler than a join-heavy schema while
// still living in a real relational database for expedition-level queries.
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
	stmts, err := supabase.Migrations()
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) CreateExpedition(ctx context.Context, exp *models.Expedition) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	metricsJSON, err := json.Marshal(exp.Metrics)
	if err != nil {
		return err
	}
	profilePlanJSON, err := json.Marshal(exp.ProfilePlan)
	if err != nil {
		return err
	}
	// JSONB params are passed as string, not []byte: under the simple query
	// protocol (needed for transaction-mode connection poolers like
	// Supavisor), pgx encodes a []byte arg as a bytea literal, which
	// Postgres then can't parse as JSON for a jsonb column. A string arg is
	// sent as a text literal instead, which casts to jsonb correctly either way.
	_, err = tx.Exec(ctx, `
		INSERT INTO expeditions (id, user_id, total_cycles, current_cycle, finished, overall_score, metrics, profile_plan)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		exp.ID, exp.UserID, exp.TotalCycles, exp.CurrentCycle, exp.Finished, exp.OverallScore, string(metricsJSON), string(profilePlanJSON))
	if err != nil {
		return fmt.Errorf("insert expedition: %w", err)
	}

	if len(exp.Cycles) != 1 {
		return fmt.Errorf("CreateExpedition expects exactly the first cycle attached, got %d", len(exp.Cycles))
	}
	if err := saveCycleTx(ctx, tx, exp.Cycles[0]); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) GetExpedition(ctx context.Context, id string) (*ExpeditionRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, total_cycles, current_cycle, finished, overall_score, metrics, profile_plan
		FROM expeditions WHERE id = $1`, id)

	var r ExpeditionRow
	var userID *string
	var metricsJSON, profilePlanJSON []byte
	if err := row.Scan(&r.ID, &userID, &r.TotalCycles, &r.CurrentCycle, &r.Finished, &r.OverallScore, &metricsJSON, &profilePlanJSON); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query expedition: %w", err)
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

func (s *PostgresStore) GetCycle(ctx context.Context, expeditionID string, number int) (*models.Cycle, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT state FROM cycles WHERE expedition_id = $1 AND cycle_number = $2`,
		expeditionID, number)

	var stateJSON []byte
	if err := row.Scan(&stateJSON); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query cycle: %w", err)
	}
	var cycle models.Cycle
	if err := json.Unmarshal(stateJSON, &cycle); err != nil {
		return nil, fmt.Errorf("unmarshal cycle state: %w", err)
	}
	return &cycle, nil
}

func (s *PostgresStore) SaveCycle(ctx context.Context, cycle *models.Cycle) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := saveCycleTx(ctx, tx, cycle); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func saveCycleTx(ctx context.Context, tx pgx.Tx, cycle *models.Cycle) error {
	stateJSON, err := json.Marshal(cycle)
	if err != nil {
		return fmt.Errorf("marshal cycle state: %w", err)
	}
	metricsJSON, err := json.Marshal(cycle.Metrics)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO cycles (expedition_id, cycle_number, profile, seed, finished, score, metrics, state, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (expedition_id, cycle_number)
		DO UPDATE SET finished = $5, score = $6, metrics = $7, state = $8, updated_at = now()`,
		cycle.ExpeditionID, cycle.Number, cycle.Profile, cycle.Seed, cycle.Finished, cycle.Score, string(metricsJSON), string(stateJSON))
	if err != nil {
		return fmt.Errorf("upsert cycle: %w", err)
	}
	return nil
}

// WithExpeditionLock serializes concurrent Submit calls against the same
// expedition using a Postgres transaction-scoped advisory lock, keyed by
// expeditionID, held on a dedicated pooled connection for the duration of
// fn. The lock is released automatically when the transaction ends
// (commit on success, rollback otherwise), regardless of how fn returns.
func (s *PostgresStore) WithExpeditionLock(ctx context.Context, expeditionID string, fn func(ctx context.Context) error) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for expedition lock: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin expedition lock transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed; releases the advisory lock on any early return

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, expeditionID); err != nil {
		return fmt.Errorf("acquire expedition lock: %w", err)
	}

	if err := fn(ctx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) AdvanceExpedition(ctx context.Context, expeditionID string, nextCycle int) error {
	_, err := s.pool.Exec(ctx, `UPDATE expeditions SET current_cycle = $2 WHERE id = $1`, expeditionID, nextCycle)
	return err
}

func (s *PostgresStore) FinishExpedition(ctx context.Context, expeditionID string, overallScore float64, metrics models.Metrics) error {
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE expeditions SET finished = TRUE, overall_score = $2, metrics = $3 WHERE id = $1`,
		expeditionID, overallScore, string(metricsJSON))
	return err
}

func (s *PostgresStore) CycleScores(ctx context.Context, expeditionID string) ([]CycleScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cycle_number, score, metrics, finished
		FROM cycles WHERE expedition_id = $1 ORDER BY cycle_number`, expeditionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CycleScore
	for rows.Next() {
		var c CycleScore
		var metricsJSON []byte
		if err := rows.Scan(&c.Number, &c.Score, &metricsJSON, &c.Finished); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metricsJSON, &c.Metrics); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListExpeditionsForUser(ctx context.Context, userID string) ([]ExpeditionSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, finished, overall_score, metrics, created_at
		FROM expeditions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExpeditionSummary
	for rows.Next() {
		var e ExpeditionSummary
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
