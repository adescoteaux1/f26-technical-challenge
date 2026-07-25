package generator

import (
	"testing"

	"github.com/adescoteaux1/generate-oracle/internal/models"
)

func TestGenerate_UnknownProfileErrors(t *testing.T) {
	if _, err := Generate("not-a-real-profile", 1); err == nil {
		t.Fatal("expected an error for an unknown profile")
	}
}

func TestGenerate_DeterministicForSameSeed(t *testing.T) {
	for _, profile := range AllProfiles {
		a, err := Generate(profile, 42)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", profile, err)
		}
		b, err := Generate(profile, 42)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", profile, err)
		}

		if len(a.Jobs) != len(b.Jobs) || len(a.Workers) != len(b.Workers) {
			t.Fatalf("%s: same seed produced different job/worker counts", profile)
		}
		for i := range a.Jobs {
			if a.Jobs[i].RequiredCPU != b.Jobs[i].RequiredCPU || a.Jobs[i].EstimatedRuntime != b.Jobs[i].EstimatedRuntime {
				t.Fatalf("%s: same seed produced different job %d parameters", profile, i)
			}
		}
	}
}

func TestGenerate_AllProfilesProduceValidDAGs(t *testing.T) {
	for _, profile := range AllProfiles {
		sim, err := Generate(profile, 7)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", profile, err)
		}
		if len(sim.Workers) == 0 {
			t.Errorf("%s: expected at least one worker", profile)
		}
		if len(sim.Jobs) == 0 {
			t.Errorf("%s: expected at least one job", profile)
		}

		for _, job := range sim.Jobs {
			for _, depID := range job.Dependencies {
				if depID >= job.ID {
					t.Errorf("%s: job %d depends on %d, which is not an earlier job (graph must be acyclic)", profile, job.ID, depID)
				}
			}
			if len(job.Dependencies) == 0 && job.Status != models.JobReady {
				t.Errorf("%s: job %d has no dependencies but is not ready (status=%s)", profile, job.ID, job.Status)
			}
			if len(job.Dependencies) > 0 && job.Status != models.JobBlocked {
				t.Errorf("%s: job %d has dependencies but is not blocked (status=%s)", profile, job.ID, job.Status)
			}
			if job.RequiredCPU <= 0 || job.RequiredMemory <= 0 {
				t.Errorf("%s: job %d has non-positive resource requirements", profile, job.ID)
			}
			if job.EstimatedRuntime <= 0 {
				t.Errorf("%s: job %d has non-positive estimated runtime", profile, job.ID)
			}
		}
	}
}
