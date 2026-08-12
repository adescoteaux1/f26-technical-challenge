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

// UnstableLoadThreshold is the load at or above which a portal reports
// unstable. Exported because it's part of the endpoint's published contract.
const UnstableLoadThreshold = 80

const (
	maxLoad            = 100
	offlineChanceOneIn = 8
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

// Portal is one card in the panel, and mirrors exactly what the endpoint
// returns. Whether the containment field is up is a generation-time detail
// (see portalState); downstream, that distinction lives in Status.
type Portal struct {
	Name   string
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
	states := drawStates(rand.New(rand.NewSource(hourSeed(t))))

	snapshot := make([]Portal, len(Names))
	for i, name := range Names {
		snapshot[i] = newPortal(name, states[i])
	}
	return snapshot
}

func hourSeed(t time.Time) int64 {
	return t.UTC().Truncate(time.Hour).Unix()
}

// portalState is the pair a portal's status is derived from. A portal with no
// traffic is idle at 0 load, which is a different state from being offline, so
// the two facts stay separate until Status is computed.
type portalState struct {
	online bool
	load   int
}

// drawStates gives every portal a freely drawn state, then overwrites one
// randomly chosen portal per guaranteed status.
func drawStates(rng *rand.Rand) []portalState {
	states := make([]portalState, len(Names))
	for i := range states {
		states[i] = drawFreely(rng)
	}
	for i, portalIndex := range rng.Perm(len(Names))[:len(guaranteedStatuses)] {
		states[portalIndex] = drawMatching(rng, guaranteedStatuses[i])
	}
	return states
}

func drawFreely(rng *rand.Rand) portalState {
	if rng.Intn(offlineChanceOneIn) == 0 {
		return portalState{online: false, load: 0}
	}
	return portalState{online: true, load: rng.Intn(maxLoad)}
}

// drawMatching draws a state that statusFor will report as status.
func drawMatching(rng *rand.Rand, status string) portalState {
	switch status {
	case StatusOffline:
		return portalState{online: false, load: 0}
	case StatusUnstable:
		return portalState{online: true, load: UnstableLoadThreshold + rng.Intn(maxLoad-UnstableLoadThreshold)}
	case StatusNominal:
		return portalState{online: true, load: rng.Intn(UnstableLoadThreshold)}
	default:
		panic("portals: no state draw defined for status " + status)
	}
}

// newPortal is the only place a Portal is built, so Status is always derived
// and never assigned.
func newPortal(name string, state portalState) Portal {
	return Portal{
		Name:   name,
		Status: statusFor(state.online, state.load),
		Load:   state.load,
	}
}

func statusFor(online bool, load int) string {
	switch {
	case !online:
		return StatusOffline
	case load >= UnstableLoadThreshold:
		return StatusUnstable
	default:
		return StatusNominal
	}
}
