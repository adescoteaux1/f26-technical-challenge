// Package generator produces randomized-but-comparable workloads.
//
// Rather than generating pure noise, every workload is sampled from one of a
// fixed set of profiles (see Profiles below). Each profile fixes the *shape*
// of the difficulty (long transfer chains, sudden surges, narrow arrival
// windows, ...) while randomizing the specifics within that shape. This keeps
// expeditions comparable across applicants: everyone who draws
// "narrow_window" faces the same kind of pressure, just with different
// numbers.
package generator

import (
	"fmt"
	"math/rand"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// Profile names, exported so the expedition engine can sample from them.
const (
	ProfileTransferChains = "transfer_chains"
	ProfileSurgeArrivals  = "surge_arrivals"
	ProfileDeepRift       = "deep_rift"
	ProfileNarrowWindow   = "narrow_window"
	ProfileGateCongestion = "gate_congestion"
	ProfileMixedTraffic   = "mixed_traffic"
)

// AllProfiles lists every profile the expedition engine samples from.
var AllProfiles = []string{
	ProfileTransferChains,
	ProfileSurgeArrivals,
	ProfileDeepRift,
	ProfileNarrowWindow,
	ProfileGateCongestion,
	ProfileMixedTraffic,
}

var originHubNames = []string{
	"central-hub-alpha", "northern-gateway", "quantum-nexus", "eastern-node",
	"southern-passage", "western-bridge",
}

// intRange is an inclusive [Min, Max] range sampled uniformly.
type intRange struct{ Min, Max int }

func (r intRange) sample(rng *rand.Rand) int {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + rng.Intn(r.Max-r.Min+1)
}

// config parameterizes one workload profile. Every profile shares the same
// generation algorithm; only these knobs differ.
type config struct {
	Gates             intRange
	GatePower         intRange
	GateContainment   intRange
	Voyages           intRange
	VoyagePower       intRange
	VoyageContainment intRange
	Duration          intRange // ticks
	ChainDepth        intRange // transfer chain length, 0/1 = no prerequisites
	ChainBranching    intRange // extra parallel prerequisites fanning into a chain node
	DeadlineSlack     float64  // deadline = requested + duration*(chain depth) * slack; lower = tighter
	ArrivalBurst      float64  // fraction of voyages requested at tick 0 vs trickling in
	ArrivalWindow     int      // ticks over which the remaining voyages trickle in
	OutageRate        float64  // probability per gate per tick of going offline
	OutageTicks       intRange // how long an outage lasts
	MaxTicks          int
}

var configs = map[string]config{
	ProfileTransferChains: {
		Gates: intRange{2, 4}, GatePower: intRange{4, 8}, GateContainment: intRange{8, 16},
		Voyages: intRange{30, 50}, VoyagePower: intRange{1, 3}, VoyageContainment: intRange{1, 4},
		Duration: intRange{2, 6}, ChainDepth: intRange{5, 10}, ChainBranching: intRange{0, 1},
		DeadlineSlack: 2.5, ArrivalBurst: 1.0, ArrivalWindow: 0,
		OutageRate: 0.01, OutageTicks: intRange{2, 5}, MaxTicks: 400,
	},
	ProfileSurgeArrivals: {
		Gates: intRange{6, 10}, GatePower: intRange{4, 8}, GateContainment: intRange{8, 16},
		Voyages: intRange{400, 700}, VoyagePower: intRange{1, 2}, VoyageContainment: intRange{1, 2},
		Duration: intRange{1, 3}, ChainDepth: intRange{0, 1}, ChainBranching: intRange{0, 0},
		DeadlineSlack: 3.0, ArrivalBurst: 0.15, ArrivalWindow: 15,
		OutageRate: 0.005, OutageTicks: intRange{1, 3}, MaxTicks: 300,
	},
	ProfileDeepRift: {
		Gates: intRange{3, 5}, GatePower: intRange{8, 16}, GateContainment: intRange{16, 32},
		Voyages: intRange{8, 16}, VoyagePower: intRange{4, 12}, VoyageContainment: intRange{4, 16},
		Duration: intRange{15, 40}, ChainDepth: intRange{1, 3}, ChainBranching: intRange{0, 1},
		DeadlineSlack: 2.0, ArrivalBurst: 0.7, ArrivalWindow: 20,
		OutageRate: 0.02, OutageTicks: intRange{3, 8}, MaxTicks: 500,
	},
	ProfileNarrowWindow: {
		Gates: intRange{5, 8}, GatePower: intRange{4, 8}, GateContainment: intRange{8, 16},
		Voyages: intRange{60, 100}, VoyagePower: intRange{1, 4}, VoyageContainment: intRange{1, 4},
		Duration: intRange{2, 8}, ChainDepth: intRange{1, 4}, ChainBranching: intRange{0, 2},
		DeadlineSlack: 1.15, ArrivalBurst: 0.5, ArrivalWindow: 30,
		OutageRate: 0.01, OutageTicks: intRange{2, 4}, MaxTicks: 300,
	},
	ProfileGateCongestion: {
		Gates: intRange{2, 3}, GatePower: intRange{2, 4}, GateContainment: intRange{4, 8},
		Voyages: intRange{80, 130}, VoyagePower: intRange{1, 4}, VoyageContainment: intRange{1, 4},
		Duration: intRange{2, 8}, ChainDepth: intRange{1, 3}, ChainBranching: intRange{0, 2},
		DeadlineSlack: 1.8, ArrivalBurst: 0.6, ArrivalWindow: 25,
		OutageRate: 0.02, OutageTicks: intRange{3, 6}, MaxTicks: 450,
	},
	ProfileMixedTraffic: {
		Gates: intRange{4, 7}, GatePower: intRange{4, 10}, GateContainment: intRange{8, 20},
		Voyages: intRange{60, 120}, VoyagePower: intRange{1, 6}, VoyageContainment: intRange{1, 8},
		Duration: intRange{2, 15}, ChainDepth: intRange{0, 5}, ChainBranching: intRange{0, 2},
		DeadlineSlack: 2.0, ArrivalBurst: 0.5, ArrivalWindow: 30,
		OutageRate: 0.012, OutageTicks: intRange{2, 6}, MaxTicks: 400,
	},
}

// Generate builds a fresh, deterministic-for-seed Cycle for the given profile.
func Generate(profile string, seed int64) (*models.Cycle, error) {
	cfg, ok := configs[profile]
	if !ok {
		return nil, fmt.Errorf("unknown workload profile %q", profile)
	}
	rng := rand.New(rand.NewSource(seed))

	cycle := &models.Cycle{
		Profile:  profile,
		Seed:     seed,
		Tick:     0,
		MaxTicks: cfg.MaxTicks,
		Stats: models.SimStats{
			OriginHubArrivals:  map[string]int{},
			OriginHubWaitTicks: map[string]int64{},
		},
		OutageRate:     cfg.OutageRate,
		OutageTicksMin: cfg.OutageTicks.Min,
		OutageTicksMax: cfg.OutageTicks.Max,
	}

	gateCount := cfg.Gates.sample(rng)
	for i := 1; i <= gateCount; i++ {
		power := cfg.GatePower.sample(rng)
		containment := cfg.GateContainment.sample(rng)
		cycle.Gates = append(cycle.Gates, &models.Gate{
			ID: i, TotalPower: power, TotalContainment: containment,
			AvailablePower: power, AvailableContainment: containment, Operational: true,
		})
	}

	voyageCount := cfg.Voyages.sample(rng)
	cycle.Voyages = generateVoyages(rng, cfg, voyageCount)
	cycle.Stats.TotalVoyages = len(cycle.Voyages)

	return cycle, nil
}

// generateVoyages builds voyages in prerequisite-respecting layers: each
// voyage may only depend on voyages generated in an earlier layer, which
// guarantees the prerequisite graph is acyclic.
func generateVoyages(rng *rand.Rand, cfg config, voyageCount int) []*models.Voyage {
	voyages := make([]*models.Voyage, 0, voyageCount)
	nextID := 1

	remaining := voyageCount
	for remaining > 0 {
		depth := cfg.ChainDepth.sample(rng)
		if depth < 1 {
			depth = 1
		}
		if depth > remaining {
			depth = remaining
		}

		var prevLayer []int
		for layer := 0; layer < depth; layer++ {
			branching := cfg.ChainBranching.sample(rng)
			layerSize := 1 + rng.Intn(branching+1)
			if layerSize > remaining {
				layerSize = remaining
			}
			var thisLayer []int
			for i := 0; i < layerSize; i++ {
				v := &models.Voyage{
					ID:                  nextID,
					OriginHub:           originHubNames[rng.Intn(len(originHubNames))],
					Priority:            1 + rng.Intn(5),
					EstimatedDuration:   cfg.Duration.sample(rng),
					RequiredPower:       cfg.VoyagePower.sample(rng),
					RequiredContainment: cfg.VoyageContainment.sample(rng),
					Status:              models.VoyageAwaitingTransfer,
				}
				v.RemainingDuration = v.EstimatedDuration
				if layer == 0 {
					v.Prerequisites = nil
				} else {
					v.Prerequisites = append([]int{}, prevLayer...)
				}
				v.RequestedTick = requestedTick(rng, cfg, nextID, voyageCount)
				v.ArrivalDeadline = v.RequestedTick + int(float64(v.EstimatedDuration*(layer+1))*cfg.DeadlineSlack) + 3
				nextID++
				thisLayer = append(thisLayer, v.ID)
				voyages = append(voyages, v)
			}
			prevLayer = thisLayer
			remaining -= layerSize
			if remaining <= 0 {
				break
			}
		}
	}

	// Voyages with no prerequisites start out boarding as soon as they're requested.
	for _, v := range voyages {
		if len(v.Prerequisites) == 0 {
			v.Status = models.VoyageBoarding
			boardingAt := v.RequestedTick
			v.BoardingTick = &boardingAt
		}
	}
	return voyages
}

// requestedTick decides when a voyage becomes visible: a fraction of voyages
// land at tick 0 (the initial backlog), the rest trickle in across
// ArrivalWindow to simulate new travel requests showing up mid-cycle.
func requestedTick(rng *rand.Rand, cfg config, voyageIndex, totalVoyages int) int {
	if rng.Float64() < cfg.ArrivalBurst || cfg.ArrivalWindow <= 0 {
		return 0
	}
	return rng.Intn(cfg.ArrivalWindow + 1)
}
