# Generate Cloud Scheduler — Oracle Server

This is the **Oracle**: the simulation/expedition server that a scheduler
client talks to over REST. It generates workloads, simulates transit tick
by tick, validates scheduling decisions, and scores performance across
multiple independent cycles per expedition.

It does **not** include a scheduler client — this repo is the grading
infrastructure a scheduler is built against.

## Setup

### Dependencies

- Go 1.25+
- A Postgres database (this project targets [Supabase](https://supabase.com),
  but any Postgres connection string works)

### Configure

Create a `.env` file in the repo root (see `.env.example`):

```
DATABASE_URL=postgres://postgres:[password]@db.[project-ref].supabase.co:5432/postgres
PORT=8080
```

`DATABASE_URL` is required. `PORT` defaults to `8080` if omitted.

### Run

```bash
go run ./cmd/oracle
```

On startup the server connects to Postgres and runs its embedded schema
migration automatically (`CREATE TABLE IF NOT EXISTS ...` — safe to run every
time, no separate migration step needed). You should see:

```
level=INFO msg="oracle server listening" port=8080
```

### Test

```bash
go test ./...
```

Unit tests cover the validator, tick engine (voyage arrival, prerequisite
unlocking, gate outage/recovery), scoring formulas, the workload
generator, expedition orchestration (profile rollover, aggregation), user
registration/login, and the HTTP layer (auth enforcement, ownership) — all
using an in-memory fake store (`internal/storetest`), so no database is
needed to run the test suite.

## Authentication

Every endpoint except `/register`, `/login`, and `/healthz` requires a
bearer token, so expeditions are tied to the applicant who created them and
their run history can be looked up later.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/register` | Create an account with `{email, nuid}`; returns `{userId, token}` |
| `POST` | `/login` | Re-authenticate with `{email, nuid}`; rotates and returns a fresh token |
| `GET` | `/me/expeditions` | List the caller's past expeditions (id, finished, overallScore, metrics, createdAt) |

```bash
curl -X POST localhost:8080/register \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "nuid": "001234567"}'
# {"userId":"...","token":"..."}
```

Store the returned token and send it as `Authorization: Bearer <token>` on
every other request. There's no password: email + NUID is the credential
pair, and `/login` is how you recover a token if it's lost (it invalidates
the old one). Tokens are opaque, random, and looked up directly against the
`users` table — there's no JWT/session/expiry machinery, which is enough for
a service whose only clients are scheduler programs, not browsers.

## API

Every endpoint is built with [Huma](https://huma.rocks), which generates an
OpenAPI 3.1 spec directly from the Go types in `internal/api/dto.go` — no
hand-written spec to keep in sync. Once the server is running:

- **`/docs`** — interactive [Scalar](https://scalar.com) API reference (try
  requests directly in the browser)
- **`/openapi.json`** (or `.yaml`) — the raw spec, useful for generating a
  client SDK

The three simulation endpoints, matching the challenge spec (all require auth):

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/expedition` | Start a new expedition (samples a sequence of workload profiles, generates the first cycle) |
| `GET` | `/expedition/{expeditionId}` | Get the current cycle's live state, or the final aggregate once finished |
| `POST` | `/cycle/{expeditionId}/schedule` | Submit a batch of `{gateId, voyageId}` assignments; advances the cycle by one tick |

Note: the `{id}` path parameter is the **expedition ID** in both `GET
/expedition/{id}` and `POST /cycle/{id}/schedule` — a client tracks one
ID for the whole expedition, not a separate ID per cycle. Requesting an
expedition you don't own returns `403 Forbidden`.

### Example flow

```bash
TOKEN=$(curl -s -X POST localhost:8080/register \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "nuid": "001234567"}' | jq -r .token)

# 1. Start an expedition
curl -X POST localhost:8080/expedition -H "Authorization: Bearer $TOKEN"
# {"expeditionId":"...","cycle":1,"totalCycles":16}

# 2. Poll current state
curl localhost:8080/expedition/<id> -H "Authorization: Bearer $TOKEN"

# 3. Submit scheduling decisions (repeat until finished)
curl -X POST localhost:8080/cycle/<id>/schedule \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '[{"gateId": 1, "voyageId": 4}]'

# Eventually:
# {"finished":true,"overallScore":84.2,"metrics":{...}}

# 4. Check your history any time
curl localhost:8080/me/expeditions -H "Authorization: Bearer $TOKEN"
```

An empty array (`[]`) is a valid schedule request body — it advances the
clock one tick without making any new assignments (useful for waiting out
prerequisites or gate outages).

Every schedule response includes a `"rejected"` array (omitted when empty)
describing which submitted assignments were skipped and why — invalid
assignments don't fail the whole request, they're just excluded from the
tick's applied decisions.

## Project organization

```
cmd/oracle/            entrypoint: config, DB connection, HTTP server, graceful shutdown
internal/
  models/              core domain types (Voyage, Gate, Cycle, Expedition, User) — no logic
  generator/           Workload Generator: 6 workload profiles, DAG-respecting voyage generation
  engine/              Simulation Engine: validator.go (reject invalid decisions) +
                       engine.go (tick advancement: voyage progress, arrival, prerequisite
                       unlocking, gate outage/recovery)
  scoring/             Scoring Engine: per-tick metrics + expedition-level aggregation
  store/               persistence boundary: Store interface + Postgres/Supabase implementation
                       (JSONB cycle state, relational users/expeditions)
  auth/                opaque bearer token generation
  userauth/            registration/login business logic (email+NUID -> token)
  evaluation/          Expedition Engine: orchestrates generator + engine + scoring + store
                       behind the three cycle endpoints; owns profile sampling, rollover,
                       and per-expedition ownership checks
  api/                 HTTP layer: router, auth middleware, handlers, response DTOs
  storetest/           in-memory store.Store fake shared by every package's tests
migrations/            embedded SQL schema, applied automatically on startup
```

The dependency direction is one-way: `api` → `{evaluation, userauth}` →
`{generator, engine, scoring, store, auth}` → `models`. Nothing below
`evaluation`/`userauth` knows about HTTP, and nothing above `models` is
imported by the domain types themselves. `store.Store` is an interface
specifically so orchestration and auth logic are testable without a live
Postgres connection — see `internal/storetest`, used by
`evaluation`, `userauth`, and `api`'s test suites alike.

## Workload profiles

Rather than fully random generation, every cycle is drawn from one of
six fixed profiles (`internal/generator/generator.go`), each tuned to stress
a different scheduling concern:

- **transfer_chains** — few gates, long chains of dependent voyages
- **surge_arrivals** — many gates, hundreds of small voyages arriving in a surge
- **deep_rift** — few voyages, but power/containment-heavy and long-transit
- **narrow_window** — tight arrival deadlines relative to duration
- **gate_congestion** — far more voyages than gates can absorb at once
- **mixed_traffic** — a general mix of the above

Each expedition (16 cycles by default) repeats full passes through all 6
profiles as many times as fit, draws any leftover cycles from a shuffled
partial pass rather than fully at random, and shuffles the final order — so
no single profile can end up over- or under-represented by luck, and
difficulty is comparable across expeditions without anyone reliably drawing
an "easy" run.

Independent of profile, two orthogonal wrinkles apply to every cycle:

- **Multi-hop corridors** — ~15% of voyages (`internal/generator/generator.go`'s
  `applyCorridors`) require passing through a sequence of 2-3 gates in order
  (`Voyage.Legs`/`LegIndex`) rather than a single hop. The engine only
  advances `LegIndex` and re-opens the voyage for boarding when a
  non-final leg completes; scoring (throughput, arrivalSuccess, ...) only
  fires once, on the final leg — so the top-level `Voyage` fields always
  describe "the current leg," and nothing downstream needed special-casing.
- **Premium hubs & SLA** — each cycle designates 1-2 origin hubs as
  premium (`applyPremiumHubs`), giving their voyages a tighter
  `SLADeadline` alongside the normal `ArrivalDeadline`, feeding the
  `slaCompliance` metric below. It's a real implementation of the
  "premium projects should finish sooner without starving everyone else"
  tension, rather than a hypothetical.

## Scoring

Metrics are recomputed from running totals on every request (not just at the
end), so `GET /expedition/{id}` always reflects live progress:

- **throughput** — % of the cycle's voyages arrived so far
- **gateUtilization** — % of total gate power-ticks spent busy
- **arrivalSuccess** — % of arrived voyages that arrived by their deadline
- **fairness** — penalizes large disparities in average queue wait time
  across origin hubs (a scheduler that starves one hub scores lower here
  even with high throughput)
- **reliability** — % of submitted assignments that were valid
- **slaCompliance** — % of premium-hub voyages that beat their SLA deadline
  (see "Premium hubs & SLA" above); defaults to 100 when no premium voyage
  has arrived yet

The overall score is a fixed weighted combination of all six (see
`internal/scoring/scoring.go`); the expedition-level score is the simple
average of each cycle's overall score, per the "average performance
across every cycle" scoring philosophy.
