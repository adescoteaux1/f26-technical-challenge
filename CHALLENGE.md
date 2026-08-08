# Nexus Transit Authority — Scheduler Challenge

Someone has to decide which traveler steps through which gate, and when.
That's you.

You'll build a **scheduler**: a program that decides which voyages depart
through which portal gates, and in what order. It talks over HTTP to a
server we call the **Oracle**, which generates the transit demand,
simulates the network, and scores how you did.

**You have total freedom on implementation.** Any language, any libraries,
any architecture, any project structure. The only hard requirement is that
your program speaks HTTP/JSON to the Oracle correctly.

---

## 1. How this gets evaluated

Engineers do a lot more than write algorithms: they make judgment calls,
balance tradeoffs, design things another person could pick up and maintain,
test their work, and explain why they built it the way they did. That's
what this challenge is trying to get at, not just whether you can write a
clever assignment loop.

We're looking for solid code and a clear explanation of the decisions
behind it.

---

## 2. The problem

The Nexus Transit Authority runs a network of interdimensional portal
gates. Travelers request voyages across the network, each with a resource
footprint, an urgency, and sometimes a chain of connecting transfers that
have to clear first. Gates have finite power and containment capacity, can
run multiple voyages at once if there's room, and can occasionally drop
offline mid-shift.

Every tick, your scheduler decides which voyages that are ready to board
get assigned to which gates. The Oracle handles everything else: advancing
the clock, running voyages to arrival, unlocking transfers, generating new
transit demand, and tracking your score.

There are several competing objectives here — move travelers through
quickly, use gate capacity efficiently, hit arrival windows, don't strand
one hub's travelers to favor another, adapt when a gate drops offline —
and pushing hard on one usually costs you on another.

---

## 3. Registering and getting a token

Register once:

```bash
curl -X POST <ORACLE_BASE_URL>/register \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "nuid": "001234567"}'
```

```json
{"userId": "...", "token": "a1b2c3..."}
```

Save that `token`. Every other request must include it:

```text
Authorization: Bearer <token>
```

If you lose your token, `POST /login` with the same `{email, nuid}` gets you a new
one (this invalidates the old one).

Everything you run is tied to your account, and you can look back at your
own history any time:

```bash
curl <ORACLE_BASE_URL>/me/expeditions -H "Authorization: Bearer $TOKEN"
```

Run as many expeditions as you want while you iterate. Only your final
submission counts, but your history is there so you can check whether
you're actually improving run over run.

---

## 4. How the interaction is shaped

Running an expedition looks like this:

1. **Start** one (`POST /expedition`). This gives you an `expeditionId`
   that covers the whole 16-cycle run — you don't start a new one per
   cycle. The Oracle moves through all 16 under the same ID and tells you
   when it advances (see §6).
2. **Read** the current state (`GET /expedition/{id}`): tick, gates,
   voyages, running metrics.
3. **Decide** which schedulable voyages should go through which gates this
   tick, and submit that batch (`POST /cycle/{id}/schedule`).
4. **Repeat** steps 2-3 until the state comes back `"finished": true` with
   your `overallScore` and final `metrics`. An empty submission is valid —
   it just advances the clock.

### Pieces you'll need to design

- **A loop** driving the read/decide/submit cycle above until the
  expedition finishes.
- **A way to tell what's actually schedulable** right now versus what
  isn't yet.
- **A capacity check** before assigning to a gate — including capacity
  used by other voyages in the same submitted batch, not just what's
  already running there.
- **Handling for partial rejection.** A submitted batch can succeed in
  part and fail in part; check the response to see what actually landed.

The `overallScore` and `metrics` you get back once `finished: true` are
each the plain average of that same value across all 16 cycles, not a
sum — see §7.

Start with the simplest scheduling rule you can think of to prove the
plumbing works end to end. Use that as your baseline for testing before
you build out any real strategy.

---

## 5. The domain model

### Voyage

```json
{
  "id": 18,
  "originHub": "central-hub-alpha",
  "priority": 4,
  "estimatedDuration": 6,
  "requiredPower": 3,
  "requiredContainment": 1,
  "arrivalDeadline": 48,
  "prerequisites": [12, 15],
  "requestedTick": 10,
  "status": "boarding",
  "remainingDuration": 6,
  "assignedGate": null,
  "boardingTick": 10,
  "departureTick": null,
  "arrivalTick": null,
  "legs": null,
  "legIndex": 0,
  "slaDeadline": null
}
```

- `status` is one of `awaiting_transfer` (prerequisites incomplete),
  `boarding` (schedulable now), `in_transit` (assigned to a gate),
  `arrived` (done).
- `priority` runs 1 (low) to 5 (critical). The Oracle doesn't enforce
  anything based on it — what you do with it is up to your strategy.
- `arrivalDeadline` and `requestedTick` are simulation tick numbers, not
  wall-clock time.
- A voyage only shows up in the API once its `requestedTick` has passed.
  You can't see or plan around travelers who haven't requested transit yet,
  and new voyages can appear mid-run.
- `prerequisites` are other voyage IDs that must reach `arrived` before
  this voyage can become `boarding`. That's separate from `legs` below,
  which is about a voyage's own multi-hop journey, not its dependency on
  other voyages.
- `legs` / `legIndex` — see **Multi-hop corridors** below. `null`/`0` for a
  normal single-hop voyage.
- `slaDeadline` — see **Premium hubs & SLA** below. `null` unless this
  voyage's `originHub` is currently a premium hub.

### Gate

```json
{
  "id": 1,
  "totalPower": 8,
  "totalContainment": 16,
  "availablePower": 5,
  "availableContainment": 12,
  "activeVoyages": [7, 9],
  "operational": true,
  "offlineUntil": 0
}
```

- Gates can run multiple voyages at once as long as capacity allows.
- Gates can go temporarily offline (a rift destabilization). While
  `operational` is `false` you can't assign it new work, and any voyages
  that were in transit through it get kicked back to `boarding` — progress
  preserved, assignment lost. The Oracle doesn't reschedule them for you.
  A scheduler that never notices a gate went offline will quietly bleed
  score.

### Assignment (what you submit)

```json
{"gateId": 1, "voyageId": 18}
```

Submit an array of these to `POST /cycle/{id}/schedule`. `[]` is valid and
just advances the clock with no new assignments.

### Multi-hop corridors

Roughly 15% of voyages, picked at random, aren't a single gate hop — they're
a corridor of 2-3 legs that have to be completed in order, each through its
own gate assignment:

```json
{
  "id": 42,
  "legs": [
    {"requiredPower": 2, "requiredContainment": 2, "estimatedDuration": 4},
    {"requiredPower": 3, "requiredContainment": 1, "estimatedDuration": 3}
  ],
  "legIndex": 0,
  "requiredPower": 2,
  "requiredContainment": 2,
  "estimatedDuration": 4,
  "remainingDuration": 4,
  "status": "boarding"
}
```

- `legs` is the voyage's entire planned corridor, visible up front so you
  can plan ahead, but you can only ever assign the voyage to a gate for its
  current leg.
- The top-level `requiredPower` / `requiredContainment` / `estimatedDuration`
  / `remainingDuration` always describe the current leg (`legs[legIndex]`),
  not the whole trip. Assign against those fields exactly like a normal
  voyage — you don't need special-case logic to schedule a corridor leg.
- When a leg completes, the voyage doesn't arrive yet (unless it was the
  last leg): `legIndex` advances, the top-level fields update to reflect
  the next leg, `assignedGate` clears, and `status` goes back to
  `boarding`. It only becomes `arrived` — and only then counts toward
  `throughput`, `arrivalSuccess`, etc. — once the last leg completes.
- The one `arrivalDeadline` on the voyage covers the whole corridor, not
  each leg individually.
- A scheduler that treats every "boarding" voyage the same way handles
  this correctly by accident, since the voyage just reappears asking to be
  scheduled again. One that assumes "assigned once" means "done" will
  silently under-report a corridor voyage's real gate demand and get
  surprised when it reappears.

### Premium hubs & SLA

Each cycle designates 1-2 origin hubs as premium, paying for a tighter
guarantee than the standard `arrivalDeadline`. You're told which hubs those
are (`premiumHubs` on the cycle state — see §6), and any voyage from one of
them additionally carries an `slaDeadline`, always tighter than its
`arrivalDeadline`:

```json
{"premiumHubs": ["central-hub-alpha", "quantum-nexus"]}
```

```json
{"id": 91, "originHub": "quantum-nexus", "arrivalDeadline": 60, "slaDeadline": 40}
```

`slaDeadline` doesn't affect validation — the Oracle will happily let you
schedule a premium voyage as late as you want, same as any other. It only
feeds the `slaCompliance` metric (see §7): the fraction of premium-hub
voyages that beat their SLA window, not just their regular deadline. This
is meant to make the "premium traffic should finish sooner without
starving everyone else" tension concrete — `fairness` is still watching,
so a scheduler that always drains premium hubs first will win
`slaCompliance` and lose `fairness`. Which tradeoff you make, and being
able to say why, is the point.

---

## 6. How an expedition is structured

One expedition is 16 cycles, run back to back under the same
`expeditionId`. Each cycle is a fresh, independent transit scenario drawn
from one of six fixed profiles, so you face a representative spread of
conditions instead of one lucky or unlucky random draw:

| Profile | What it stresses |
| --- | --- |
| `transfer_chains` | Few gates, long chains of connecting voyages |
| `surge_arrivals` | Many gates, hundreds of small voyages requested in a wave |
| `deep_rift` | Few voyages, but massive power/containment draw each |
| `narrow_window` | Arrival deadlines tight relative to transit duration |
| `gate_congestion` | Far more voyages than your gates can absorb at once |
| `mixed_traffic` | A general mix of all of the above |

Every expedition repeats full passes through all six profiles as many
times as fit (16 cycles is two full passes plus a partial third), draws any
leftover cycles from a shuffled partial pass rather than fully at random,
and shuffles the final order, so no single profile can dominate an
expedition by luck. When one cycle finishes, the Oracle automatically
starts the next — keep polling and scheduling against the same
`expeditionId` throughout. When all 16 are done, you get:

```json
{"finished": true, "overallScore": 74.0, "metrics": {"throughput": 94.4, "gateUtilization": 44.8, "arrivalSuccess": 68.6, "fairness": 61.7, "reliability": 100, "slaCompliance": 82.0}}
```

`overallScore` is the average across all 16 cycles. Consistency across
varied conditions matters more than one spiky best case.

---

## 7. What's being measured

Six metrics, computed continuously: **throughput** (% of voyages that
arrived), **gateUtilization** (% of gate capacity kept busy),
**arrivalSuccess** (% of arrived voyages that beat their deadline),
**fairness** (penalizes starving one origin hub's travelers to favor
another), **reliability** (% of your submitted assignments that were
valid), **slaCompliance** (% of premium-hub voyages that beat their
tighter SLA window — see §5's "Premium hubs & SLA"). `overallScore` is a
weighted blend of all six. We're not publishing the exact weights, for the
same reason as §1: this isn't a formula to solve for.

---

## 8. What to submit

Your whole submission — code, `README.md`, `DESIGN.md`, tests, all of it —
needs to live in a GitHub repo (public, or shared with whoever's reviewing
it). That repo is the submission: we're evaluating your code quality, how
you iterated and worked through the problem (largely via `DESIGN.md` and,
where it's visible, your commit/expedition history), and your final
scores.

1. **A working scheduler** that runs one or more full expeditions against
   the Oracle end to end.
2. **`README.md`** — setup, how to run it, dependencies, project layout.
   Assume the reader has never seen your project before.
3. **`DESIGN.md`** (max 2 pages) — not a tour of every class, but how you
   thought about the problem:
   - **Initial design** — what was your first approach, and why?
   - **Iteration** — what changed as you developed? Did your strategy
     evolve after seeing your scores?
   - **Tradeoffs** — what did you deliberately optimize for? What did you
     deliberately not optimize, and why was that the right call?
   - **Challenges** — what was the hardest engineering problem you ran into?
   - **Future work** — with another weekend, what would you build next?
   - **Reflection** — what part of this are you most proud of?
4. **Tests** — automated, whatever amount and style you think is
   appropriate. A few tests that reflect real judgment about what's worth
   testing beats 100% coverage with no thought behind it.

---

## 9. If you're not sure where to start

Treat this like any other piece of software you're building, not a puzzle
to solve in one sitting:

1. **Get a thin vertical slice working end to end** — the loop in §4, with
   the dumbest possible logic. Confirm you can see `finished: true`.
2. **Write a test for your assignment logic in isolation.** Feed it a
   handful of fake voyages and gates, assert what it decides, without a
   live Oracle connection. If that's awkward to write, that's a sign your
   scheduling logic is too tangled up with your HTTP code.
3. **Make the strategy swappable.** Even if you only ever write one
   strategy, structure it so a second one could exist. It's a good forcing
   function for a clean interface between "talk to the Oracle" and "decide
   what to schedule."
4. **Iterate on the decision logic itself.** Try a real prioritization
   rule (earliest deadline, or highest priority), see what it does to your
   metrics, and make a deliberate call about the next tradeoff instead of
   guessing.
5. **Handle gate outages explicitly.** Notice when a voyage you previously
   assigned comes back to `boarding` with a different (or no)
   `assignedGate`.
6. **Only then worry about corridors and premium hubs.** A scheduler that
   treats every "boarding" voyage the same way already handles multi-leg
   corridors correctly by construction. Premium hubs are a genuine policy
   decision — favor them how much, at what cost to fairness? — worth
   making deliberately once your basics are solid, not before.
7. **Write `DESIGN.md` as you go, not at the end.** It's easier to record
   your reasoning in the moment than to reconstruct it afterward, and it
   reads more honestly for it.

## 10. Extension: resilience against a flaky network

This has nothing to do with scheduling logic, and nothing here requires
it — a request either succeeds or it doesn't, and the loop in §4 works
fine without any of this. But if you want to push further: treat the
Oracle like the real, occasionally-flaky network dependency it is. A
client that retries transient failures with backoff, and re-checks state
rather than blindly resubmitting after an ambiguous failure (a request can
fail on your end after the Oracle already applied it), is worth showing
off if you have time for it.

To let you actually build and test this instead of just asserting it
works, the Oracle exposes a separate endpoint that simulates failure modes
deterministically, with no server-side session state and no connection to
any expedition or score:

```text
GET /chaos/probe?mode=<mode>&attempt=<n>&failUntil=<n>&delayMs=<n>
```

- `mode=error` — always returns a transient-looking failure.
- `mode=timeout` — delays the response by `delayMs` before succeeding.
- `mode=flaky` — fails while `attempt < failUntil`, succeeds once
  `attempt >= failUntil`. You supply and increment `attempt` yourself
  across your own retries.
- `mode=success` (default) — always succeeds.

Because it's deterministic and stateless, you can write a real, repeatable
test against your retry/backoff logic instead of hoping it works against a
live, actually-random failure.
