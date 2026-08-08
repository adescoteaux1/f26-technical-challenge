"""The "dumbest scheduler that still works" — a reference floor, not a target.

Strategy: each tick, walk voyages in whatever order the Control Tower returns them,
and assign every "boarding" voyage to the first operational gate (in ID
order) with enough spare power/containment. No prioritization, no fairness,
no deadline awareness. This is intentionally the naive baseline described in
CHALLENGE.md so reviewers have a concrete "here's what near-zero effort
looks like" data point when calibrating scores.

Multi-hop corridor voyages and premium/SLA hubs (see CHALLENGE.md) need
*zero* changes here: a corridor voyage's `requiredPower`/`requiredContainment`
always describe its current leg, and it just reappears as "boarding" for its
next leg like any other voyage. This scheduler handles that correctly by
accident, not by design — it doesn't know premium hubs exist at all, which
is exactly why it won't score well on slaCompliance.
"""

from __future__ import annotations

import control_tower_client as tower

EMAIL = "simple-scheduler@internal.local"
NUID = "000000001"


def decide_assignments(state: dict) -> list[dict]:
    # Track remaining capacity locally so we don't over-commit a gate across
    # multiple assignments within the same tick's batch.
    remaining = {
        g["id"]: (g["availablePower"], g["availableContainment"])
        for g in state["gates"]
        if g["operational"]
    }

    assignments = []
    for voyage in state["voyages"]:
        if voyage["status"] != "boarding":
            continue
        for gate_id, (power, containment) in remaining.items():
            if power >= voyage["requiredPower"] and containment >= voyage["requiredContainment"]:
                assignments.append({"gateId": gate_id, "voyageId": voyage["id"]})
                remaining[gate_id] = (power - voyage["requiredPower"], containment - voyage["requiredContainment"])
                break
    return assignments


def run() -> None:
    token = tower.login_or_register(EMAIL, NUID)
    expedition = tower.create_expedition(token)
    expedition_id = expedition["expeditionId"]
    print(f"expedition {expedition_id}: {expedition['totalCycles']} cycles")

    state = tower.get_expedition(token, expedition_id)
    ticks = 0
    while not state.get("finished"):
        assignments = decide_assignments(state)
        state = tower.submit_cycle(token, expedition_id, assignments)
        ticks += 1
        if ticks % 200 == 0:
            print(f"  ...cycle {state.get('cycle')}/{state.get('totalCycles')}, "
                  f"tick {state.get('tick')}, profile={state.get('profile')}")

    print(f"finished after {ticks} ticks")
    print(f"overallScore: {state['overallScore']}")
    print(f"metrics: {state['metrics']}")


if __name__ == "__main__":
    run()
