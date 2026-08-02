# Oracle Server — Design Notes

This documents the design of the **Oracle** (the grading/simulation
infrastructure), not a scheduler — there is no scheduling strategy to defend
here, just the engineering behind the environment a scheduler runs against.

## Initial Design

The spec leaves a lot of the Oracle's internals unspecified on purpose
("the exact scoring formula is intentionally not published"), but the three
REST endpoints and their example payloads are fixed. I started from the
contract and worked inward: get `POST /expedition` → `GET /expedition/{id}`
→ `POST /cycle/{id}/schedule` returning something plausible with one
workload type, then layer in the real generator, engine, and scoring.

Two decisions were made early and shaped everything else:

1. **Workload profiles over pure randomness.** Six fixed profiles
   (transfer chains, surge arrivals, deep rift, narrow window,
   gate congestion, mixed traffic), each with its own tuned ranges for
   gate count, voyage count, prerequisite depth, deadline slack, and outage
   rate. Every expedition samples all six at least once, plus a couple of
   extra draws, then shuffles the order. This was the one requirement I
   held to non-negotiably: without it, two applicants could get wildly
   different difficulty by luck of the seed, which undermines the whole
   "average across cycles" scoring philosophy.
2. **The Oracle is stateless per HTTP request.** Every tick's full
   cycle state (voyages, gates, accumulated stats) round-trips through
   Postgres as a JSONB document keyed by `(expedition_id, cycle_number)`.
   No in-memory session, no sticky state — a scheduler could restart mid-run
   and keep going.

## Iteration

The biggest change during development was how the sequence of workload
profiles gets threaded through cycle rollover. My first pass derived
each cycle's profile from a hash of `(expeditionId, slotNumber)` at
rollover time — no extra state needed, but it meant the "guarantee every
profile appears at least once" property computed at expedition-creation time
was silently discarded after cycle 1. That's a correctness bug
disguised as a simplification: the schema didn't actually guarantee what the
comment claimed. I replaced it with an explicit `profile_plan` column
persisted at creation time, and rollover just indexes into it. More schema,
but the guarantee is real instead of asserted.

I also went back and forth on `BoardingTick` vs. `RequestedTick` for the
fairness metric's wait-time calculation. Using requested time would conflate
"waiting on a scheduler" with "waiting on a prerequisite chain," which isn't
the scheduler's fault. Tracking the tick a voyage's prerequisites actually
resolved (`BoardingTick`) isolates the wait time that's actually attributable
to scheduling decisions.

## Tradeoffs

**Optimized for:** correctness of the validator (every rejection returns a
specific, actionable reason — "gate 4 has insufficient power for voyage 18
(needs 6, has 3)" rather than a generic 400), and testability of the
orchestration logic. `store.Store` is an interface specifically so
`internal/evaluation`'s rollover/aggregation logic has a unit test suite
that runs against an in-memory fake — no live database needed to validate
that behavior, which matters a lot for infrastructure a submission gets
graded against.

**Not optimized for:** scale, and I want to be upfront about that rather
than pretend otherwise. Storing full cycle state as a JSONB blob and
re-reading/re-writing it whole on every tick is simple but doesn't scale
past a few hundred voyages — a real production version would shard state or
move to incremental updates. Per-tick latency against a remote Supabase
instance measured ~190ms in testing (dominated by round trips, not compute),
which is fine for an interview-scale expedition (a few thousand ticks total)
but would not hold up at the "one million queued voyages" scale mentioned in
the interview questions. I also did not tune the profile parameters or
scoring weights against real scheduler behavior — they're principled
starting guesses, not calibrated. That calibration only becomes meaningful
once real schedulers are running against this.

## Challenges

The trickiest problem was actually in the spec's ambiguity, not the code:
`POST /cycle/{id}/schedule` and `GET /expedition/{id}` both use `{id}`,
but the endpoints are named `/cycle/` and `/expedition/` respectively —
it's not obvious from the spec alone whether `{id}` means the same thing in
both. I resolved this by making `{id}` mean the expedition ID everywhere: a
client holds one identifier for the life of an expedition, and the Oracle
tracks which cycle number is "current" server-side. The alternative
(the client tracking a separate cycle ID that changes on rollover) adds
a coordination step for no real benefit, since cycles within one
expedition are never scheduled concurrently.

A close second: deciding what "gate goes offline" should do to
in-transit voyages. Pausing a voyage's progress without touching its
assignment is simplest but makes gate outages nearly free for the scheduler
to ignore. I chose a harsher model — an offline gate's in-transit voyages
get evicted back to `boarding` (keeping partial `RemainingDuration` progress
but losing their assignment), which means a scheduler that never checks gate
health will visibly bleed throughput and reliability. That felt truer to
"gates may become unavailable" as a real adaptive-scheduling pressure rather
than background noise.

## Future Work

Given another weekend: replace the per-tick full-state round trip with an
in-process LRU of active cycles backed by periodic + on-finish
persistence, so tick latency stops being dominated by network round trips.
I'd also add property-based tests for the generator (assert the prerequisite
graph is always acyclic and every arrival deadline is theoretically reachable
given gate capacity, across hundreds of random seeds, not just the few I hand
picked). Finally, I'd build a small CLI that visualizes one expedition's run
tick-by-tick — right now debugging a scheduler against this Oracle means
reading raw JSON, and a timeline view of gate utilization and voyage
arrivals would make that dramatically easier for anyone building against it.

## Reflection

The part I'm most proud of is the split between `internal/models` (the full
internal state, serialized wholesale for persistence) and `internal/api`'s
response DTOs (which reshape that state into the wire format, dropping
fields like accumulated stats and unrequested voyages). Early on I was
tempted to control the public shape with `json:"-"` tags directly on the
domain model, which would have been less code — but it silently broke
persistence, because the same struct was doing double duty as both the
storage format and the API response. Separating those two concerns cleanly
is a small thing, but it's the kind of decision that keeps the engine,
store, and API layers honestly independent of each other instead of
accidentally coupled through shared struct tags.
