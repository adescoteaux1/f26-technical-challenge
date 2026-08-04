// Package engine is the simulation's source of truth: it owns prerequisite
// resolution, resource accounting, the simulation clock, and gate
// outage/recovery. The validator (validator.go) gates what the engine ever
// sees; AdvanceTick assumes assignments already applied are legal.
package engine

import (
	"math/rand"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// AdvanceTick moves the cycle forward by one tick:
//  1. in-transit voyages make progress (unless their gate is down) and may arrive
//  2. arrived voyages free their gate's resources and unlock dependent voyages
//  3. gates may go offline or come back online
//  4. bookkeeping stats are updated for the scoring engine
func AdvanceTick(cycle *models.Cycle) {
	cycle.Tick++

	voyagesByID := make(map[int]*models.Voyage, len(cycle.Voyages))
	for _, v := range cycle.Voyages {
		voyagesByID[v.ID] = v
	}
	gatesByID := make(map[int]*models.Gate, len(cycle.Gates))
	for _, g := range cycle.Gates {
		gatesByID[g.ID] = g
	}

	progressActiveVoyages(cycle, voyagesByID, gatesByID)
	unlockPrerequisiteVoyages(cycle, voyagesByID)
	simulateGateAvailability(cycle, gatesByID, voyagesByID)
	accumulateUtilization(cycle)

	if allVoyagesArrived(cycle) || cycle.Tick >= cycle.MaxTicks {
		cycle.Finished = true
	}
}

// progressActiveVoyages advances remaining duration for every voyage whose
// gate is currently up, and completes voyages that reach zero.
func progressActiveVoyages(cycle *models.Cycle, voyagesByID map[int]*models.Voyage, gatesByID map[int]*models.Gate) {
	for _, voyage := range cycle.Voyages {
		if voyage.Status != models.VoyageInTransit {
			continue
		}
		gate := gatesByID[*voyage.AssignedGate]
		if gate == nil || !gate.Operational {
			continue // paused while its gate is down
		}

		voyage.RemainingDuration--
		if voyage.RemainingDuration > 0 {
			continue
		}

		completeVoyage(cycle, voyage, gate)
	}
}

// completeVoyage runs when a voyage's current leg finishes. For a
// single-hop voyage (or the last leg of a corridor), that means the whole
// trip has arrived: free the gate, record scoring stats, and check the
// deadline. For an earlier leg of a multi-hop corridor, the voyage instead
// goes back to boarding for its next leg — it doesn't count as arrived
// (and isn't scored) until the final leg completes, so a 3-leg corridor
// still only counts once toward throughput, not three times.
func completeVoyage(cycle *models.Cycle, voyage *models.Voyage, gate *models.Gate) {
	gate.AvailablePower += voyage.RequiredPower
	gate.AvailableContainment += voyage.RequiredContainment
	gate.ActiveVoyages = removeInt(gate.ActiveVoyages, voyage.ID)

	tick := cycle.Tick
	departureTick := voyage.DepartureTick // captured before clearing; used below for the final leg's wait time
	voyage.AssignedGate = nil
	voyage.DepartureTick = nil

	if voyage.LegIndex+1 < len(voyage.Legs) {
		voyage.LegIndex++
		next := voyage.Legs[voyage.LegIndex]
		voyage.RequiredPower = next.RequiredPower
		voyage.RequiredContainment = next.RequiredContainment
		voyage.EstimatedDuration = next.EstimatedDuration
		voyage.RemainingDuration = next.EstimatedDuration
		voyage.Status = models.VoyageBoarding
		voyage.BoardingTick = &tick
		return
	}

	voyage.Status = models.VoyageArrived
	voyage.ArrivalTick = &tick

	cycle.Stats.ArrivedVoyages++
	cycle.Stats.OriginHubArrivals[voyage.OriginHub]++
	if voyage.BoardingTick != nil && departureTick != nil {
		wait := int64(*departureTick - *voyage.BoardingTick)
		cycle.Stats.TotalWaitTicks += wait
		cycle.Stats.OriginHubWaitTicks[voyage.OriginHub] += wait
	}
	if tick <= voyage.ArrivalDeadline {
		cycle.Stats.ArrivalsOnTime++
	} else {
		cycle.Stats.ArrivalsLate++
	}
	if voyage.SLADeadline != nil {
		if tick <= *voyage.SLADeadline {
			cycle.Stats.PremiumArrivalsOnTime++
		} else {
			cycle.Stats.PremiumArrivalsLate++
		}
	}
}

// unlockPrerequisiteVoyages marks awaiting-transfer voyages as boarding once
// every prerequisite has arrived. Voyages whose prerequisites aren't done yet
// stay awaiting transfer regardless of requested tick.
func unlockPrerequisiteVoyages(cycle *models.Cycle, voyagesByID map[int]*models.Voyage) {
	for _, voyage := range cycle.Voyages {
		if voyage.Status != models.VoyageAwaitingTransfer {
			continue
		}
		ready := true
		for _, depID := range voyage.Prerequisites {
			dep := voyagesByID[depID]
			if dep == nil || dep.Status != models.VoyageArrived {
				ready = false
				break
			}
		}
		if ready {
			voyage.Status = models.VoyageBoarding
			tick := cycle.Tick
			voyage.BoardingTick = &tick
		}
	}
}

// simulateGateAvailability randomly takes healthy gates offline and recovers
// gates whose outage has expired. A crashed gate loses in-flight voyages
// back to the boarding queue (their partial progress is preserved) so the
// scheduler must adapt rather than assume gates are permanent.
func simulateGateAvailability(cycle *models.Cycle, gatesByID map[int]*models.Gate, voyagesByID map[int]*models.Voyage) {
	rng := rand.New(rand.NewSource(cycle.Seed + int64(cycle.Tick)*1000003))

	for _, gate := range cycle.Gates {
		if !gate.Operational {
			if cycle.Tick >= gate.OfflineUntil {
				gate.Operational = true
			}
			continue
		}
		if cycle.OutageRate <= 0 || rng.Float64() >= cycle.OutageRate {
			continue
		}

		outage := cycle.OutageTicksMin
		if cycle.OutageTicksMax > cycle.OutageTicksMin {
			outage += rng.Intn(cycle.OutageTicksMax - cycle.OutageTicksMin + 1)
		}
		gate.Operational = false
		gate.OfflineUntil = cycle.Tick + outage

		for _, voyageID := range gate.ActiveVoyages {
			voyage := voyagesByID[voyageID]
			if voyage == nil {
				continue
			}
			voyage.Status = models.VoyageBoarding
			voyage.AssignedGate = nil
			voyage.DepartureTick = nil
			boardingTick := cycle.Tick
			voyage.BoardingTick = &boardingTick
		}
		gate.ActiveVoyages = nil
		gate.AvailablePower = gate.TotalPower
		gate.AvailableContainment = gate.TotalContainment
	}
}

func accumulateUtilization(cycle *models.Cycle) {
	for _, g := range cycle.Gates {
		if !g.Operational {
			continue
		}
		cycle.Stats.GateTotalResourceTicks += int64(g.TotalPower)
		cycle.Stats.GateBusyResourceTicks += int64(g.TotalPower - g.AvailablePower)
	}
}

func allVoyagesArrived(cycle *models.Cycle) bool {
	for _, v := range cycle.Voyages {
		if v.Status != models.VoyageArrived {
			return false
		}
	}
	return true
}

func removeInt(slice []int, v int) []int {
	out := slice[:0]
	for _, x := range slice {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
