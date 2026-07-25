// Package generator produces randomized-but-comparable workloads.
//
// Rather than generating pure noise, every workload is sampled from one of a
// fixed set of profiles (see Profiles below). Each profile fixes the *shape*
// of the difficulty (long dependency chains, sudden bursts, tight deadlines,
// ...) while randomizing the specifics within that shape. This keeps
// evaluations comparable across applicants: everyone who draws
// "deadline_critical" faces the same kind of pressure, just with different
// numbers.
package generator

import (
	"fmt"
	"math/rand"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// Profile names, exported so the evaluation engine can sample from them.
const (
	ProfileDependencyChains    = "dependency_chains"
	ProfileBurstTraffic        = "burst_traffic"
	ProfileHeavyCompute        = "heavy_compute"
	ProfileDeadlineCritical    = "deadline_critical"
	ProfileResourceConstrained = "resource_constrained"
	ProfileBalanced            = "balanced"
)

// AllProfiles lists every profile the evaluation engine samples from.
var AllProfiles = []string{
	ProfileDependencyChains,
	ProfileBurstTraffic,
	ProfileHeavyCompute,
	ProfileDeadlineCritical,
	ProfileResourceConstrained,
	ProfileBalanced,
}

var projectNames = []string{
	"ai-inference", "report-gen", "image-processing", "email-delivery",
	"analytics-pipeline", "db-sync",
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
	Workers        intRange
	WorkerCPU      intRange
	WorkerMemory   intRange
	Jobs           intRange
	JobCPU         intRange
	JobMemory      intRange
	Runtime        intRange // ticks
	ChainDepth     intRange // dependency chain length, 0/1 = no dependencies
	ChainBranching intRange // extra parallel deps fanning into a chain node
	DeadlineSlack  float64  // deadline = arrival + runtime*(chain depth) * slack; lower = tighter
	ArrivalBurst   float64  // fraction of jobs that arrive at tick 0 vs trickling in
	ArrivalWindow  int      // ticks over which the remaining jobs trickle in
	FailureRate    float64  // probability per worker per tick of going unavailable
	FailureTicks   intRange // how long an outage lasts
	MaxTicks       int
}

var configs = map[string]config{
	ProfileDependencyChains: {
		Workers: intRange{2, 4}, WorkerCPU: intRange{4, 8}, WorkerMemory: intRange{8, 16},
		Jobs: intRange{30, 50}, JobCPU: intRange{1, 3}, JobMemory: intRange{1, 4},
		Runtime: intRange{2, 6}, ChainDepth: intRange{5, 10}, ChainBranching: intRange{0, 1},
		DeadlineSlack: 2.5, ArrivalBurst: 1.0, ArrivalWindow: 0,
		FailureRate: 0.01, FailureTicks: intRange{2, 5}, MaxTicks: 400,
	},
	ProfileBurstTraffic: {
		Workers: intRange{6, 10}, WorkerCPU: intRange{4, 8}, WorkerMemory: intRange{8, 16},
		Jobs: intRange{400, 700}, JobCPU: intRange{1, 2}, JobMemory: intRange{1, 2},
		Runtime: intRange{1, 3}, ChainDepth: intRange{0, 1}, ChainBranching: intRange{0, 0},
		DeadlineSlack: 3.0, ArrivalBurst: 0.15, ArrivalWindow: 15,
		FailureRate: 0.005, FailureTicks: intRange{1, 3}, MaxTicks: 300,
	},
	ProfileHeavyCompute: {
		Workers: intRange{3, 5}, WorkerCPU: intRange{8, 16}, WorkerMemory: intRange{16, 32},
		Jobs: intRange{8, 16}, JobCPU: intRange{4, 12}, JobMemory: intRange{4, 16},
		Runtime: intRange{15, 40}, ChainDepth: intRange{1, 3}, ChainBranching: intRange{0, 1},
		DeadlineSlack: 2.0, ArrivalBurst: 0.7, ArrivalWindow: 20,
		FailureRate: 0.02, FailureTicks: intRange{3, 8}, MaxTicks: 500,
	},
	ProfileDeadlineCritical: {
		Workers: intRange{5, 8}, WorkerCPU: intRange{4, 8}, WorkerMemory: intRange{8, 16},
		Jobs: intRange{60, 100}, JobCPU: intRange{1, 4}, JobMemory: intRange{1, 4},
		Runtime: intRange{2, 8}, ChainDepth: intRange{1, 4}, ChainBranching: intRange{0, 2},
		DeadlineSlack: 1.15, ArrivalBurst: 0.5, ArrivalWindow: 30,
		FailureRate: 0.01, FailureTicks: intRange{2, 4}, MaxTicks: 300,
	},
	ProfileResourceConstrained: {
		Workers: intRange{2, 3}, WorkerCPU: intRange{2, 4}, WorkerMemory: intRange{4, 8},
		Jobs: intRange{80, 130}, JobCPU: intRange{1, 4}, JobMemory: intRange{1, 4},
		Runtime: intRange{2, 8}, ChainDepth: intRange{1, 3}, ChainBranching: intRange{0, 2},
		DeadlineSlack: 1.8, ArrivalBurst: 0.6, ArrivalWindow: 25,
		FailureRate: 0.02, FailureTicks: intRange{3, 6}, MaxTicks: 450,
	},
	ProfileBalanced: {
		Workers: intRange{4, 7}, WorkerCPU: intRange{4, 10}, WorkerMemory: intRange{8, 20},
		Jobs: intRange{60, 120}, JobCPU: intRange{1, 6}, JobMemory: intRange{1, 8},
		Runtime: intRange{2, 15}, ChainDepth: intRange{0, 5}, ChainBranching: intRange{0, 2},
		DeadlineSlack: 2.0, ArrivalBurst: 0.5, ArrivalWindow: 30,
		FailureRate: 0.012, FailureTicks: intRange{2, 6}, MaxTicks: 400,
	},
}

// Generate builds a fresh, deterministic-for-seed Simulation for the given profile.
func Generate(profile string, seed int64) (*models.Simulation, error) {
	cfg, ok := configs[profile]
	if !ok {
		return nil, fmt.Errorf("unknown workload profile %q", profile)
	}
	rng := rand.New(rand.NewSource(seed))

	sim := &models.Simulation{
		Profile:  profile,
		Seed:     seed,
		Tick:     0,
		MaxTicks: cfg.MaxTicks,
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{},
			ProjectWaitTicks:   map[string]int64{},
		},
		FailureRate:     cfg.FailureRate,
		FailureTicksMin: cfg.FailureTicks.Min,
		FailureTicksMax: cfg.FailureTicks.Max,
	}

	workerCount := cfg.Workers.sample(rng)
	for i := 1; i <= workerCount; i++ {
		cpu := cfg.WorkerCPU.sample(rng)
		mem := cfg.WorkerMemory.sample(rng)
		sim.Workers = append(sim.Workers, &models.Worker{
			ID: i, TotalCPU: cpu, TotalMemory: mem,
			AvailableCPU: cpu, AvailableMemory: mem, Available: true,
		})
	}

	jobCount := cfg.Jobs.sample(rng)
	sim.Jobs = generateJobs(rng, cfg, jobCount)
	sim.Stats.TotalJobs = len(sim.Jobs)

	return sim, nil
}

// generateJobs builds jobs in dependency-respecting layers: each job may only
// depend on jobs generated in an earlier layer, which guarantees the
// dependency graph is acyclic.
func generateJobs(rng *rand.Rand, cfg config, jobCount int) []*models.Job {
	jobs := make([]*models.Job, 0, jobCount)
	nextID := 1

	remaining := jobCount
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
				j := &models.Job{
					ID:               nextID,
					Project:          projectNames[rng.Intn(len(projectNames))],
					Priority:         1 + rng.Intn(5),
					EstimatedRuntime: cfg.Runtime.sample(rng),
					RequiredCPU:      cfg.JobCPU.sample(rng),
					RequiredMemory:   cfg.JobMemory.sample(rng),
					Status:           models.JobBlocked,
				}
				j.RemainingRuntime = j.EstimatedRuntime
				if layer == 0 {
					j.Dependencies = nil
				} else {
					j.Dependencies = append([]int{}, prevLayer...)
				}
				j.ArrivalTick = arrivalTick(rng, cfg, nextID, jobCount)
				j.Deadline = j.ArrivalTick + int(float64(j.EstimatedRuntime*(layer+1))*cfg.DeadlineSlack) + 3
				nextID++
				thisLayer = append(thisLayer, j.ID)
				jobs = append(jobs, j)
			}
			prevLayer = thisLayer
			remaining -= layerSize
			if remaining <= 0 {
				break
			}
		}
	}

	// Jobs with no dependencies start out ready as soon as they arrive.
	for _, j := range jobs {
		if len(j.Dependencies) == 0 {
			j.Status = models.JobReady
			readyAt := j.ArrivalTick
			j.ReadyTick = &readyAt
		}
	}
	return jobs
}

// arrivalTick decides when a job becomes visible: a fraction of jobs land at
// tick 0 (the initial backlog), the rest trickle in across ArrivalWindow to
// simulate new work showing up mid-simulation.
func arrivalTick(rng *rand.Rand, cfg config, jobIndex, totalJobs int) int {
	if rng.Float64() < cfg.ArrivalBurst || cfg.ArrivalWindow <= 0 {
		return 0
	}
	return rng.Intn(cfg.ArrivalWindow + 1)
}
