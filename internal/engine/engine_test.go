package engine

import (
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

func runningJobSim(runtime, cpu, mem int) (*models.Simulation, *models.Job, *models.Worker) {
	worker := &models.Worker{ID: 1, TotalCPU: 4, TotalMemory: 8, AvailableCPU: 4 - cpu, AvailableMemory: 8 - mem, Available: true, RunningJobs: []int{1}}
	job := &models.Job{
		ID: 1, Status: models.JobRunning, RequiredCPU: cpu, RequiredMemory: mem,
		EstimatedRuntime: runtime, RemainingRuntime: runtime,
	}
	workerID := 1
	job.AssignedWorker = &workerID
	sim := &models.Simulation{
		Workers:  []*models.Worker{worker},
		Jobs:     []*models.Job{job},
		MaxTicks: 1000,
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{},
			ProjectWaitTicks:   map[string]int64{},
		},
	}
	return sim, job, worker
}

func TestAdvanceTick_JobCompletesAfterRuntimeElapses(t *testing.T) {
	sim, job, _ := runningJobSim(2, 2, 2)

	AdvanceTick(sim)
	if job.Status != models.JobRunning {
		t.Fatalf("expected job still running after 1 of 2 ticks, got %s", job.Status)
	}
	if job.RemainingRuntime != 1 {
		t.Fatalf("expected remaining runtime 1, got %d", job.RemainingRuntime)
	}

	AdvanceTick(sim)
	if job.Status != models.JobCompleted {
		t.Fatalf("expected job completed after 2 ticks, got %s", job.Status)
	}
	if job.CompletionTick == nil || *job.CompletionTick != 2 {
		t.Fatalf("expected completion tick 2, got %v", job.CompletionTick)
	}
}

func TestAdvanceTick_CompletionFreesWorkerResources(t *testing.T) {
	sim, _, worker := runningJobSim(1, 3, 3)

	AdvanceTick(sim)

	if worker.AvailableCPU != worker.TotalCPU {
		t.Errorf("expected worker CPU fully freed, got %d/%d", worker.AvailableCPU, worker.TotalCPU)
	}
	if worker.AvailableMemory != worker.TotalMemory {
		t.Errorf("expected worker memory fully freed, got %d/%d", worker.AvailableMemory, worker.TotalMemory)
	}
	if len(worker.RunningJobs) != 0 {
		t.Errorf("expected worker.RunningJobs empty, got %v", worker.RunningJobs)
	}
}

func TestAdvanceTick_DependentJobUnlocksOnlyWhenAllDepsComplete(t *testing.T) {
	dep1 := &models.Job{ID: 1, Status: models.JobRunning, RemainingRuntime: 1}
	w1 := 10
	dep1.AssignedWorker = &w1
	dep2 := &models.Job{ID: 2, Status: models.JobBlocked, Dependencies: []int{1}}
	// dep2 itself never gets assigned; used purely to seed the dependent's dependency list
	dependent := &models.Job{ID: 3, Status: models.JobBlocked, Dependencies: []int{1, 2}}

	worker := &models.Worker{ID: 10, TotalCPU: 4, TotalMemory: 8, AvailableCPU: 4, AvailableMemory: 8, Available: true, RunningJobs: []int{1}}
	sim := &models.Simulation{
		Workers:  []*models.Worker{worker},
		Jobs:     []*models.Job{dep1, dep2, dependent},
		MaxTicks: 1000,
		Stats: models.SimStats{
			ProjectCompletions: map[string]int{},
			ProjectWaitTicks:   map[string]int64{},
		},
	}

	AdvanceTick(sim) // dep1 completes; dep2 still blocked (depends on dep1... wait dep2 has no assignment)

	if dependent.Status != models.JobBlocked {
		t.Fatalf("expected dependent still blocked while dep2 incomplete, got %s", dependent.Status)
	}
	if dep2.Status != models.JobReady {
		t.Fatalf("expected dep2 to unlock once dep1 completed, got %s", dep2.Status)
	}

	// Manually complete dep2 and re-run unlock logic via another tick.
	dep2.Status = models.JobCompleted
	tick := sim.Tick
	dep2.CompletionTick = &tick

	AdvanceTick(sim)

	if dependent.Status != models.JobReady {
		t.Fatalf("expected dependent ready once both deps completed, got %s", dependent.Status)
	}
}

func TestAdvanceTick_RunningJobPausesWhileWorkerDown(t *testing.T) {
	sim, job, worker := runningJobSim(3, 2, 2)
	worker.Available = false
	worker.UnavailableUntil = 1000 // stays down for the whole test

	AdvanceTick(sim)

	if job.RemainingRuntime != 3 {
		t.Fatalf("expected job progress paused while worker down, remaining=%d", job.RemainingRuntime)
	}
}

func TestAdvanceTick_WorkerFailureRequeuesRunningJob(t *testing.T) {
	sim, job, worker := runningJobSim(5, 2, 2)
	sim.FailureRate = 1.0 // force failure on the next eligible tick
	sim.FailureTicksMin = 3
	sim.FailureTicksMax = 3

	AdvanceTick(sim)

	if worker.Available {
		t.Fatalf("expected worker to have failed")
	}
	if job.Status != models.JobReady {
		t.Fatalf("expected running job requeued to ready after worker failure, got %s", job.Status)
	}
	if job.AssignedWorker != nil {
		t.Fatalf("expected job unassigned after worker failure")
	}
	if worker.AvailableCPU != worker.TotalCPU {
		t.Fatalf("expected worker resources reset on failure")
	}

	// Worker should recover once its outage expires.
	AdvanceTick(sim)
	AdvanceTick(sim)
	if worker.Available {
		t.Fatalf("expected worker still down before outage expires")
	}
	AdvanceTick(sim)
	if !worker.Available {
		t.Fatalf("expected worker to recover after outage duration elapses")
	}
}

func TestAdvanceTick_FinishesWhenAllJobsComplete(t *testing.T) {
	sim, _, _ := runningJobSim(1, 1, 1)

	AdvanceTick(sim)

	if !sim.Finished {
		t.Fatalf("expected simulation finished once all jobs completed")
	}
}

func TestAdvanceTick_FinishesAtMaxTicks(t *testing.T) {
	sim, _, _ := runningJobSim(1000, 1, 1)
	sim.MaxTicks = 1

	AdvanceTick(sim)

	if !sim.Finished {
		t.Fatalf("expected simulation finished at MaxTicks even with incomplete jobs")
	}
}
