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

		if len(a.Voyages) != len(b.Voyages) || len(a.Gates) != len(b.Gates) {
			t.Fatalf("%s: same seed produced different voyage/gate counts", profile)
		}
		for i := range a.Voyages {
			if a.Voyages[i].RequiredPower != b.Voyages[i].RequiredPower || a.Voyages[i].EstimatedDuration != b.Voyages[i].EstimatedDuration {
				t.Fatalf("%s: same seed produced different voyage %d parameters", profile, i)
			}
		}
	}
}

func TestGenerate_AllProfilesProduceValidDAGs(t *testing.T) {
	for _, profile := range AllProfiles {
		cycle, err := Generate(profile, 7)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", profile, err)
		}
		if len(cycle.Gates) == 0 {
			t.Errorf("%s: expected at least one gate", profile)
		}
		if len(cycle.Voyages) == 0 {
			t.Errorf("%s: expected at least one voyage", profile)
		}

		for _, voyage := range cycle.Voyages {
			for _, depID := range voyage.Prerequisites {
				if depID >= voyage.ID {
					t.Errorf("%s: voyage %d depends on %d, which is not an earlier voyage (graph must be acyclic)", profile, voyage.ID, depID)
				}
			}
			if len(voyage.Prerequisites) == 0 && voyage.Status != models.VoyageBoarding {
				t.Errorf("%s: voyage %d has no prerequisites but is not boarding (status=%s)", profile, voyage.ID, voyage.Status)
			}
			if len(voyage.Prerequisites) > 0 && voyage.Status != models.VoyageAwaitingTransfer {
				t.Errorf("%s: voyage %d has prerequisites but is not awaiting transfer (status=%s)", profile, voyage.ID, voyage.Status)
			}
			if voyage.RequiredPower <= 0 || voyage.RequiredContainment <= 0 {
				t.Errorf("%s: voyage %d has non-positive resource requirements", profile, voyage.ID)
			}
			if voyage.EstimatedDuration <= 0 {
				t.Errorf("%s: voyage %d has non-positive estimated duration", profile, voyage.ID)
			}
		}
	}
}
