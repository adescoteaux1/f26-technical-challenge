# Generate Cloud Scheduler — Challenge

You're going to build a **scheduler**: a program that decides which jobs run
on which workers, and when. It talks to a server we call the **Oracle**,
which generates the workload, simulates execution, and scores how you did.

There is no known optimal strategy for this problem. We expect everyone to
make different tradeoffs, and we're at least as interested in how you
approached the problem — what you optimized for, what you deliberately
didn't, how your thinking evolved — as we are in the final score. A
brilliantly-engineered scheduler with a modest score and a clear explanation
of its tradeoffs can outscore a slightly-faster one with no story behind it.

**You have total freedom on implementation.** Any language, any libraries,
any architecture, any project structure. Command-line tool, long-running
daemon, a pile of shell scripts — doesn't matter. The only hard requirement
is that your program speaks HTTP/JSON to the Oracle correctly. Show off
whatever you're good at: a clean architecture, a clever algorithm, thorough
tests, a visualization of your scheduler's behavior — this is an open
canvas.

If you're newer to this kind of project, don't worry about finding the
"right" approach before you start. Section 3 below gives you a literal
copy-pasteable path to a working (if not very smart) scheduler in a few
minutes; everything past that is iteration.

---

## 1. The problem

Generate Cloud runs background jobs for many product teams — AI inference,
report generation, image processing, email delivery, analytics pipelines,
database syncs. Jobs have resource requirements, priorities, deadlines, and
sometimes dependencies on each other. Workers have limited CPU and memory,
and can run multiple jobs at once if they have room, or go temporarily
unavailable.

Your scheduler's job, every tick, is to decide: **which ready jobs get
assigned to which workers.** The Oracle handles everything else — advancing
time, running jobs to completion, unlocking dependencies, generating new
work, and tracking your score.

There are several competing objectives (finish work quickly, use workers
efficiently, hit deadlines, don't starve any one project, adapt when things
change) and optimizing hard for one usually costs you on another. Deciding
which tradeoffs to make **is** the challenge.

---

## 2. Registering and getting a token

Before anything else, register once:

```bash
curl -X POST <ORACLE_BASE_URL>/register \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "nuid": "001234567"}'
```

```json
{"userId": "...", "token": "a1b2c3..."}
```

Save that `token`. Every other request must include it:

```
Authorization: Bearer <token>
```

There's no password — email + NUID is your credential pair. If you lose
your token, `POST /login` with the same `{email, nuid}` gets you a new one
(this invalidates the old one, so only one token per person is ever valid at
a time).

Everything you run is tied to your account, and you can look back at your
own history any time:

```bash
curl <ORACLE_BASE_URL>/me/evaluations -H "Authorization: Bearer $TOKEN"
```

```json
[
  {"evaluationId": "...", "finished": true, "overallScore": 74.0,
   "metrics": {"throughput": 94.4, "workerUtilization": 44.8, "deadlineSuccess": 68.6, "fairness": 61.7, "reliability": 100}, "createdAt": "..."}
]
```

Run as many evaluations as you want while you iterate — only your final
submission (however you and your reviewer agree to identify it) counts, but
seeing your own history is there so you can track whether you're actually
improving run over run.

---

## 3. Quick start: the simplest possible loop

This is the entire shape of the interaction. Get this working first, with
the dumbest possible scheduling logic, before you write anything clever.

```bash
TOKEN="..."   # from /register or /login

# 1. Start an evaluation
curl -X POST <ORACLE_BASE_URL>/evaluation -H "Authorization: Bearer $TOKEN"
# -> {"evaluationId": "abc-123", "simulation": 1, "totalSimulations": 8}

# 2. Look at the current state
curl <ORACLE_BASE_URL>/evaluation/abc-123 -H "Authorization: Bearer $TOKEN"
# -> tick, workers[], jobs[] (only jobs that have arrived so far), metrics, ...

# 3. Submit assignments for any jobs you want to schedule this tick
curl -X POST <ORACLE_BASE_URL>/simulation/abc-123/schedule \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '[{"workerId": 1, "jobId": 4}]'
# -> the new state, one tick later

# Repeat step 3 (an empty array [] is fine — it just advances the clock)
# until the response looks like:
# {"finished": true, "overallScore": 74.0, "metrics": {...}}
```

The dumbest scheduler that still works: each tick, look at every job with
`"status": "ready"`, and assign it to the first worker with enough spare CPU
and memory. That's it — that's a complete, valid scheduler. It won't score
well, but it proves your plumbing works end to end, and every improvement
past this point is a design decision you get to make (and defend in your
writeup).

**One `evaluationId` covers the whole evaluation.** You don't track a
separate ID per simulation — the Oracle runs 8 simulations back-to-back
under one ID and tells you when it moves on (see §5).

---

## 3.1 The pieces you'll need to write

1. **A loop**: call `GET /evaluation/{id}`, decide assignments, `POST
   /simulation/{id}/schedule`, repeat until `finished: true`.
2. **A way to tell which jobs are actually schedulable right now**: only
   jobs with `"status": "ready"` can be assigned. Everything else (`blocked`,
   `running`, `completed`) will be rejected if you try.
3. **A way to check whether a worker has room**: compare a job's
   `requiredCpu`/`requiredMemory` against a worker's `availableCpu`/
   `availableMemory` — and remember to account for other jobs you're
   assigning to the *same* worker in the *same* batch, since the Oracle
   checks resource usage across your whole submitted batch, not just one
   assignment at a time.
4. **Handling of rejections**: any assignment the Oracle can't apply comes
   back in a `"rejected"` array with a specific reason (e.g. `"worker 4 has
   insufficient CPU for job 18 (needs 6, has 3)"`) — it doesn't fail your
   whole request, just that one assignment. Read these while you're
   developing; they're the fastest way to find bugs in your logic.

---

## 4. The domain model

### Job

```json
{
  "id": 18,
  "project": "analytics-pipeline",
  "priority": 4,
  "estimatedRuntime": 6,
  "requiredCpu": 3,
  "requiredMemory": 1,
  "deadline": 48,
  "dependencies": [12, 15],
  "arrivalTick": 10,
  "status": "ready",
  "remainingRuntime": 6,
  "assignedWorker": null,
  "startTick": null,
  "completionTick": null
}
```

- `status` is one of `blocked` (dependencies incomplete), `ready`
  (schedulable now), `running` (assigned), `completed`.
- `priority` is 1 (low) to 5 (critical) — the Oracle doesn't enforce
  anything based on it; what you do with it is entirely up to your
  strategy.
- `deadline` and `arrivalTick` are both simulation tick numbers, not
  wall-clock time.
- A job only appears in the API at all once its `arrivalTick` has passed —
  you can't see (or plan around) jobs that haven't arrived yet. New jobs can
  appear mid-run.
- `dependencies` are other job IDs that must reach `completed` before this
  job can become `ready`. A job with no dependencies is `ready` as soon as
  it arrives.

### Worker

```json
{
  "id": 1,
  "totalCpu": 8,
  "totalMemory": 16,
  "availableCpu": 5,
  "availableMemory": 12,
  "runningJobs": [7, 9],
  "available": true,
  "unavailableUntil": 0
}
```

- Workers can run multiple jobs at once as long as `availableCpu` /
  `availableMemory` cover them.
- Workers can go **temporarily unavailable** (simulating an outage).
  While `available` is `false`, you can't assign new work to it. Any jobs
  that were running on it at the time get **kicked back to `ready`** (their
  progress toward `remainingRuntime` is preserved, but they're unassigned
  and need to be rescheduled) — the Oracle doesn't do this for you
  automatically. A scheduler that never notices a worker went down will
  quietly bleed throughput and reliability.

### Assignment (what you submit)

```json
{"workerId": 1, "jobId": 18}
```

Submit an array of these to `POST /simulation/{id}/schedule`. `[]` is valid
and just advances the clock with no new assignments.

---

## 5. How an evaluation is structured

One evaluation = **8 simulations**, run back to back under the same
`evaluationId`. Each simulation is a fresh, independent workload — different
jobs, different workers, different failure patterns — drawn from one of six
fixed **profiles**:

| Profile | What it stresses |
|---|---|
| `dependency_chains` | Few workers, long chains of dependent jobs — tests how well you keep a critical path moving |
| `burst_traffic` | Many workers, hundreds of small jobs arriving in a sudden wave — tests throughput under load |
| `heavy_compute` | Few jobs, but large and long-running — tests how you pack scarce, expensive resources |
| `deadline_critical` | Deadlines tight relative to runtime — tests prioritization under time pressure |
| `resource_constrained` | Far more jobs than your workers can absorb at once — tests queuing/triage decisions |
| `balanced` | A general mix of all of the above |

Every evaluation guarantees all six profiles show up at least once (plus two
extra random draws), in a shuffled order you can't predict in advance. This
is deliberate: everyone faces the same *range* of difficulty, so nobody gets
lucky (or unlucky) with an easy draw, and your score reflects how well your
strategy generalizes rather than how well it fits one workload.

When one simulation finishes, the Oracle automatically starts the next —
you keep polling/scheduling against the same `evaluationId` the whole time.
When all 8 are done, `GET /evaluation/{id}` (or the response from your last
`schedule` call) returns:

```json
{"finished": true, "overallScore": 74.0, "metrics": {"throughput": 94.4, "workerUtilization": 44.8, "deadlineSuccess": 68.6, "fairness": 61.7, "reliability": 100}}
```

Your `overallScore` is the **average across all 8 simulations** — a
scheduler that's great on `heavy_compute` but falls apart on
`burst_traffic` will score worse than one that's solidly good across the
board. Consistency is rewarded over a spiky best case.

---

## 6. What's being measured

Five metrics, computed continuously (not just at the end):

- **throughput** — % of the simulation's jobs you got to `completed`
- **workerUtilization** — % of total worker capacity actually kept busy
- **deadlineSuccess** — % of completed jobs that finished by their deadline
- **fairness** — penalizes big disparities in average wait time *between
  projects* — a scheduler that always favors one project and starves
  another scores worse here even with high throughput
- **reliability** — % of your submitted assignments that were actually
  valid (not rejected)

`overallScore` is a weighted blend of these five. We're intentionally not
publishing the exact weights — same as the real prompt this is modeled on,
the point isn't to let you solve for a formula, it's to get you thinking
about the shape of the tradeoff space. Every one of these will pull against
at least one of the others eventually; deciding which ones you care about
most, and being able to explain why, is the actual exercise.

---

## 7. What to submit

1. **A working scheduler** that runs one or more full evaluations against
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
   appropriate. We'd rather see a few tests that reflect real judgment about
   what's worth testing than 100% coverage with no thought behind it.

## 8. How this gets evaluated

| Category | Weight |
|---|---|
| Correctness | 25% |
| Scheduling performance | 20% |
| Code quality | 20% |
| Architecture | 15% |
| Testing | 10% |
| Documentation | 10% |

Notice that your `overallScore` from the Oracle only feeds one 20% slice of
this. A clean, well-tested, thoughtfully-documented scheduler with a decent
(not spectacular) score is a *stronger* submission than a top-score
scheduler that's an unreadable pile of hacks. Optimize accordingly.

## 9. A reasonable path if you're not sure where to start

1. Get the "assign any ready job to the first worker with room" loop from
   §3 running end to end. Confirm you can see a `finished: true` response.
2. Add a real priority: e.g. schedule the job with the earliest deadline
   first, or the highest `priority`, when multiple jobs compete for the
   same worker.
3. Add fairness awareness: don't let one project's jobs always win — round
   robin across projects, or explicitly deprioritize a project that's
   already gotten a lot of scheduling attention recently.
4. Handle worker failure explicitly: notice when a job you previously
   assigned is back to `ready` with a different (or no) `assignedWorker`,
   and make sure your bookkeeping doesn't get confused by it.
5. Look at your metrics after a full run, figure out which one is dragging
   your score down, and go fix *that* specifically rather than
   re-architecting everything.
6. Write down what you tried and why in `DESIGN.md` as you go — it's much
   easier to write truthfully in the moment than to reconstruct
   after the fact.

Have fun with it. There's no house style here — a scheduler that's a single
clean greedy loop with excellent tests and a sharp writeup is a completely
legitimate answer, and so is an elaborate multi-strategy system with a
simulated-annealing tuner. We want to see how *you* think, not whether you
can guess what we had in mind.
