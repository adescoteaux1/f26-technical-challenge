// Package engine is the simulation's source of truth: it owns dependency
// resolution, resource accounting, the simulation clock, and worker
// failure/recovery. The validator (validator.go) gates what the engine ever
// sees; AdvanceTick assumes assignments already applied are legal.
package engine

import (
	"math/rand"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// AdvanceTick moves the simulation forward by one tick:
//  1. running jobs make progress (unless their worker is down) and may complete
//  2. completed jobs free their worker's resources and unlock dependents
//  3. workers may fail or recover
//  4. bookkeeping stats are updated for the scoring engine
func AdvanceTick(sim *models.Simulation) {
	sim.Tick++

	jobsByID := make(map[int]*models.Job, len(sim.Jobs))
	for _, j := range sim.Jobs {
		jobsByID[j.ID] = j
	}
	workersByID := make(map[int]*models.Worker, len(sim.Workers))
	for _, w := range sim.Workers {
		workersByID[w.ID] = w
	}

	progressRunningJobs(sim, jobsByID, workersByID)
	unlockDependents(sim, jobsByID)
	simulateWorkerAvailability(sim, workersByID, jobsByID)
	accumulateUtilization(sim)

	if allJobsTerminal(sim) || sim.Tick >= sim.MaxTicks {
		sim.Finished = true
	}
}

// progressRunningJobs advances remaining runtime for every job whose worker
// is currently up, and completes jobs that reach zero.
func progressRunningJobs(sim *models.Simulation, jobsByID map[int]*models.Job, workersByID map[int]*models.Worker) {
	for _, job := range sim.Jobs {
		if job.Status != models.JobRunning {
			continue
		}
		worker := workersByID[*job.AssignedWorker]
		if worker == nil || !worker.Available {
			continue // paused while its worker is down
		}

		job.RemainingRuntime--
		if job.RemainingRuntime > 0 {
			continue
		}

		completeJob(sim, job, worker)
	}
}

func completeJob(sim *models.Simulation, job *models.Job, worker *models.Worker) {
	job.Status = models.JobCompleted
	tick := sim.Tick
	job.CompletionTick = &tick

	worker.AvailableCPU += job.RequiredCPU
	worker.AvailableMemory += job.RequiredMemory
	worker.RunningJobs = removeInt(worker.RunningJobs, job.ID)

	sim.Stats.CompletedJobs++
	sim.Stats.ProjectCompletions[job.Project]++
	if job.ReadyTick != nil && job.StartTick != nil {
		wait := int64(*job.StartTick - *job.ReadyTick)
		sim.Stats.TotalWaitTicks += wait
		sim.Stats.ProjectWaitTicks[job.Project] += wait
	}
	if tick <= job.Deadline {
		sim.Stats.DeadlinesMet++
	} else {
		sim.Stats.DeadlinesMissed++
	}
}

// unlockDependents marks blocked jobs as ready once every dependency has
// completed. Jobs whose dependencies aren't done yet stay blocked regardless
// of arrival tick.
func unlockDependents(sim *models.Simulation, jobsByID map[int]*models.Job) {
	for _, job := range sim.Jobs {
		if job.Status != models.JobBlocked {
			continue
		}
		ready := true
		for _, depID := range job.Dependencies {
			dep := jobsByID[depID]
			if dep == nil || dep.Status != models.JobCompleted {
				ready = false
				break
			}
		}
		if ready {
			job.Status = models.JobReady
			tick := sim.Tick
			job.ReadyTick = &tick
		}
	}
}

// simulateWorkerAvailability randomly fails healthy workers and recovers
// workers whose outage has expired. A crashed worker loses in-flight jobs
// back to the ready queue (their partial progress is preserved) so the
// scheduler must adapt rather than assume workers are permanent.
func simulateWorkerAvailability(sim *models.Simulation, workersByID map[int]*models.Worker, jobsByID map[int]*models.Job) {
	rng := rand.New(rand.NewSource(sim.Seed + int64(sim.Tick)*1000003))

	for _, worker := range sim.Workers {
		if !worker.Available {
			if sim.Tick >= worker.UnavailableUntil {
				worker.Available = true
			}
			continue
		}
		if sim.FailureRate <= 0 || rng.Float64() >= sim.FailureRate {
			continue
		}

		outage := sim.FailureTicksMin
		if sim.FailureTicksMax > sim.FailureTicksMin {
			outage += rng.Intn(sim.FailureTicksMax - sim.FailureTicksMin + 1)
		}
		worker.Available = false
		worker.UnavailableUntil = sim.Tick + outage

		for _, jobID := range worker.RunningJobs {
			job := jobsByID[jobID]
			if job == nil {
				continue
			}
			job.Status = models.JobReady
			job.AssignedWorker = nil
			job.StartTick = nil
			readyTick := sim.Tick
			job.ReadyTick = &readyTick
		}
		worker.RunningJobs = nil
		worker.AvailableCPU = worker.TotalCPU
		worker.AvailableMemory = worker.TotalMemory
	}
}

func accumulateUtilization(sim *models.Simulation) {
	for _, w := range sim.Workers {
		if !w.Available {
			continue
		}
		sim.Stats.WorkerTotalResourceTicks += int64(w.TotalCPU)
		sim.Stats.WorkerBusyResourceTicks += int64(w.TotalCPU - w.AvailableCPU)
	}
}

func allJobsTerminal(sim *models.Simulation) bool {
	for _, j := range sim.Jobs {
		if j.Status != models.JobCompleted {
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
