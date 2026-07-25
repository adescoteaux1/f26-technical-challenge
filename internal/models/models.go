// Package models defines the core domain types shared by the generator,
// simulation engine, validator, and scoring engine.
package models

import "time"

// JobStatus tracks where a job is in its lifecycle.
type JobStatus string

const (
	JobBlocked   JobStatus = "blocked" // waiting on dependencies
	JobReady     JobStatus = "ready"   // dependencies satisfied, unassigned
	JobRunning   JobStatus = "running" // assigned to a worker, executing
	JobCompleted JobStatus = "completed"
)

// Job is a unit of work submitted by a product team.
type Job struct {
	ID               int    `json:"id"`
	Project          string `json:"project"`
	Priority         int    `json:"priority"`         // 1 (low) - 5 (critical)
	EstimatedRuntime int    `json:"estimatedRuntime"` // ticks required while running
	RequiredCPU      int    `json:"requiredCpu"`
	RequiredMemory   int    `json:"requiredMemory"`
	Deadline         int    `json:"deadline"` // tick by which the job should complete
	Dependencies     []int  `json:"dependencies"`
	ArrivalTick      int    `json:"arrivalTick"` // tick at which the job becomes visible

	Status           JobStatus `json:"status"`
	RemainingRuntime int       `json:"remainingRuntime"`
	AssignedWorker   *int      `json:"assignedWorker,omitempty"`
	ReadyTick        *int      `json:"readyTick,omitempty"` // tick dependencies were satisfied
	StartTick        *int      `json:"startTick,omitempty"`
	CompletionTick   *int      `json:"completionTick,omitempty"`
}

// Worker executes jobs and has finite CPU/memory capacity.
type Worker struct {
	ID               int   `json:"id"`
	TotalCPU         int   `json:"totalCpu"`
	TotalMemory      int   `json:"totalMemory"`
	AvailableCPU     int   `json:"availableCpu"`
	AvailableMemory  int   `json:"availableMemory"`
	RunningJobs      []int `json:"runningJobs"`
	Available        bool  `json:"available"`
	UnavailableUntil int   `json:"unavailableUntil"` // tick at which the worker comes back online; internal bookkeeping
}

// Assignment is a single scheduling decision submitted by the client.
type Assignment struct {
	WorkerID int `json:"workerId"`
	JobID    int `json:"jobId"`
}

// Metrics is the set of category scores reported to the client.
type Metrics struct {
	Throughput        float64 `json:"throughput"`
	WorkerUtilization float64 `json:"workerUtilization"`
	DeadlineSuccess   float64 `json:"deadlineSuccess"`
	Fairness          float64 `json:"fairness"`
	Reliability       float64 `json:"reliability"`
}

// Simulation is one independently-scored workload within an evaluation.
//
// This struct is the full internal representation persisted to storage
// between HTTP requests (the Oracle is stateless per-request). The public
// API response shape is a separate DTO built by the api package, which
// exposes only the fields the spec calls for (and hides jobs that haven't
// arrived yet).
type Simulation struct {
	EvaluationID string    `json:"evaluationId"`
	Number       int       `json:"simulation"`
	Profile      string    `json:"profile"`
	Seed         int64     `json:"seed"`
	Tick         int       `json:"tick"`
	MaxTicks     int       `json:"maxTicks"`
	Workers      []*Worker `json:"workers"`
	Jobs         []*Job    `json:"jobs"` // all jobs, including ones not yet arrived
	Finished     bool      `json:"finished"`
	Score        float64   `json:"score"`
	Metrics      Metrics   `json:"metrics"`

	FailureRate     float64 `json:"failureRate"`
	FailureTicksMin int     `json:"failureTicksMin"`
	FailureTicksMax int     `json:"failureTicksMax"`

	// Stats accumulates running totals used by the scoring engine. It is
	// internal bookkeeping, not part of the public API response.
	Stats SimStats `json:"stats"`
}

// SimStats accumulates running totals used to compute metrics at any point in time.
type SimStats struct {
	TotalJobs                int              `json:"totalJobs"`
	CompletedJobs            int              `json:"completedJobs"`
	DeadlinesMet             int              `json:"deadlinesMet"`
	DeadlinesMissed          int              `json:"deadlinesMissed"`
	TotalWaitTicks           int64            `json:"totalWaitTicks"` // queue wait time summed across completed jobs
	WorkerBusyResourceTicks  int64            `json:"workerBusyResourceTicks"`
	WorkerTotalResourceTicks int64            `json:"workerTotalResourceTicks"`
	InvalidAssignments       int              `json:"invalidAssignments"`
	ValidAssignments         int              `json:"validAssignments"`
	ProjectCompletions       map[string]int   `json:"projectCompletions"`
	ProjectWaitTicks         map[string]int64 `json:"projectWaitTicks"`
}

// VisibleJob is the subset of a job's fields the scheduler is allowed to see:
// jobs that have not yet arrived are omitted entirely from state responses.
func (s *Simulation) VisibleJobs() []*Job {
	visible := make([]*Job, 0, len(s.Jobs))
	for _, j := range s.Jobs {
		if j.ArrivalTick <= s.Tick {
			visible = append(visible, j)
		}
	}
	return visible
}

// Evaluation groups multiple independent simulations sampled across workload profiles.
type Evaluation struct {
	ID                string        `json:"evaluationId"`
	UserID            string        `json:"-"` // owner; never returned to the client directly
	TotalSimulations  int           `json:"totalSimulations"`
	CurrentSimulation int           `json:"simulation"` // 1-indexed
	Simulations       []*Simulation `json:"-"`
	Finished          bool          `json:"finished"`
	OverallScore      float64       `json:"overallScore"`
	Metrics           Metrics       `json:"metrics"`

	// ProfilePlan is the full sequence of workload profiles sampled at
	// creation time (see evaluation.sampleProfileOrder). It is internal
	// bookkeeping, never returned to the client.
	ProfilePlan []string `json:"-"`
}

// User is an applicant who has registered to run evaluations against the
// Oracle. NUID (Northeastern University ID) plus email double as the
// registration credential pair; Token is the opaque bearer credential
// issued at register/login time and required on every other endpoint.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	NUID      string    `json:"nuid"`
	Token     string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}
