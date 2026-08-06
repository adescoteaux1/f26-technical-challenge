CREATE TABLE IF NOT EXISTS expeditions (
    id                 TEXT PRIMARY KEY,
    total_cycles       INT NOT NULL,
    current_cycle      INT NOT NULL DEFAULT 1,
    finished           BOOLEAN NOT NULL DEFAULT FALSE,
    overall_score      DOUBLE PRECISION NOT NULL DEFAULT 0,
    metrics            JSONB NOT NULL DEFAULT '{}'::jsonb,
    profile_plan       JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cycles (
    expedition_id     TEXT NOT NULL REFERENCES expeditions(id) ON DELETE CASCADE,
    cycle_number      INT NOT NULL,
    profile           TEXT NOT NULL,
    seed              BIGINT NOT NULL,
    finished          BOOLEAN NOT NULL DEFAULT FALSE,
    score             DOUBLE PRECISION NOT NULL DEFAULT 0,
    metrics           JSONB NOT NULL DEFAULT '{}'::jsonb,
    state             JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (expedition_id, cycle_number)
);
