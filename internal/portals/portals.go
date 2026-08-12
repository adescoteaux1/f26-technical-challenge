// Package portals generates the operations console's Portal Network Status
// panel: six fixed portals whose load is randomized, with status derived from
// that load and from whether the containment field is up.
package portals

import (
	"math/rand"
	"time"
)

const (
	StatusNominal  = "nominal"
	StatusUnstable = "unstable"
	StatusOffline  = "offline"
)

const (
	maxLoad               = 100
	unstableLoadThreshold = 80
	offlineChanceOneIn    = 8
)

// Names are the six portals the console renders, in display order.
var Names = []string{
	"Central Hub Alpha",
	"Northern Gateway",
	"Quantum Nexus",
	"Eastern Node",
	"Southern Passage",
	"Western Bridge",
}

// guaranteedStatuses each get a portal reserved for them every hour, so the
// console always has an offline chip, an unstable badge and a healthy corridor
// to render. Left to chance, roughly one hour in seven came back all-nominal.
var guaranteedStatuses = []string{StatusOffline, StatusUnstable, StatusNominal}

// Portal is one card in the panel. Online reports the containment field, Load
// reports traffic: an online portal carrying nothing is idle at 0 load, which
// is a different state from being offline.
type Portal struct {
	Name   string
	Online bool
	Status string
	Load   int
}

// Snapshot returns all six portals. The result is stable for the whole clock
// hour, so polling more often re-reads the same state rather than watching it
// flicker.
func Snapshot() []Portal {
	return snapshotAt(time.Now())
}

func snapshotAt(t time.Time) []Portal {
	return snapshot(rand.New(rand.NewSource(hourSeed(t))))
}

func hourSeed(t time.Time) int64 {
	return t.UTC().Truncate(time.Hour).Unix()
}

func snapshot(rng *rand.Rand) []Portal {
	requiredStatuses := reserveGuaranteedStatuses(rng)

	snapshot := make([]Portal, 0, len(Names))
	for i, name := range Names {
		online, load := sampleState(rng, requiredStatuses[i])
		snapshot = append(snapshot, Portal{
			Name:   name,
			Online: online,
			Status: StatusFor(online, load),
			Load:   load,
		})
	}
	return snapshot
}

// reserveGuaranteedStatuses returns, per portal, the status it must report this
// hour. Portals left empty are unconstrained.
func reserveGuaranteedStatuses(rng *rand.Rand) []string {
	required := make([]string, len(Names))
	for i, portalIndex := range rng.Perm(len(Names))[:len(guaranteedStatuses)] {
		required[portalIndex] = guaranteedStatuses[i]
	}
	return required
}

func sampleState(rng *rand.Rand, requiredStatus string) (online bool, load int) {
	switch requiredStatus {
	case StatusOffline:
		return false, 0
	case StatusUnstable:
		return true, unstableLoadThreshold + rng.Intn(maxLoad-unstableLoadThreshold)
	case StatusNominal:
		return true, rng.Intn(unstableLoadThreshold)
	default:
		return sampleUnconstrainedState(rng)
	}
}

func sampleUnconstrainedState(rng *rand.Rand) (online bool, load int) {
	if rng.Intn(offlineChanceOneIn) == 0 {
		return false, 0
	}
	return true, rng.Intn(maxLoad)
}

// StatusFor derives a portal's status; it is never assigned directly.
func StatusFor(online bool, load int) string {
	switch {
	case !online:
		return StatusOffline
	case load >= unstableLoadThreshold:
		return StatusUnstable
	default:
		return StatusNominal
	}
}
