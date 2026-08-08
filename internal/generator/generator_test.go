package generator

import (
	"testing"

	"github.com/adescoteaux1/generate-control-tower/internal/models"
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

func TestGenerate_MultiLegVoyagesAreWellFormed(t *testing.T) {
	sawAtLeastOneCorridor := false

	for _, profile := range AllProfiles {
		cycle, err := Generate(profile, 99)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", profile, err)
		}
		for _, v := range cycle.Voyages {
			if len(v.Legs) == 0 {
				continue // single-hop voyage, nothing corridor-specific to check
			}
			sawAtLeastOneCorridor = true

			if len(v.Legs) < 2 {
				t.Errorf("%s: voyage %d has Legs but fewer than 2 of them (%d)", profile, v.ID, len(v.Legs))
			}
			if v.LegIndex != 0 {
				t.Errorf("%s: voyage %d should start at LegIndex 0, got %d", profile, v.ID, v.LegIndex)
			}
			first := v.Legs[0]
			if v.RequiredPower != first.RequiredPower || v.RequiredContainment != first.RequiredContainment {
				t.Errorf("%s: voyage %d's top-level requirements don't match its first leg", profile, v.ID)
			}
			if v.EstimatedDuration != first.EstimatedDuration || v.RemainingDuration != first.EstimatedDuration {
				t.Errorf("%s: voyage %d's duration fields don't match its first leg", profile, v.ID)
			}
			for i, leg := range v.Legs {
				if leg.RequiredPower <= 0 || leg.RequiredContainment <= 0 || leg.EstimatedDuration <= 0 {
					t.Errorf("%s: voyage %d leg %d has a non-positive field: %+v", profile, v.ID, i, leg)
				}
			}
			if v.ArrivalDeadline <= v.RequestedTick {
				t.Errorf("%s: voyage %d has a corridor deadline that isn't after its requested tick", profile, v.ID)
			}
		}
	}

	if !sawAtLeastOneCorridor {
		t.Fatal("expected at least one multi-leg corridor voyage across all profiles at this seed")
	}
}

func TestGenerate_PremiumHubsGetTighterSLADeadlines(t *testing.T) {
	for _, profile := range AllProfiles {
		cycle, err := Generate(profile, 7)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", profile, err)
		}

		if len(cycle.PremiumHubs) == 0 {
			t.Fatalf("%s: expected at least one premium hub to be designated", profile)
		}
		premium := make(map[string]bool, len(cycle.PremiumHubs))
		for _, hub := range cycle.PremiumHubs {
			premium[hub] = true
		}

		for _, v := range cycle.Voyages {
			if premium[v.OriginHub] {
				if v.SLADeadline == nil {
					t.Errorf("%s: voyage %d from premium hub %q has no SLADeadline", profile, v.ID, v.OriginHub)
					continue
				}
				if *v.SLADeadline > v.ArrivalDeadline {
					t.Errorf("%s: voyage %d's SLA deadline (%d) is looser than its arrival deadline (%d)", profile, v.ID, *v.SLADeadline, v.ArrivalDeadline)
				}
				if *v.SLADeadline < v.RequestedTick {
					t.Errorf("%s: voyage %d's SLA deadline (%d) is before it was even requested (%d)", profile, v.ID, *v.SLADeadline, v.RequestedTick)
				}
			} else if v.SLADeadline != nil {
				t.Errorf("%s: voyage %d from non-premium hub %q unexpectedly has an SLADeadline", profile, v.ID, v.OriginHub)
			}
		}
	}
}
