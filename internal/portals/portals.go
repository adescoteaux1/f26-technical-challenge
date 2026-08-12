// Package portals generates the Portal Network Status panel's data for the
// operations console: a fixed set of six portals whose containment field may
// be down and whose load is randomized per snapshot, with status derived from
// those two facts.
package portals

import (
	"math/rand"
	"time"
)

// Status values a portal can report. Always derived via StatusFor, never set
// independently.
const (
	StatusNominal  = "nominal"
	StatusUnstable = "unstable"
	StatusOffline  = "offline"
)

const (
	// unstableThreshold is the load at which a rift starts destabilizing —
	// still passing traffic, but too close to its containment capacity to
	// accept new transits reliably.
	unstableThreshold = 80

	// offlineOdds is the 1-in-N chance a portal's containment field is down.
	offlineOdds = 8
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

// Portal is one row of the Portal Network Status panel. Online is a fact about
// the containment field; Load is how hard the portal is being driven. An
// online portal with no traffic is idle at 0 load, which is not the same
// state as being offline.
type Portal struct {
	Name   string
	Online bool
	Status string
	Load   int
}

// Snapshot returns the current state of all six portals.
func Snapshot() []Portal {
	return snapshot(rand.New(rand.NewSource(time.Now().UnixNano())))
}

func snapshot(rng *rand.Rand) []Portal {
	out := make([]Portal, 0, len(Names))
	for _, name := range Names {
		online := rng.Intn(offlineOdds) != 0
		load := 0
		if online {
			load = rng.Intn(100)
		}
		out = append(out, Portal{
			Name:   name,
			Online: online,
			Status: StatusFor(online, load),
			Load:   load,
		})
	}
	return out
}

// StatusFor derives a portal's status from whether its containment field is up
// and how hard it is being driven.
func StatusFor(online bool, load int) string {
	switch {
	case !online:
		return StatusOffline
	case load >= unstableThreshold:
		return StatusUnstable
	default:
		return StatusNominal
	}
}
