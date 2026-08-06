# Nexus Transit Authority — Scheduler Challenge

Somewhere between the terminals and the corridors, someone has to decide
which traveler steps through which gate, and when. That's you.

You're going to build a **scheduler**: a program that decides which voyages
depart through which portal gates, and in what order. It talks to a server
we call the **Oracle** — the closest thing the Nexus Transit Authority has
to an all-seeing dispatcher — which generates the transit demand, simulates
the network, and scores how you did.

There is no known optimal strategy for this problem, and that's on purpose.
Engineers are responsible for a lot more than writing algorithms — making
judgment calls, balancing tradeoffs, designing something another person
could pick up and maintain, testing it, explaining *why* you built it the
way you did. This challenge is trying to simulate that whole job, not just
the "write a clever assignment loop" part of it. Read the next section
before you write any code — it changes how you should spend your time.

**You have total freedom on implementation.** Any language, any libraries,
any architecture, any project structure. Command-line tool, long-running
daemon, a pile of shell scripts — doesn't matter. The only hard requirement
is that your program speaks HTTP/JSON to the Oracle correctly.

---

## 1. How this gets evaluated (read this first)

| Category | Weight |
| --- | --- |
| Correctness | 25% |
| Scheduling performance | 20% |
| Code quality | 20% |
| Architecture | 15% |
| Testing | 10% |
| Documentation | 10% |

Your scheduler's actual score from the Oracle — the number in
`overallScore` — is **one-fifth of your grade.** The other four-fifths is
whether your code is correct, well-organized, tested, and clearly explained.

We're saying this up front because it's easy to read a doc full of gate
stats, transit profiles, and scoring formulas (there's one coming up below)
and conclude that the game is to squeeze out the highest possible
`overallScore`. That's not the game. A simple, clearly-written, well-tested
greedy scheduler with an honest writeup about its limitations will
outscore an elaborate, cleverly-tuned scheduler that's an unreadable pile of
special cases with no tests. Optimize for the 80%, not the 20%.

Concretely, that means we want to see things like:

- Your scheduling *strategy* is isolated from your HTTP/plumbing code —
  someone should be able to swap in a different strategy without touching
  how you talk to the Oracle.
- You have tests for your assignment logic that don't require a live Oracle
  connection to run.
- Your `README.md` and `DESIGN.md` (details in §8) actually explain your
  reasoning, not just what the code does.
- You made deliberate tradeoffs and can say what they were, including things
  you chose *not* to build.

None of that shows up in `overallScore`. All of it is most of your grade.

---

## 2. The problem

The Nexus Transit Authority operates a network of interdimensional portal
gates — travelers request voyages across the network, each with a resource
footprint, an urgency, and sometimes a chain of connecting transfers that
have to clear first. Gates have finite power and containment capacity, can
run multiple voyages at once if they have room, and can occasionally drop
offline mid-shift.

Your scheduler's job, every tick, is to decide: **which voyages ready to
board get assigned to which gates.** The Oracle handles everything else —
advancing the clock, running voyages to arrival, unlocking transfers,
generating new transit demand, and tracking your score.

There are several competing objectives (move travelers through quickly, use
gate capacity efficiently, hit arrival windows, don't strand any one hub's
travelers while favoring another, adapt when a gate drops offline) and
optimizing hard for one usually costs you on another. That tension is real,
and worth thinking about — just don't let it become the *only* thing you
think about.

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

There's no password — email + NUID is your credential pair. If you lose
your token, `POST /login` with the same `{email, nuid}` gets you a new one
(this invalidates the old one).

Everything you run is tied to your account, and you can look back at your
own history any time:

```bash
curl <ORACLE_BASE_URL>/me/expeditions -H "Authorization: Bearer $TOKEN"
```

Run as many expeditions as you want while you iterate — only your final
submission counts, but your history is there so you can sanity-check
whether you're actually improving run over run.

---

## 4. Quick start: the simplest possible loop

This is the entire shape of the interaction. Get this working first, with
the dumbest possible scheduling logic, before you write anything clever —
it de-risks the plumbing so every hour after this is spent on decisions that
actually matter.

```bash
TOKEN="..."   # from /register or /login

# 1. Start an expedition (a full 16-cycle run through the network)
curl -X POST <ORACLE_BASE_URL>/expedition -H "Authorization: Bearer $TOKEN"
# -> {"expeditionId": "abc-123", "cycle": 1, "totalCycles": 16}

# 2. Look at the current state
curl <ORACLE_BASE_URL>/expedition/abc-123 -H "Authorization: Bearer $TOKEN"
# -> tick, gates[], voyages[] (only voyages requested so far), metrics, ...

# 3. Submit gate assignments for any voyages you want to send this tick
curl -X POST <ORACLE_BASE_URL>/cycle/abc-123/schedule \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '[{"gateId": 1, "voyageId": 4}]'
# -> the new state, one tick later

# Repeat step 3 (an empty array [] is fine — it just advances the clock)
# until the response looks like:
# {"finished": true, "overallScore": 74.0, "metrics": {...}}
```

The dumbest scheduler that still works: each tick, look at every voyage with
`"status": "boarding"`, and assign it to the first gate with enough spare
power and containment. That's a complete, valid scheduler. It won't score
well, but it proves your plumbing works end to end — and gives you
something real to write tests against before you start layering in
strategy.

**One `expeditionId` covers the whole expedition.** You don't track a
separate ID per cycle — the Oracle runs 16 cycles back-to-back under one ID
and tells you when it moves on (see §6).

### The pieces you'll need to write

1. **A loop**: call `GET /expedition/{id}`, decide assignments, `POST
   /cycle/{id}/schedule`, repeat until `finished: true`.
2. **A way to tell which voyages are actually schedulable right now**: only
   voyages with `"status": "boarding"` can be assigned.
3. **A way to check whether a gate has room**: compare a voyage's
   `requiredPower`/`requiredContainment` against a gate's `availablePower`/
   `availableContainment` — accounting for other voyages you're assigning
   to the *same* gate in the *same* batch, since the Oracle checks capacity
   across your whole submitted batch, not one assignment at a time.
4. **Handling of rejections**: any assignment the Oracle can't apply comes
   back in a `"rejected"` array with a specific reason (e.g. `"gate 4 has
   insufficient power for voyage 18 (needs 6, has 3)"`) — it doesn't fail
   your whole request, just that one assignment.

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
- `priority` is 1 (low) to 5 (critical) — the Oracle doesn't enforce
  anything based on it; what you do with it is up to your strategy.
- `arrivalDeadline` and `requestedTick` are simulation tick numbers, not
  wall-clock time.
- A voyage only appears in the API once its `requestedTick` has passed —
  you can't see or plan around travelers who haven't requested transit yet.
  New voyages can appear mid-run.
- `prerequisites` are other voyage IDs that must reach `arrived` before
  *this* voyage can become `boarding` — a different concept from `legs`
  below, which is about one voyage's own multi-hop journey, not its
  dependency on other voyages.
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
- Gates can go **temporarily offline** (a rift destabilization). While
  `operational` is `false`, you can't assign it new work. Any voyages that
  were in transit through it get **kicked back to `boarding`** (progress
  preserved, assignment lost) — the Oracle doesn't reschedule them for you.
  A scheduler that never notices a gate went offline will quietly bleed
  score.

### Assignment (what you submit)

```json
{"gateId": 1, "voyageId": 18}
```

Submit an array of these to `POST /cycle/{id}/schedule`. `[]` is valid and
just advances the clock with no new assignments.

### Multi-hop corridors

Some voyages (roughly 15%, picked at random) aren't a single gate hop —
they're a corridor of 2-3 legs that must be completed **in order**, each
through its own gate assignment:

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

- `legs` is the voyage's *entire planned corridor*, visible up front so you
  can plan ahead — but you can only ever assign the voyage to a gate for
  its *current* leg.
- The top-level `requiredPower` / `requiredContainment` / `estimatedDuration`
  / `remainingDuration` always describe **the current leg** (`legs[legIndex]`),
  not the whole trip — assign against those fields exactly like a normal
  voyage; you don't need any special-case logic to schedule a corridor leg.
- When a leg completes, the voyage does **not** arrive yet (unless it was
  the last leg): `legIndex` advances, the top-level fields update to reflect
  the next leg, `assignedGate` clears, and `status` goes back to `boarding`.
  It only becomes `arrived` — and only then counts toward `throughput`,
  `arrivalSuccess`, etc. — once the *last* leg completes.
- The one `arrivalDeadline` on the voyage covers the whole corridor, not
  each leg individually.
- A scheduler that treats every "boarding" voyage identically will handle
  this correctly by accident (it just re-appears asking to be scheduled
  again) — but one that assumes "assigned once" means "done" will silently
  under-report a corridor voyage's real gate demand and get surprised when
  it reappears.

### Premium hubs & SLA

Each cycle designates 1-2 origin hubs as **premium** — paying for a
tighter guarantee than the standard `arrivalDeadline`. You're told which
hubs those are (`premiumHubs` on the cycle state — see §6), and any voyage
from one of them additionally carries an `slaDeadline`, always tighter than
its `arrivalDeadline`:

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
is deliberately the "premium projects should finish sooner without
starving everyone else" tension made concrete — `fairness` is still
watching, so a scheduler that just always drains premium hubs first will
win `slaCompliance` and lose `fairness`. Which tradeoff you make (and being
able to say why) is the point.

---

## 6. How an expedition is structured

One expedition = **16 cycles**, run back to back under the same
`expeditionId`. Each cycle is a fresh, independent transit scenario drawn
from one of six fixed profiles, so you face a representative spread of
conditions rather than one lucky or unlucky random draw:

| Profile | What it stresses |
| --- | --- |
| `transfer_chains` | Few gates, long chains of connecting voyages |
| `surge_arrivals` | Many gates, hundreds of small voyages requested in a wave |
| `deep_rift` | Few voyages, but massive power/containment draw each |
| `narrow_window` | Arrival deadlines tight relative to transit duration |
| `gate_congestion` | Far more voyages than your gates can absorb at once |
| `mixed_traffic` | A general mix of all of the above |

Every expedition repeats full passes through all six profiles as many times
as fit (16 cycles = two full passes plus a partial third), draws any
leftover cycles from a shuffled partial pass rather than fully at random,
and shuffles the final order — so no single profile can dominate an
expedition by luck. When one cycle finishes, the Oracle automatically
starts the next — keep polling/scheduling against the same `expeditionId`
throughout. When all 16 are done, you get:

```json
{"finished": true, "overallScore": 74.0, "metrics": {"throughput": 94.4, "gateUtilization": 44.8, "arrivalSuccess": 68.6, "fairness": 61.7, "reliability": 100, "slaCompliance": 82.0}}
```

`overallScore` is the **average across all 16 cycles** — consistency across
varied conditions matters more than a spiky best case.

---

## 7. What's being measured

Six metrics, computed continuously: **throughput** (% of voyages that
arrived), **gateUtilization** (% of gate capacity kept busy),
**arrivalSuccess** (% of arrived voyages that beat their deadline),
**fairness** (penalizes starving one origin hub's travelers to favor
another), **reliability** (% of your submitted assignments that were
valid), **slaCompliance** (% of premium-hub voyages that beat their tighter
SLA window — see §5's "Premium hubs & SLA"). `overallScore` is a weighted
blend of all six — we're not publishing the exact weights, same reasoning
as §1: this isn't a formula to solve for.

---

## 8. What to submit

Your whole submission — code, `README.md`, `DESIGN.md`, tests, all of it —
needs to live in a GitHub repo (public, or shared with whoever's reviewing
it). That repo *is* the submission.

1. **A working scheduler** that runs one or more full expeditions against
   the Oracle end to end.
2. **`README.md`** — setup, how to run it, dependencies, project layout.
   Assume the reader has never seen your project before.
3. **`DESIGN.md`** (max 2 pages) — not a tour of every class, but how you
   *thought* about the problem:
   - **Initial design** — what was your first approach, and why?
   - **Iteration** — what changed as you developed? Did your strategy
     evolve after seeing your scores?
   - **Tradeoffs** — what did you deliberately optimize for? What did you
     deliberately *not* optimize, and why was that the right call?
   - **Challenges** — what was the hardest engineering problem you ran into?
   - **Future work** — with another weekend, what would you build next?
   - **Reflection** — what part of this are you most proud of?
4. **Tests** — automated, whatever amount and style you think is
   appropriate. A few tests that reflect real judgment about what's worth
   testing beats 100% coverage with no thought behind it.

---

## 9. If you're not sure where to start

Treat this like any other piece of software you're building, not like a
puzzle to solve in one sitting:

1. **Get a thin vertical slice working end to end** — the loop in §4, with
   the dumbest possible logic. Confirm you can see `finished: true`.
2. **Write a test for your assignment logic in isolation** — feed it a
   handful of fake voyages and gates, assert what it decides, without a live
   Oracle connection. If that's awkward to write, that's a signal your
   scheduling logic is too tangled up with your HTTP code.
3. **Make the strategy swappable.** Even if you only ever write one
   strategy, structure it so a second one could exist — it's a good forcing
   function for a clean interface between "talk to the Oracle" and "decide
   what to schedule."
4. **Now iterate on the decision logic itself** — try a real prioritization
   rule (earliest deadline, or highest priority), see what it does to your
   metrics, and make a deliberate call about the next tradeoff instead of
   guessing.
5. **Handle gate outages explicitly** — notice when a voyage you previously
   assigned comes back to `boarding` with a different (or no)
   `assignedGate`.
6. **Only then worry about corridors and premium hubs** — a scheduler that
   just treats every "boarding" voyage the same way already handles
   multi-leg corridors correctly by construction; premium hubs are a
   genuine policy decision (favor them how much, at what cost to fairness?)
   worth making deliberately once your basics are solid, not before.
7. **Write `DESIGN.md` as you go**, not at the end. It's much easier to
   record your reasoning in the moment than to reconstruct it afterward,
   and it'll read more honestly for it.

One optional idea if you want to push further, and it has nothing to do
with scheduling logic: treat the Oracle like the real, occasionally-flaky
network dependency it is. Nothing here requires it — a request either
succeeds or it doesn't, and the loop in §4 works fine without any of this —
but a client that retries transient failures with backoff, and re-checks
state rather than blindly resubmitting after an ambiguous failure (a
request can fail on your end after the Oracle already applied it), is
demonstrating a kind of production-mindedness that's genuinely rare to see
and worth showing off if you have the time for it.

There's no house style here — a scheduler that's a single clean greedy loop
with excellent tests and a sharp writeup is a completely legitimate answer.
So is an elaborate multi-strategy system. We want to see how *you* think
and build, not whether you can guess what we had in mind.
