# Generate Cloud Scheduler — Oracle Server

This is the **Oracle**: the simulation/evaluation server that a scheduler
client talks to over REST. It generates workloads, simulates execution tick
by tick, validates scheduling decisions, and scores performance across
multiple independent simulations per evaluation.

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

Unit tests cover the validator, tick engine (job completion, dependency
unlocking, worker failure/recovery), scoring formulas, the workload
generator, evaluation orchestration (profile rollover, aggregation), user
registration/login, and the HTTP layer (auth enforcement, ownership) — all
using an in-memory fake store (`internal/storetest`), so no database is
needed to run the test suite.

## Authentication

Every endpoint except `/register`, `/login`, and `/healthz` requires a
bearer token, so evaluations are tied to the applicant who created them and
their run history can be looked up later.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/register` | Create an account with `{email, nuid}`; returns `{userId, token}` |
| `POST` | `/login` | Re-authenticate with `{email, nuid}`; rotates and returns a fresh token |
| `GET` | `/me/evaluations` | List the caller's past evaluations (id, finished, overallScore, metrics, createdAt) |

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

The three simulation endpoints, matching the challenge spec (all require auth):

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/evaluation` | Start a new evaluation (samples a sequence of workload profiles, generates the first simulation) |
| `GET` | `/evaluation/{evaluationId}` | Get the current simulation's live state, or the final aggregate once finished |
| `POST` | `/simulation/{evaluationId}/schedule` | Submit a batch of `{workerId, jobId}` assignments; advances the simulation by one tick |

Note: the `{id}` path parameter is the **evaluation ID** in both `GET
/evaluation/{id}` and `POST /simulation/{id}/schedule` — a client tracks one
ID for the whole evaluation, not a separate ID per simulation. Requesting an
evaluation you don't own returns `403 Forbidden`.

### Example flow

```bash
TOKEN=$(curl -s -X POST localhost:8080/register \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "nuid": "001234567"}' | jq -r .token)

# 1. Start an evaluation
curl -X POST localhost:8080/evaluation -H "Authorization: Bearer $TOKEN"
# {"evaluationId":"...","simulation":1,"totalSimulations":8}

# 2. Poll current state
curl localhost:8080/evaluation/<id> -H "Authorization: Bearer $TOKEN"

# 3. Submit scheduling decisions (repeat until finished)
curl -X POST localhost:8080/simulation/<id>/schedule \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '[{"workerId": 1, "jobId": 4}]'

# Eventually:
# {"finished":true,"overallScore":84.2,"metrics":{...}}

# 4. Check your history any time
curl localhost:8080/me/evaluations -H "Authorization: Bearer $TOKEN"
```

An empty array (`[]`) is a valid schedule request body — it advances the
clock one tick without making any new assignments (useful for waiting out
dependencies or worker outages).

Every schedule response includes a `"rejected"` array (omitted when empty)
describing which submitted assignments were skipped and why — invalid
assignments don't fail the whole request, they're just excluded from the
tick's applied decisions.

## Project organization

```
cmd/oracle/            entrypoint: config, DB connection, HTTP server, graceful shutdown
internal/
  models/              core domain types (Job, Worker, Simulation, Evaluation, User) — no logic
  generator/           Workload Generator: 6 workload profiles, DAG-respecting job generation
  engine/              Simulation Engine: validator.go (reject invalid decisions) +
                       engine.go (tick advancement: job progress, completion, dependency
                       unlocking, worker failure/recovery)
  scoring/             Scoring Engine: per-tick metrics + evaluation-level aggregation
  store/               persistence boundary: Store interface + Postgres/Supabase implementation
                       (JSONB simulation state, relational users/evaluations)
  auth/                opaque bearer token generation
  userauth/            registration/login business logic (email+NUID -> token)
  evaluation/          Evaluation Engine: orchestrates generator + engine + scoring + store
                       behind the three simulation endpoints; owns profile sampling, rollover,
                       and per-evaluation ownership checks
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

Rather than fully random generation, every simulation is drawn from one of
six fixed profiles (`internal/generator/generator.go`), each tuned to stress
a different scheduling concern:

- **dependency_chains** — few workers, long chains of dependent jobs
- **burst_traffic** — many workers, hundreds of small jobs arriving in a burst
- **heavy_compute** — few jobs, but CPU/memory-heavy and long-running
- **deadline_critical** — tight deadlines relative to runtime
- **resource_constrained** — far more jobs than workers can absorb at once
- **balanced** — a general mix of the above

Each evaluation samples all 6 profiles at least once, then fills the
remaining slots (default 8 total simulations) with additional random draws,
and shuffles the order — so difficulty is comparable across evaluations
without anyone reliably drawing an "easy" run.

## Scoring

Metrics are recomputed from running totals on every request (not just at the
end), so `GET /evaluation/{id}` always reflects live progress:

- **throughput** — % of the simulation's jobs completed so far
- **workerUtilization** — % of total worker CPU-ticks spent busy
- **deadlineSuccess** — % of completed jobs that finished by their deadline
- **fairness** — penalizes large disparities in average queue wait time
  across projects (a scheduler that starves one project scores lower here
  even with high throughput)
- **reliability** — % of submitted assignments that were valid

The overall score is a fixed weighted combination of the five (see
`internal/scoring/scoring.go`); the evaluation-level score is the simple
average of each simulation's overall score, per the "average performance
across every simulation" scoring philosophy.
