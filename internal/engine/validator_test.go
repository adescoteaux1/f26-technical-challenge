package engine

import (
	"strings"
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

func newTestSim() *models.Simulation {
	return &models.Simulation{
		Tick: 0,
		Workers: []*models.Worker{
			{ID: 1, TotalCPU: 4, TotalMemory: 8, AvailableCPU: 4, AvailableMemory: 8, Available: true},
		},
		Jobs: []*models.Job{
			{ID: 1, Status: models.JobReady, RequiredCPU: 2, RequiredMemory: 2, ArrivalTick: 0},
		},
	}
}

func TestValidateAndApply_AcceptsValidAssignment(t *testing.T) {
	sim := newTestSim()

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})

	if len(result.Rejected) != 0 {
		t.Fatalf("expected no rejections, got %+v", result.Rejected)
	}
	if len(result.Accepted) != 1 {
		t.Fatalf("expected 1 accepted assignment, got %d", len(result.Accepted))
	}

	job := sim.Jobs[0]
	if job.Status != models.JobRunning {
		t.Errorf("expected job status running, got %s", job.Status)
	}
	if job.AssignedWorker == nil || *job.AssignedWorker != 1 {
		t.Errorf("expected job assigned to worker 1, got %v", job.AssignedWorker)
	}

	worker := sim.Workers[0]
	if worker.AvailableCPU != 2 {
		t.Errorf("expected worker availableCPU 2, got %d", worker.AvailableCPU)
	}
	if worker.AvailableMemory != 6 {
		t.Errorf("expected worker availableMemory 6, got %d", worker.AvailableMemory)
	}
	if len(worker.RunningJobs) != 1 || worker.RunningJobs[0] != 1 {
		t.Errorf("expected worker.RunningJobs = [1], got %v", worker.RunningJobs)
	}
}

func TestValidateAndApply_RejectsUnknownWorker(t *testing.T) {
	sim := newTestSim()
	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 99, JobID: 1}})
	requireSingleRejection(t, result, "does not exist")
}

func TestValidateAndApply_RejectsUnknownJob(t *testing.T) {
	sim := newTestSim()
	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 99}})
	requireSingleRejection(t, result, "does not exist")
}

func TestValidateAndApply_RejectsBlockedJob(t *testing.T) {
	sim := newTestSim()
	sim.Jobs[0].Status = models.JobBlocked
	sim.Jobs[0].Dependencies = []int{42}

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "blocked")
}

func TestValidateAndApply_RejectsAlreadyRunningJob(t *testing.T) {
	sim := newTestSim()
	running := 1
	sim.Jobs[0].Status = models.JobRunning
	sim.Jobs[0].AssignedWorker = &running

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "already running")
}

func TestValidateAndApply_RejectsCompletedJob(t *testing.T) {
	sim := newTestSim()
	sim.Jobs[0].Status = models.JobCompleted

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "already completed")
}

func TestValidateAndApply_RejectsInsufficientCPU(t *testing.T) {
	sim := newTestSim()
	sim.Jobs[0].RequiredCPU = 100

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "insufficient CPU")
}

func TestValidateAndApply_RejectsInsufficientMemory(t *testing.T) {
	sim := newTestSim()
	sim.Jobs[0].RequiredMemory = 100

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "insufficient memory")
}

func TestValidateAndApply_RejectsUnavailableWorker(t *testing.T) {
	sim := newTestSim()
	sim.Workers[0].Available = false

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "unavailable")
}

func TestValidateAndApply_RejectsJobNotYetArrived(t *testing.T) {
	sim := newTestSim()
	sim.Jobs[0].ArrivalTick = 5
	sim.Tick = 0

	result := ValidateAndApply(sim, []models.Assignment{{WorkerID: 1, JobID: 1}})
	requireSingleRejection(t, result, "has not arrived")
}

func TestValidateAndApply_RejectsDuplicateJobInSameBatch(t *testing.T) {
	sim := newTestSim()
	sim.Workers = append(sim.Workers, &models.Worker{ID: 2, TotalCPU: 4, TotalMemory: 8, AvailableCPU: 4, AvailableMemory: 8, Available: true})

	result := ValidateAndApply(sim, []models.Assignment{
		{WorkerID: 1, JobID: 1},
		{WorkerID: 2, JobID: 1},
	})

	if len(result.Accepted) != 1 {
		t.Fatalf("expected exactly 1 accepted assignment, got %d", len(result.Accepted))
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected assignment, got %d", len(result.Rejected))
	}
	if got := result.Rejected[0].Reason; !strings.Contains(got, "already assigned earlier in this same request") {
		t.Errorf("unexpected rejection reason: %s", got)
	}
}

func TestValidateAndApply_ResourceAccumulationWithinBatch(t *testing.T) {
	// A single worker with capacity for exactly one of two jobs submitted in
	// the same batch: the second must be rejected for insufficient resources
	// even though the worker started with enough capacity for either job
	// individually.
	sim := &models.Simulation{
		Workers: []*models.Worker{
			{ID: 1, TotalCPU: 4, TotalMemory: 8, AvailableCPU: 4, AvailableMemory: 8, Available: true},
		},
		Jobs: []*models.Job{
			{ID: 1, Status: models.JobReady, RequiredCPU: 3, RequiredMemory: 3},
			{ID: 2, Status: models.JobReady, RequiredCPU: 3, RequiredMemory: 3},
		},
	}

	result := ValidateAndApply(sim, []models.Assignment{
		{WorkerID: 1, JobID: 1},
		{WorkerID: 1, JobID: 2},
	})

	if len(result.Accepted) != 1 {
		t.Fatalf("expected exactly 1 accepted assignment, got %d: %+v", len(result.Accepted), result.Accepted)
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected assignment, got %d", len(result.Rejected))
	}
}

func requireSingleRejection(t *testing.T, result ScheduleResult, substr string) {
	t.Helper()
	if len(result.Accepted) != 0 {
		t.Fatalf("expected no accepted assignments, got %+v", result.Accepted)
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected assignment, got %d", len(result.Rejected))
	}
	if !strings.Contains(result.Rejected[0].Reason, substr) {
		t.Errorf("expected rejection reason to contain %q, got %q", substr, result.Rejected[0].Reason)
	}
}
