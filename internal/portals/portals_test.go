package portals

import (
	"math/rand"
	"testing"
)

func TestStatusFor_DerivesFromOnlineAndLoad(t *testing.T) {
	cases := []struct {
		online bool
		load   int
		want   string
	}{
		{false, 0, StatusOffline},
		{true, 0, StatusNominal}, // online with no traffic is idle, not offline
		{true, 1, StatusNominal},
		{true, 55, StatusNominal},
		{true, 79, StatusNominal},
		{true, 80, StatusUnstable},
		{true, 88, StatusUnstable},
		{true, 100, StatusUnstable},
	}
	for _, tc := range cases {
		if got := StatusFor(tc.online, tc.load); got != tc.want {
			t.Errorf("StatusFor(%t, %d) = %q, want %q", tc.online, tc.load, got, tc.want)
		}
	}
}

func TestSnapshot_ReturnsAllSixPortalsInOrder(t *testing.T) {
	got := Snapshot()
	if len(got) != 6 {
		t.Fatalf("expected exactly 6 portals, got %d", len(got))
	}
	for i, p := range got {
		if p.Name != Names[i] {
			t.Errorf("portal %d: name = %q, want %q", i, p.Name, Names[i])
		}
	}
}

// Loads are random, so check the invariants that must hold for every seed
// rather than specific values.
func TestSnapshot_LoadInRangeAndStatusConsistent(t *testing.T) {
	for seed := range int64(200) {
		for _, p := range snapshot(rand.New(rand.NewSource(seed))) {
			if p.Load < 0 || p.Load > 100 {
				t.Fatalf("seed %d: %s load %d out of range", seed, p.Name, p.Load)
			}
			if !p.Online && p.Load != 0 {
				t.Fatalf("seed %d: %s is offline but reports load %d", seed, p.Name, p.Load)
			}
			if want := StatusFor(p.Online, p.Load); p.Status != want {
				t.Fatalf("seed %d: %s status %q does not match online=%t load=%d (want %q)",
					seed, p.Name, p.Status, p.Online, p.Load, want)
			}
		}
	}
}

func TestSnapshot_ReachesEveryStatus(t *testing.T) {
	seen := map[string]bool{}
	for seed := range int64(200) {
		for _, p := range snapshot(rand.New(rand.NewSource(seed))) {
			seen[p.Status] = true
		}
	}
	for _, status := range []string{StatusNominal, StatusUnstable, StatusOffline} {
		if !seen[status] {
			t.Errorf("no snapshot across 200 seeds produced status %q", status)
		}
	}
}

// The point of tracking Online separately: a healthy portal with no traffic
// must read as nominal at 0 load, not as offline.
func TestSnapshot_ProducesIdleOnlinePortal(t *testing.T) {
	for seed := range int64(500) {
		for _, p := range snapshot(rand.New(rand.NewSource(seed))) {
			if p.Online && p.Load == 0 {
				if p.Status != StatusNominal {
					t.Fatalf("seed %d: idle online portal %s reported %q, want %q",
						seed, p.Name, p.Status, StatusNominal)
				}
				return
			}
		}
	}
	t.Error("no snapshot across 500 seeds produced an online portal at 0 load")
}
