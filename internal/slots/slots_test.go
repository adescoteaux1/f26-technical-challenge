package slots

import (
	"testing"
	"time"
)

func TestAvailable_IsStableAndFullyPopulated(t *testing.T) {
	first := Available()
	if len(first) != catalogueSize {
		t.Fatalf("expected %d slots, got %d", catalogueSize, len(first))
	}

	for i, slot := range first {
		switch {
		case slot.ID == "":
			t.Errorf("slot %d has no ID", i)
		case slot.Destination == "" || slot.Portal == "":
			t.Errorf("%s is missing destination or portal", slot.ID)
		case slot.SeatsAvailable < 1:
			t.Errorf("%s offers %d seats, expected at least 1", slot.ID, slot.SeatsAvailable)
		case slot.DurationMinutes < 1 || slot.FareCredits < 1:
			t.Errorf("%s has a nonsensical duration or fare: %+v", slot.ID, slot)
		}
	}

	for i, slot := range Available() {
		if slot.ID != first[i].ID || slot.SeatsAvailable != first[i].SeatsAvailable {
			t.Fatalf("slot %d changed between calls: %+v then %+v", i, first[i], slot)
		}
	}
}

func TestAvailable_IDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, slot := range Available() {
		if seen[slot.ID] {
			t.Errorf("duplicate slot ID %s", slot.ID)
		}
		seen[slot.ID] = true
	}
}

// Departures are computed from the current day, so the catalogue can't age
// into the past however long the server runs.
func TestAvailable_DeparturesAreAlwaysUpcoming(t *testing.T) {
	for _, now := range []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2030, 6, 15, 23, 59, 0, 0, time.UTC),
	} {
		for _, slot := range availableAt(now) {
			if !slot.DepartsAt.After(now) {
				t.Errorf("at %s, %s departs at %s — not upcoming",
					now.Format(time.RFC3339), slot.ID, slot.DepartsAt.Format(time.RFC3339))
			}
		}
	}
}

func TestFind_MatchesTheCatalogue(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	want := availableAt(now)[3]

	got, ok := findAt(want.ID, now)
	if !ok {
		t.Fatalf("%s is in the catalogue but Find missed it", want.ID)
	}
	if got != want {
		t.Errorf("Find returned %+v, want %+v", got, want)
	}

	if _, ok := findAt("SLOT-9999", now); ok {
		t.Error("Find reported a slot that was never in the catalogue")
	}
}

func TestOutcomeFrom_MapsRollsToOutcomes(t *testing.T) {
	cases := []struct {
		roll        float64
		failureKind int
		want        Outcome
	}{
		{0, 0, OutcomeSlotTaken},
		{0, 1, OutcomeCorridorUnstable},
		{submissionFailureRate - 0.01, 0, OutcomeSlotTaken},
		{submissionFailureRate, 0, OutcomeConfirmed},
		{1, 1, OutcomeConfirmed},
	}
	for _, tc := range cases {
		if got := outcomeFrom(tc.roll, tc.failureKind); got != tc.want {
			t.Errorf("outcomeFrom(%v, %d) = %q, want %q", tc.roll, tc.failureKind, got, tc.want)
		}
	}
}

// The endpoint is only useful as a challenge if failures actually show up.
func TestSimulateOutcome_ProducesBothSuccessAndFailure(t *testing.T) {
	seen := map[Outcome]int{}
	for range 500 {
		seen[SimulateOutcome()]++
	}
	if seen[OutcomeConfirmed] == 0 {
		t.Error("500 submissions never succeeded")
	}
	if seen[OutcomeSlotTaken]+seen[OutcomeCorridorUnstable] == 0 {
		t.Error("500 submissions never failed")
	}
}
