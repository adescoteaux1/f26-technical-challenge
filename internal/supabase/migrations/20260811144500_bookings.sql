-- Transit bookings rendered by the operations console's "My Upcoming Transits"
-- panel. Status is a stored fact here, not derived from live portal state, and
-- these portal names are intentionally separate from the six in
-- internal/portals -- the two panels are independent data sets.
CREATE TABLE IF NOT EXISTS bookings (
    reference     TEXT PRIMARY KEY,
    departs_at    TIMESTAMPTZ NOT NULL,
    destination   TEXT NOT NULL,
    portal        TEXT NOT NULL,
    load_percent  INT,
    status        TEXT NOT NULL,
    status_detail TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT bookings_status_valid CHECK (status IN ('cleared', 'queued', 'held', 'canceled')),
    CONSTRAINT bookings_load_range CHECK (load_percent IS NULL OR load_percent BETWEEN 0 AND 100)
);

CREATE INDEX IF NOT EXISTS idx_bookings_departs_at ON bookings(departs_at);

-- Seed data. departs_at is relative to when the row is first inserted, so the
-- list always reads as upcoming on a freshly provisioned database. ON CONFLICT
-- DO NOTHING keeps this safe to re-run, since every migration in this project
-- is applied on every startup.
INSERT INTO bookings (reference, departs_at, destination, portal, load_percent, status, status_detail)
SELECT
    v.reference,
    date_trunc('day', now()) + make_interval(days => v.day_offset) + v.depart_time::interval,
    v.destination,
    v.portal,
    v.load_percent,
    v.status,
    v.status_detail
FROM (VALUES
    ('BK-4417', 2, TIME '07:15', 'Alpha-7', 'Meridian Crossing', 36, 'cleared', NULL::text),
    ('BK-4418', 4, TIME '08:40', 'Beta-9', 'Coastal Junction', 67, 'cleared', NULL),
    ('BK-4419', 5, TIME '09:00', 'Gamma-5', 'Highland Terminus', 91, 'queued', 'Corridor Unstable'),
    ('BK-4420', 7, TIME '10:40', 'Delta-11', 'Solstice Approach', 35, 'cleared', NULL),
    ('BK-4421', 8, TIME '11:30', 'Epsilon-2', 'Umbral Causeway', 76, 'cleared', NULL),
    ('BK-4422', 9, TIME '13:55', 'Zeta-12', 'Tidewater Arch', NULL, 'held', 'Corridor Offline'),
    ('BK-4423', 10, TIME '14:30', 'Theta-8', 'Zenith Landing', 35, 'cleared', NULL),
    ('BK-4424', 13, TIME '16:05', 'Omega-3', 'Lowrift Annex', 85, 'queued', 'Corridor Unstable'),
    ('BK-4425', 16, TIME '18:45', 'Sigma-4', 'Meridian Crossing', 56, 'canceled', NULL),
    ('BK-4426', 18, TIME '21:10', 'Kappa-6', 'Coastal Junction', 39, 'cleared', NULL),
    ('BK-4427', 20, TIME '07:15', 'Alpha-7', 'Highland Terminus', 35, 'cleared', NULL),
    ('BK-4428', 22, TIME '08:40', 'Beta-9', 'Solstice Approach', 59, 'cleared', NULL),
    ('BK-4429', 24, TIME '09:00', 'Gamma-5', 'Umbral Causeway', 82, 'queued', 'Corridor Unstable'),
    ('BK-4430', 25, TIME '10:40', 'Delta-11', 'Tidewater Arch', 42, 'cleared', NULL),
    ('BK-4431', 26, TIME '11:30', 'Epsilon-2', 'Zenith Landing', NULL, 'held', 'Corridor Offline'),
    ('BK-4432', 28, TIME '13:55', 'Zeta-12', 'Lowrift Annex', 77, 'canceled', NULL),
    ('BK-4433', 30, TIME '14:30', 'Theta-8', 'Meridian Crossing', 73, 'cleared', NULL),
    ('BK-4434', 31, TIME '16:05', 'Omega-3', 'Coastal Junction', 90, 'canceled', NULL),
    ('BK-4435', 33, TIME '18:45', 'Sigma-4', 'Highland Terminus', 46, 'cleared', NULL),
    ('BK-4436', 35, TIME '21:10', 'Kappa-6', 'Solstice Approach', 46, 'cleared', NULL),
    ('BK-4437', 37, TIME '07:15', 'Alpha-7', 'Umbral Causeway', 43, 'cleared', NULL),
    ('BK-4438', 38, TIME '08:40', 'Beta-9', 'Tidewater Arch', 38, 'cleared', NULL),
    ('BK-4439', 40, TIME '09:00', 'Gamma-5', 'Zenith Landing', 58, 'cleared', NULL),
    ('BK-4440', 41, TIME '10:40', 'Delta-11', 'Lowrift Annex', 53, 'cleared', NULL),
    ('BK-4441', 43, TIME '11:30', 'Epsilon-2', 'Meridian Crossing', NULL, 'held', 'Corridor Offline'),
    ('BK-4442', 45, TIME '13:55', 'Zeta-12', 'Coastal Junction', NULL, 'held', 'Corridor Offline'),
    ('BK-4443', 46, TIME '14:30', 'Theta-8', 'Highland Terminus', 92, 'queued', 'Corridor Unstable'),
    ('BK-4444', 48, TIME '16:05', 'Omega-3', 'Solstice Approach', 64, 'canceled', NULL),
    ('BK-4445', 51, TIME '18:45', 'Sigma-4', 'Umbral Causeway', 96, 'queued', 'Corridor Unstable'),
    ('BK-4446', 53, TIME '21:10', 'Kappa-6', 'Tidewater Arch', 73, 'cleared', NULL),
    ('BK-4447', 54, TIME '07:15', 'Alpha-7', 'Zenith Landing', 89, 'queued', 'Corridor Unstable'),
    ('BK-4448', 55, TIME '08:40', 'Beta-9', 'Lowrift Annex', 53, 'cleared', NULL),
    ('BK-4449', 56, TIME '09:00', 'Gamma-5', 'Meridian Crossing', 40, 'cleared', NULL),
    ('BK-4450', 58, TIME '10:40', 'Delta-11', 'Coastal Junction', 67, 'cleared', NULL),
    ('BK-4451', 60, TIME '11:30', 'Epsilon-2', 'Highland Terminus', 74, 'cleared', NULL),
    ('BK-4452', 62, TIME '13:55', 'Zeta-12', 'Solstice Approach', 50, 'canceled', NULL),
    ('BK-4453', 64, TIME '14:30', 'Theta-8', 'Umbral Causeway', 61, 'cleared', NULL),
    ('BK-4454', 67, TIME '16:05', 'Omega-3', 'Tidewater Arch', 61, 'cleared', NULL),
    ('BK-4455', 69, TIME '18:45', 'Sigma-4', 'Zenith Landing', 53, 'cleared', NULL),
    ('BK-4456', 71, TIME '21:10', 'Kappa-6', 'Lowrift Annex', 65, 'cleared', NULL),
    ('BK-4457', 73, TIME '07:15', 'Alpha-7', 'Meridian Crossing', 54, 'cleared', NULL),
    ('BK-4458', 74, TIME '08:40', 'Beta-9', 'Coastal Junction', NULL, 'held', 'Corridor Offline'),
    ('BK-4459', 76, TIME '09:00', 'Gamma-5', 'Highland Terminus', 74, 'cleared', NULL),
    ('BK-4460', 78, TIME '10:40', 'Delta-11', 'Solstice Approach', 48, 'cleared', NULL),
    ('BK-4461', 81, TIME '11:30', 'Epsilon-2', 'Umbral Causeway', 57, 'cleared', NULL),
    ('BK-4462', 83, TIME '13:55', 'Zeta-12', 'Tidewater Arch', 75, 'cleared', NULL)
) AS v(reference, day_offset, depart_time, destination, portal, load_percent, status, status_detail)
ON CONFLICT (reference) DO NOTHING;
