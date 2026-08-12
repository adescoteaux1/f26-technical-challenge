# Interviewer Guide — Nexus Transit Authority Scheduler Challenge

**Internal only — do not share with applicants.** This is the answer key:
scoring weights, calibration data, and question prompts that would let
someone game the rubric if they saw it in advance.

This guide is for whoever sits in on an applicant's walkthrough of their
scheduler submission. It assumes you've read `CHALLENGE.md` (what the
applicant was given) but may not have touched the Control Tower's internals
recently.

**Finding a candidate's repo:** `CHALLENGE.md` §0 requires applicants to get
their repo via `/apply` rather than create their own — it'll be under the
org as `f26-challenge-<their-github-username>`, not wherever they might tell
you. If someone points you at a different repo, that's worth a question on
its own.

---

## 1. What this is actually testing

Per `CHALLENGE.md` §1: the challenge is deliberately not "write a clever
assignment loop." It's meant to surface the same judgment calls as real
engineering work — tradeoffs, testability, maintainability, and the ability
to explain *why*, not just *what*. Keep that framing during the
walkthrough: an applicant who wrote a simple greedy scheduler but can
clearly explain what they traded off and why is a stronger signal than one
who wrote something elaborate they can't defend.

The submission has four required parts (`CHALLENGE.md` §8) — code,
`README.md`, `DESIGN.md`, tests — and all four are fair game to probe, not
just the code.

## 2. The Control Tower, briefly

You don't need to re-derive this from source during an interview, but here's
the map if a question comes up:

- `internal/generator` — builds each cycle's workload from one of six fixed
  profiles (`transfer_chains`, `surge_arrivals`, `deep_rift`,
  `narrow_window`, `gate_congestion`, `mixed_traffic`), plus two orthogonal
  wrinkles applied to any profile: multi-hop corridors (~15% of voyages) and
  1-2 premium hubs per cycle with a tighter SLA.
- `internal/engine` — `validator.go` decides what's legal (capacity,
  operational gates, voyage status); `engine.go` advances the clock,
  progresses in-transit voyages, unlocks prerequisites, and rolls gates
  offline/online.
- `internal/scoring` — turns accumulated stats into the six metrics and the
  weighted overall score (weights below).
- `internal/evaluation` — orchestrates all of the above behind the three
  HTTP endpoints; owns profile sequencing and expedition-level aggregation.

An expedition is 16 cycles (two full passes through all six profiles plus a
shuffled partial third), and `overallScore`/the final `metrics` are the
**plain average across all 16** — not a sum, not weighted toward any
profile. A submission that does great on 5 profiles and falls apart on
`narrow_window` will show up as solid-but-unremarkable, not as an outlier —
that's by design (see `DESIGN.md`'s Iteration section on why cycle count
went from 8 to 16).

## 3. The scoring formula (never shown to applicants)

From `internal/scoring/scoring.go`:

| Metric | Weight | What it measures |
| --- | --- | --- |
| `throughput` | 0.20 | % of voyages that arrived |
| `arrivalSuccess` | 0.20 | % of arrived voyages that beat `arrivalDeadline` |
| `gateUtilization` | 0.15 | % of gate capacity kept busy |
| `fairness` | 0.15 | Penalizes large disparities in average queue wait time across origin hubs (coefficient-of-variation based; identical avg waits → 100, ≥1.5 CoV → 0) |
| `reliability` | 0.15 | % of submitted assignments that were valid |
| `slaCompliance` | 0.15 | % of premium-hub voyages that beat their tighter `slaDeadline` |

`throughput` and `arrivalSuccess` dominate slightly (0.20 each); everything
else is 0.15. These aren't tuned against real scheduler behavior (per
`DESIGN.md`'s Tradeoffs section, they're "principled starting guesses, not
calibrated") — don't treat a specific point value as more meaningful than
the general shape it implies.

## 4. Score calibration — what's actually plausible

This is the single most useful thing in this doc: **don't eyeball a score
in isolation.** From `examples-internal/README.md`'s measured runs (current
16-cycle, corridors+SLA config):

| Scheduler | overallScore | throughput | gateUtilization | arrivalSuccess | fairness | reliability | slaCompliance |
| --- | --- | --- | --- | --- | --- | --- | --- |
| naive greedy (`simple_scheduler.py`) | 68.5 | 100 | 46.4 | 60.0 | 54.9 | 100 | 42.2 |
| deliberate, SLA+corridor-aware (`better_scheduler.py` v3) | 72.0 | 95.5 | 45.2 | 65.9 | 65.1 | 100 | 54.6 |

Takeaways to actually use in an interview:

- **The floor is higher than you'd guess and the ceiling is closer than
  you'd guess.** A correct-but-naive greedy scheduler already lands ~68-70
  overall. A thoughtfully designed one only reached ~72 in testing. Runs
  vary (workload profiles are randomized per expedition), so treat anything
  in roughly the **65-78** range as "unsurprising" regardless of how
  sophisticated the design sounds in the walkthrough.
- **Be skeptical, not impressed, by scores well above ~80.** That's outside
  what deliberate internal reference solutions have achieved. Ask how they
  measured it — one lucky run? Fewer than 16 cycles? A bug that inflates one
  metric while a candidate didn't notice fairness or SLA collapsing? Ask
  them to run it again and compare.
- **A "smarter-sounding" heuristic can score worse.** The internal
  `better_scheduler` v1 (raw earliest-deadline-first, ignoring how long a
  voyage takes) *lost* to the naive baseline despite winning on
  `gateUtilization` and `fairness` — its `arrivalSuccess` collapsed because
  long voyages with distant-but-real deadlines hogged gates ahead of
  short-fuse ones. When an applicant describes a heuristic, ask what it
  actually did to their metrics, not just whether it sounds more
  sophisticated than greedy-first-fit.
- **A generic heuristic doesn't automatically help a metric it wasn't aimed
  at.** `slaCompliance` only moved decisively once a scheduler treated
  premium-hub urgency as its own explicit policy (using `slaDeadline` as the
  effective deadline), not as a side effect of general prioritization. If an
  applicant's `slaCompliance` is high, ask what specifically their scheduler
  does differently for premium-hub voyages — "nothing, it just happened"
  is a plausible and slightly concerning answer.

## 5. Suggested walkthrough flow & question bank

Use `CHALLENGE.md` §9's own progression (thin slice → isolated tests →
swappable strategy → real prioritization → gate outages → corridors/premium
hubs) as your rubric ladder — it's a reasonable proxy for how far someone
got and how deliberately.

**Architecture / design**
- "Walk me through what happens between reading state and submitting an
  assignment." (Listening for: a clear seam between HTTP plumbing and
  decision logic — CHALLENGE.md §9 explicitly asks for a "swappable
  strategy.")
- "How would you plug in a second scheduling strategy?" (If the answer is
  "I'd have to rewrite a lot," that's a real signal, not a nitpick.)

**Core mechanics — checks for actual engagement vs. lucky defaults**
- "How do you make sure you don't over-commit a gate within one batch?"
  (The Control Tower checks capacity across the *whole submitted batch*, not
  assignment-by-assignment — a submission that doesn't track this locally
  would see rejections and should have noticed.)
- "What does your scheduler do when a gate goes offline mid-voyage?" A
  surprising number of naive submissions never explicitly handle this —
  the voyage silently reappears as `boarding` with a cleared
  `assignedGate`, and a scheduler that doesn't distinguish "new voyage" from
  "bounced-back voyage" will still work, but ask if they *noticed* this
  behavior and whether they did anything deliberate about it.
- "Walk me through how a multi-hop corridor voyage is represented, and what
  your scheduler does differently for one." Correct answer: *nothing extra
  is required* — the top-level fields always describe the current leg. If
  they added special-case logic, ask why, and check whether it's actually
  correct (a common bug: assuming leg 1 completing means the voyage is
  done).
- "Tell me about the premium-hub/SLA tension." Listen for whether they
  identified this as a real tradeoff (favor premium hubs vs. fairness) and
  made a deliberate call, vs. either ignoring premium hubs entirely or
  favoring them unconditionally (wins `slaCompliance`, tanks `fairness` —
  see §4 above).
- "What's in your `rejected` handling?" If assignments were ever rejected
  and they didn't notice/adjust, that's worth exploring — did they even log
  it?

**Process / iteration (the part a lot of candidates under-invest in)**
- "What was your first working version, and what's different now?" Wants a
  specific story with before/after numbers, not "I refactored for
  cleanliness." `DESIGN.md`'s own Iteration section (this repo's, about the
  Control Tower) is a good model of the level of specificity to look for.
- "What did you deliberately *not* do, and why?" A thoughtful "not
  optimized for X because Y" answer is a stronger signal than someone who
  claims to have optimized everything.
- "Show me a test you're proud of, and one you decided not to write."
  Per `CHALLENGE.md` §9 step 2: can they test scheduling logic without a
  live Control Tower connection? If not, that's a sign the decision logic is
  tangled up with the HTTP layer.
- If they attempted the retry/resilience extension (`CHALLENGE.md` §10):
  "What do you do if a request times out but the Control Tower actually applied
  it?" Listen for "I re-check state before resubmitting," not "I just
  retry the same request."

## 6. Red flags

- Score claims noticeably outside the ~65-78 band without a story for why
  (see §4).
- Can't explain *any* tradeoff — everything is framed as strictly better
  with no cost, which usually means they didn't actually compare.
- No awareness of gate outages or the `rejected` array at all.
- Tests that only run against a live Control Tower (can't test decision logic in
  isolation) — a specific "your scheduling logic is too tangled with your
  HTTP code" smell called out directly in `CHALLENGE.md` §9.
- `DESIGN.md` that reads like it was written in one sitting at the end —
  vague "iteration" section, no real before/after, no specific numbers.
- Unconditionally draining premium hubs first (or ignoring them
  entirely) — either one means they didn't engage with the tension the
  feature exists to create.

## 7. Current Control Tower state (as of 2026-08-08)

Two things changed recently that are worth knowing if you're comparing
notes with an older interview or a candidate's earlier test runs:

1. **Concurrency fix.** `evaluation.Submit`'s read-modify-write now runs
   under a per-expedition lock (`Store.WithExpeditionLock`), so overlapping
   `POST /cycle/{id}/schedule` calls for the same expedition (e.g. a
   naive retry racing the original request) can no longer silently clobber
   each other's result. This mostly matters for applicants who attempted
   the §10 resilience extension with real concurrent retries.
2. **API docs and wire format now match `CHALLENGE.md`'s examples exactly.**
   Previously, fields like `assignedGate`, `slaDeadline`, `legs`, etc. were
   silently *omitted* from the JSON when null instead of sent as explicit
   `null` (contradicting the documented example), and the generated
   `/docs` reference had no field descriptions or enum values at all. Both
   are fixed. If an applicant's client code defensively checks for missing
   keys instead of null values, that's not a bug in their submission — it
   was a reasonable defensive read of the old behavior.

## 8. Calibration runs

`examples-internal/simple_scheduler.py` and `better_scheduler.py` are the
two reference points behind §4's numbers. Re-running them occasionally
(`cd examples-internal && python simple_scheduler.py`) against the current
Control Tower is a good sanity check if a batch of interviews starts producing
scores that don't match this guide's expectations — the Control Tower's tuning
could have drifted, not just the applicant pool.
