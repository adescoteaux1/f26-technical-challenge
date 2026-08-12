# Reviewing the Operations Console Challenge

**Internal only — do not share with applicants.** This names the specific
traps the challenge is built around.

Applicant-facing spec: `FRONTEND_CHALLENGE.md`.

---

## What it tests

Part 1 gives them a design and a known-correct answer: it tests execution.
Part 2 gives them an API, no design, and a 30% failure rate: it tests how
they decide when nobody has decided for them. `DESIGN.md` is where that
reasoning is visible.

Part 2 is deliberately open-ended. There is no implementation we're looking
for, and several quite different builds can all be good ones. What you're
judging is whether the choices were made on purpose.

## Review order

1. **Watch the recording** in their `README.md`. Fastest look at how the states actually behave.
2. **Read `DESIGN.md`** before the code, so their account lands before your
   own opinion of it.
3. **Run it and break it.** Play around: book a slot, submit until it
   fails, page to the bottom of the list. Six or so submissions hits every
   outcome, and most of the signal is here.
4. **Read the code** with specific questions, not as a tour.

---

## Part 1 — execution

- [ ] Pages through all 46 bookings: no duplicates, no gaps, terminates.
- [ ] Every state renders, and is distinguishable at a glance: three portal
      statuses, four booking statuses, and the two easy misses — `load:
null` shown as absent rather than `0%` or `NaN`, and `load: 0` with
      `status: nominal` shown as healthy rather than offline.
- [ ] `4 / 6` count is computed from the data, not hardcoded — it should
      track the network state as it changes.
- [ ] Not polling per-second for state that changes hourly.

---

## Part 2 — judgment

**Check this first.** `slot_taken` and `insufficient_seats` return **HTTP
200** with a failure `status` in the body; only `corridor_unstable` is a
`503`. A client branching on `res.ok` alone shows **a success screen for a
booking that never happened**, roughly 1 submission in 6. Book the same slot
repeatedly until you see it. It's the most reliable discriminator here, and
not a gotcha — the outcome table is in the spec.

- [ ] `corridor_unstable` (retryable) and `slot_taken` (terminal) lead to
      different behaviour, not one shared error path.
- [ ] Form contents survive a failed submission.
- [ ] Double-submit guarded: button disabled in flight, or request locked.
- [ ] Nothing says "Booked" before a `confirmed` status with a `reference`.
- [ ] Error text is legible to a traveler, not raw JSON or `Error 503`.
- [ ] The flow has a deliberate and design choices taken, preferrably their DESIGN.md talks about this

---

## `DESIGN.md`

**Strong.** Names the alternative they rejected. Says what they cut and why
that was right at this scope. Talks about the traveler rather than the
components (aka talks about the user and how they inteact with the product rather than pure implementation), and about decisions rather than implementation.

**Weak.** Narrates the code. Justifies every choice as standard practice.
Mentions no tradeoffs. Describes the API back at us.

---

## Engineering worth noticing

- [ ] Submission is one state, not separate `isLoading` / `isError` /
      `isSuccess` flags that admit impossible combinations.
- [ ] One shared API client, not `fetch` scattered through components.
      Independent requests run in parallel; the same request doesn't fire
      once per component that wants it (aka look at how data travel between different nodes of the components, are they prop drilling where applicable?).
- [ ] Next page in the pagination appends rather than refetching everything so far.
- [ ] Loading and empty states exist and were designed and considered.
- [ ] Focus moves somewhere sensible after a failure (good redirect)

**Component design.** Look at the repeated pieces — portal card, booking
row, status badge. Reused with props, or copy-pasted per usage? Are
variants handled systematically (something like class-variance-authority,
or any typed variant map) rather than nested ternaries inside `className`?
A useful test: adding a fifth booking status should touch one place.

**Credit the unasked-for.** Responsiveness, keyboard support, skeleton
states, a real empty state, sensible focus order — none of it was required.
Someone who did it anyway decided to, and it reflects a lot in their attention to detail.

**Then ask what they prioritized.** A first pass that works end to end beats one finished corner:
pixel-perfect styling on a flow that can't complete a booking is worth less
than a plain one that can Look for a throughline that ran early and got better. 

---

## Look our for

Mocked data instead of the live API · any success state reachable without a
`confirmed` status · retrying `slot_taken` in a loop · `detail` injected as HTML ·

## Don't penalize

Framework choice or technologies used, we mentioned that anything is fair game.

---

## Interview questions

Not a list to work through. Ask about the tradeoffs you noticed while
reviewing, and skip anything that doesn't fit what's in front of you.

By this point you've watched it run and read the code, so asking what it
does is wasted time. What's left is why it's that way, and what would have
to change for it to be different. Some examples of that shape:

- "You built [their flow shape]. What would push you toward [the other
  one]?"
- "On a submission failure you [what they do]. What made you land there
  rather than [an alternative]?"
- "`DESIGN.md` says you cut [X]. What would have to be true for it to be
  worth building?"
- "The operator runs this 200 times a shift, not once. What changes?"
- "At 200 slots instead of 18, what breaks first in what you built, and
  what would you want us to change about the endpoint?"
- "Where did you spend time you'd take back?"
