"""A more deliberate scheduler — still simple, but each choice is a tradeoff
made on purpose rather than "whatever's first."

Differences from simple_scheduler.py, and why:

  - Voyages are ordered by how close they are to missing their deadline
    (ties broken by priority, then by which origin hub has been served the
    least so far) instead of whatever order the Oracle happens to return
    them in. This directly targets arrivalSuccess without ignoring
    throughput: something with a deadline 200 ticks out can wait behind
    something with 3 ticks left.
  - Gate selection is best-fit (smallest sufficient gate) rather than
    first-fit, to reduce fragmentation — a gate landed with one huge voyage
    when a smaller gate would've done just as well is capacity another
    voyage can't use this tick.
  - A running tally of assignments per origin hub feeds the tie-break above,
    so one hub can't perpetually crowd out another when their voyages are
    otherwise equally urgent. This is deliberately a tie-break, not the
    primary sort key — fairness matters, but not at the cost of a voyage
    that's about to blow its deadline.

The strategy is isolated in the Scheduler class so it's obvious what would
need to change to try a different approach — nothing outside `decide`
knows or cares how assignments get chosen.
"""

from __future__ import annotations

from collections import defaultdict

import oracle_client as oracle

EMAIL = "better-scheduler@internal.local"
NUID = "000000002"


class Scheduler:
    def __init__(self) -> None:
        # How many voyages we've assigned per origin hub so far, across the
        # whole expedition. Used only as a tie-breaker (see _sort_key).
        self._hub_assignment_counts: dict[str, int] = defaultdict(int)

    def decide(self, state: dict) -> list[dict]:
        gates = {g["id"]: g for g in state["gates"] if g["operational"]}
        remaining = {gid: (g["availablePower"], g["availableContainment"]) for gid, g in gates.items()}

        boarding = [v for v in state["voyages"] if v["status"] == "boarding"]
        boarding.sort(key=lambda v: self._sort_key(v, state["tick"]))

        assignments = []
        for voyage in boarding:
            gate_id = self._best_fit_gate(voyage, remaining)
            if gate_id is None:
                continue
            assignments.append({"gateId": gate_id, "voyageId": voyage["id"]})
            power, containment = remaining[gate_id]
            remaining[gate_id] = (power - voyage["requiredPower"], containment - voyage["requiredContainment"])
            self._hub_assignment_counts[voyage["originHub"]] += 1

        return assignments

    def _sort_key(self, voyage: dict, current_tick: int) -> tuple:
        # Slack (a.k.a. laxity), not raw ticks-until-deadline: how much room this
        # voyage has *beyond the time it still needs*, not just how soon its
        # deadline falls. Pure earliest-deadline-first looked right but actually
        # hurt arrivalSuccess in testing: a long voyage with a distant-but-real
        # deadline would get sorted behind a short voyage due days out, then sit
        # on a gate for a long time while genuinely time-pressured (low-slack)
        # voyages piled up behind it. Sorting by slack instead accounts for how
        # long the voyage itself takes, which is what earliest-deadline-first
        # was missing.
        slack = (voyage["arrivalDeadline"] - current_tick) - voyage["remainingDuration"]
        return (
            slack,                                                  # least slack first
            -voyage["priority"],                                    # higher priority first on a tie
            self._hub_assignment_counts[voyage["originHub"]],       # least-served hub first on a tie
        )

    @staticmethod
    def _best_fit_gate(voyage: dict, remaining: dict[int, tuple[int, int]]) -> int | None:
        best_gate_id = None
        best_waste = None
        for gate_id, (power, containment) in remaining.items():
            if power < voyage["requiredPower"] or containment < voyage["requiredContainment"]:
                continue
            waste = (power - voyage["requiredPower"]) + (containment - voyage["requiredContainment"])
            if best_waste is None or waste < best_waste:
                best_waste = waste
                best_gate_id = gate_id
        return best_gate_id


def run() -> None:
    token = oracle.login_or_register(EMAIL, NUID)
    expedition = oracle.create_expedition(token)
    expedition_id = expedition["expeditionId"]
    print(f"expedition {expedition_id}: {expedition['totalCycles']} cycles")

    scheduler = Scheduler()
    state = oracle.get_expedition(token, expedition_id)
    ticks = 0
    while not state.get("finished"):
        assignments = scheduler.decide(state)
        state = oracle.submit_cycle(token, expedition_id, assignments)
        ticks += 1
        if ticks % 200 == 0:
            print(f"  ...cycle {state.get('cycle')}/{state.get('totalCycles')}, "
                  f"tick {state.get('tick')}, profile={state.get('profile')}")

    print(f"finished after {ticks} ticks")
    print(f"overallScore: {state['overallScore']}")
    print(f"metrics: {state['metrics']}")


if __name__ == "__main__":
    run()
