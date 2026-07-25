package engine

import (
	"fmt"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

// RejectedAssignment pairs a submitted assignment with why it was rejected,
// so applicants can debug their scheduler against descriptive errors.
type RejectedAssignment struct {
	models.Assignment
	Reason string `json:"reason"`
}

// ScheduleResult is the outcome of validating a batch of assignments.
type ScheduleResult struct {
	Accepted []models.Assignment
	Rejected []RejectedAssignment
}

// ValidateAndApply checks every submitted assignment against the current
// simulation state (including resource usage from earlier assignments in the
// same batch) and applies the ones that pass. Invalid assignments are
// reported back with a reason but do not abort the whole batch.
func ValidateAndApply(sim *models.Simulation, assignments []models.Assignment) ScheduleResult {
	result := ScheduleResult{}

	workersByID := make(map[int]*models.Worker, len(sim.Workers))
	for _, w := range sim.Workers {
		workersByID[w.ID] = w
	}
	jobsByID := make(map[int]*models.Job, len(sim.Jobs))
	for _, j := range sim.Jobs {
		jobsByID[j.ID] = j
	}

	claimedJobs := make(map[int]bool) // jobs already assigned earlier in this same batch

	for _, a := range assignments {
		reason := validateOne(sim, workersByID, jobsByID, claimedJobs, a)
		if reason != "" {
			result.Rejected = append(result.Rejected, RejectedAssignment{Assignment: a, Reason: reason})
			sim.Stats.InvalidAssignments++
			continue
		}

		worker := workersByID[a.WorkerID]
		job := jobsByID[a.JobID]

		worker.AvailableCPU -= job.RequiredCPU
		worker.AvailableMemory -= job.RequiredMemory
		worker.RunningJobs = append(worker.RunningJobs, job.ID)

		job.Status = models.JobRunning
		job.AssignedWorker = &a.WorkerID
		start := sim.Tick
		job.StartTick = &start

		claimedJobs[job.ID] = true
		result.Accepted = append(result.Accepted, a)
		sim.Stats.ValidAssignments++
	}

	return result
}

func validateOne(
	sim *models.Simulation,
	workersByID map[int]*models.Worker,
	jobsByID map[int]*models.Job,
	claimedJobs map[int]bool,
	a models.Assignment,
) string {
	worker, ok := workersByID[a.WorkerID]
	if !ok {
		return fmt.Sprintf("worker %d does not exist", a.WorkerID)
	}
	job, ok := jobsByID[a.JobID]
	if !ok {
		return fmt.Sprintf("job %d does not exist", a.JobID)
	}
	if job.ArrivalTick > sim.Tick {
		return fmt.Sprintf("job %d has not arrived yet (arrives tick %d, current tick %d)", job.ID, job.ArrivalTick, sim.Tick)
	}
	if claimedJobs[job.ID] {
		return fmt.Sprintf("job %d was already assigned earlier in this same request", job.ID)
	}
	switch job.Status {
	case models.JobCompleted:
		return fmt.Sprintf("job %d is already completed", job.ID)
	case models.JobRunning:
		return fmt.Sprintf("job %d is already running on worker %d", job.ID, *job.AssignedWorker)
	case models.JobBlocked:
		return fmt.Sprintf("job %d is blocked on incomplete dependencies %v", job.ID, job.Dependencies)
	}
	if !worker.Available {
		return fmt.Sprintf("worker %d is currently unavailable", worker.ID)
	}
	if job.RequiredCPU > worker.AvailableCPU {
		return fmt.Sprintf("worker %d has insufficient CPU for job %d (needs %d, has %d)", worker.ID, job.ID, job.RequiredCPU, worker.AvailableCPU)
	}
	if job.RequiredMemory > worker.AvailableMemory {
		return fmt.Sprintf("worker %d has insufficient memory for job %d (needs %d, has %d)", worker.ID, job.ID, job.RequiredMemory, worker.AvailableMemory)
	}
	return ""
}
