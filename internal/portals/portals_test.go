package portals

import (
	"reflect"
	"testing"
	"time"
)

// hourOffset walks distinct clock hours, which map one-to-one onto seeds.
func hourOffset(h int) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
}

func TestStatusFor_DerivesFromContainmentAndLoad(t *testing.T) {
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
		if got := statusFor(tc.online, tc.load); got != tc.want {
			t.Errorf("statusFor(%t, %d) = %q, want %q", tc.online, tc.load, got, tc.want)
		}
	}
}

func TestSnapshot_IsStableWithinTheHourAndChangesAcrossHours(t *testing.T) {
	hour := time.Date(2026, 8, 11, 14, 0, 0, 0, time.UTC)

	first := snapshotAt(hour)
	for _, within := range []time.Time{hour, hour.Add(time.Second), hour.Add(59 * time.Minute), hour.Add(59*time.Minute + 59*time.Second)} {
		if got := snapshotAt(within); !reflect.DeepEqual(got, first) {
			t.Errorf("snapshot at %s differs from the start of the same hour", within.Format(time.RFC3339))
		}
	}

	// Across a full day, the hourly seed has to actually move the state —
	// otherwise "updates every hour" would be indistinguishable from static.
	distinct := 0
	for h := range 24 {
		if !reflect.DeepEqual(snapshotAt(hour.Add(time.Duration(h)*time.Hour)), first) {
			distinct++
		}
	}
	if distinct == 0 {
		t.Error("every hour of the day produced an identical snapshot")
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
	for hour := range 200 {
		for _, p := range snapshotAt(hourOffset(hour)) {
			if p.Load < 0 || p.Load > 100 {
				t.Fatalf("hour +%d: %s load %d out of range", hour, p.Name, p.Load)
			}
			switch p.Status {
			case StatusOffline:
				if p.Load != 0 {
					t.Fatalf("hour +%d: %s is offline but reports load %d", hour, p.Name, p.Load)
				}
			case StatusUnstable:
				if p.Load < UnstableLoadThreshold {
					t.Fatalf("hour +%d: %s is unstable at load %d, below the threshold", hour, p.Name, p.Load)
				}
			case StatusNominal:
				if p.Load >= UnstableLoadThreshold {
					t.Fatalf("hour +%d: %s is nominal at load %d, at or above the threshold", hour, p.Name, p.Load)
				}
			default:
				t.Fatalf("hour +%d: %s has unexpected status %q", hour, p.Name, p.Status)
			}
		}
	}
}

// Every snapshot must contain all three statuses, not merely reach them
// eventually: the console needs an offline chip and an unstable badge to
// render in any given hour.
func TestSnapshot_AlwaysContainsEveryStatus(t *testing.T) {
	for hour := range 500 {
		seen := map[string]bool{}
		for _, p := range snapshotAt(hourOffset(hour)) {
			seen[p.Status] = true
		}
		for _, status := range []string{StatusNominal, StatusUnstable, StatusOffline} {
			if !seen[status] {
				t.Fatalf("hour +%d: snapshot has no %q portal (got %v)", hour, status, seen)
			}
		}
	}
}

// Which portals draw which status still has to vary, or the panel would show
// the same corridor offline forever.
func TestSnapshot_OfflinePortalVariesAcrossHours(t *testing.T) {
	hour := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)

	offline := map[string]bool{}
	for h := range 48 {
		for _, p := range snapshotAt(hour.Add(time.Duration(h) * time.Hour)) {
			if p.Status == StatusOffline {
				offline[p.Name] = true
			}
		}
	}
	if len(offline) < 2 {
		t.Errorf("across 48 hours only %d distinct portal(s) ever went offline: %v", len(offline), offline)
	}
}

// The reason containment and load stay separate during generation: a healthy
// portal with no traffic has to read as nominal at 0 load, not as offline.
func TestSnapshot_ProducesNominalPortalAtZeroLoad(t *testing.T) {
	for hour := range 500 {
		for _, p := range snapshotAt(hourOffset(hour)) {
			if p.Status == StatusNominal && p.Load == 0 {
				return
			}
		}
	}
	t.Error("no snapshot across 500 hours produced a nominal portal at 0 load")
}
