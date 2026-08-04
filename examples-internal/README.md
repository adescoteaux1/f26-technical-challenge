# Internal reference solutions — do not share externally

Two example scheduler clients against the Oracle, used to calibrate what
"simple" and "better" look like when reviewing an applicant's actual
submission. **These are internal-only.** If this repo or `CHALLENGE.md` is
ever handed to applicants, this folder must be stripped out first — it's an
answer key.

- `simple_scheduler.py` — a naive, greedy scheduler: assign any boarding
  voyage to the first gate with room, in ID order. No prioritization, no
  fairness, no awareness of gate outages beyond correctness, and no idea
  premium hubs exist. This is intentionally close to the "dumbest scheduler
  that still works" described in `CHALLENGE.md` §4 — it's the floor, not an
  example to aspire to. It handles multi-hop corridor voyages correctly
  anyway, purely by accident (see its docstring): it never has to know a
  voyage is mid-corridor because the fields it reads always describe the
  *current* leg.
- `better_scheduler.py` — orders voyages by *slack* (deadline minus
  remaining duration, not raw ticks-until-deadline — see the comment in
  `Scheduler._sort_key` for why pure earliest-deadline-first actually
  performed worse in testing), ties broken by priority then by which origin
  hub has been served least; does best-fit gate selection to reduce
  fragmentation. It also accounts for the two newer Oracle features
  deliberately rather than by accident: a premium-hub voyage's *effective*
  deadline for urgency purposes is its tighter `slaDeadline`, not the looser
  `arrivalDeadline` (so it only jumps the queue once its SLA is actually at
  risk, not unconditionally), and a multi-hop voyage's slack accounts for
  *all* its remaining legs, not just the current one. The strategy lives
  entirely in the `Scheduler` class (`decide()`), isolated from the HTTP
  loop in `run()`, so it's easy to see exactly what a "swappable strategy"
  architecture (per `CHALLENGE.md` §9) looks like in practice.

Neither is meant to be "the best possible scheduler" — they're reference
points on the spectrum, not a ceiling.

## A real caveat: scores vary run to run

The Oracle samples its workload profiles randomly per evaluation and isn't
seeded by the client (by design — see `DESIGN.md`), so two runs of the
*same* script will face different workload draws and won't produce
identical scores. The Oracle originally ran 8 cycles per evaluation with
fully-random extra profile draws; it now runs **16** cycles per evaluation
with a balanced-coverage sampler (see `DESIGN.md`'s Iteration section) to
reduce exactly this kind of noise. Actual live runs across both versions:

| Run | Cycles/sampling | overallScore | throughput | gateUtilization | arrivalSuccess | fairness | reliability | slaCompliance |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `simple_scheduler` | 8, old sampling | 74.4 | 95.0 | 42.2 | 70.8 | 63.7 | 100 | — |
| `better_scheduler` v1 (raw earliest-deadline-first) | 8, old sampling | 72.9 | 95.7 | 49.8 | 51.4 | 74.3 | 100 | — |
| `better_scheduler` v2 (slack-based) | 8, old sampling | 75.2 | 95.6 | 53.0 | 58.1 | 74.7 | 100 | — |
| `simple_scheduler` | 16, balanced sampling | 72.2 | 90.3 | 39.3 | 69.2 | 62.7 | 100 | — |
| `better_scheduler` v2 (slack-based) | 16, balanced sampling | 70.6 | 89.4 | 41.4 | 70.0 | 49.9 | 100 | — |
| `simple_scheduler` | 16, corridors + premium/SLA | 68.5 | 100 | 46.4 | 60.0 | 54.9 | 100 | 42.2 |
| `better_scheduler` v3 (SLA + corridor-aware slack) | 16, corridors + premium/SLA | 72.0 | 95.5 | 45.2 | 65.9 | 65.1 | 100 | 54.6 |

(`—` = the `slaCompliance` metric didn't exist yet at that point; those runs
predate the multi-hop corridor and premium-hub features documented in
`CHALLENGE.md` §5.)

Two things worth taking away from this, honestly:

1. **v1 lost to the naive baseline** despite winning on gateUtilization and
   fairness — its arrivalSuccess collapsed because sorting by raw deadline
   (ignoring how long a voyage takes) let long voyages with
   distant-but-real deadlines hog gates while short-fuse voyages piled up
   behind them. That's exactly the kind of gap a single metric can hide,
   and a reviewer should be alert to it in real submissions: check whether
   an applicant's "smarter" heuristic actually improves the metrics it's
   aimed at, not just whether it sounds more sophisticated.
2. **Even after the fix, v2 doesn't reliably beat `simple_scheduler`** on
   overallScore — it won one run and lost two, and its fairness win (the
   most consistent advantage in the 8-cycle runs) flipped to a loss in the
   16-cycle run. Don't read that as the extra effort in `better_scheduler`
   being wasted: it still wins gateUtilization in every run and usually
   wins fairness. It means the specific heuristics here (slack ordering,
   best-fit, a simple hub tiebreak) aren't enough of an edge to dominate a
   correct greedy baseline under this Oracle's current metric weights — a
   real gap in the reference solution, not a bug. Take that as a signal
   about how much headroom actually exists here, not as "any naive
   scheduler is basically as good as a thoughtful one." Don't treat any
   single run's numbers as gospel — run a few times if you want a stable
   read, and expect real submissions to vary the same way.
3. **v3 is the first version to win decisively** — better on overallScore,
   arrivalSuccess, fairness, *and* slaCompliance, losing only narrowly on
   throughput. The difference from v2 isn't more cleverness in general, it's
   that v3 responds to the *specific new metric* directly (SLA-aware
   effective deadline) instead of relying on general-purpose heuristics to
   accidentally help it. That's worth remembering when reviewing a
   submission that added a feature-specific policy after seeing its
   scores versus one that just tuned existing heuristics harder.

## Running them

```bash
cd examples-internal
pip install -r requirements.txt

# against a locally running Oracle (see repo root README.md to start it)
python simple_scheduler.py
python better_scheduler.py
```

Both scripts read `ORACLE_BASE_URL` (default `http://localhost:8080`) and
will register a fixed internal account on first run, then log back in on
subsequent runs (so repeated runs accumulate history under the same
account — useful for watching how a script's score holds up as the Oracle
itself changes).
