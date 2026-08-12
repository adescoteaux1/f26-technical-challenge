// Package slots provides the bookable transit inventory for the operations
// console's booking flow: a stable catalogue of departure slots, and a
// simulated submission outcome that fails often enough that a client has to
// handle it.
package slots

import (
	"fmt"
	"math/rand"
	"time"
)

// Outcome is the result of submitting a booking. Only Confirmed is a success;
// the rest are distinct failures so a client can tell a retryable corridor
// problem from a slot it has genuinely lost.
type Outcome string

const (
	OutcomeConfirmed         Outcome = "confirmed"
	OutcomeSlotTaken         Outcome = "slot_taken"
	OutcomeCorridorUnstable  Outcome = "corridor_unstable"
	OutcomeInsufficientSeats Outcome = "insufficient_seats"
)

// Retryable reports whether resubmitting the identical request could succeed.
// Only a corridor problem is transient; the others need the client to change
// something first.
func (o Outcome) Retryable() bool {
	return o == OutcomeCorridorUnstable
}

// Detail is the human-readable explanation a console can surface as-is.
func (o Outcome) Detail() string {
	switch o {
	case OutcomeConfirmed:
		return "booking confirmed"
	case OutcomeSlotTaken:
		return "another traveler claimed this slot first — pick a different one"
	case OutcomeCorridorUnstable:
		return "corridor destabilized mid-handshake — safe to retry"
	case OutcomeInsufficientSeats:
		return "not enough seats left on this slot for your party"
	default:
		return string(o)
	}
}

// submissionFailureRate is how often SimulateOutcome fails. High enough that a
// client without error handling breaks on the first demo.
const submissionFailureRate = 0.3

// catalogueSeed is fixed so slot IDs and inventory stay identical across
// restarts and across everyone hitting the same server.
const catalogueSeed = 20260811

// Slot is one bookable departure.
type Slot struct {
	ID              string
	Destination     string
	Portal          string
	DepartsAt       time.Time
	DurationMinutes int
	SeatsAvailable  int
	FareCredits     int
}

// listing is a catalogue row minus its departure time, which is computed
// relative to the current day so the inventory never ages into the past.
type listing struct {
	id              string
	destination     string
	portal          string
	dayOffset       int
	departAfterDay  time.Duration
	durationMinutes int
	seatsAvailable  int
	fareCredits     int
}

var (
	destinations = []string{"Alpha-7", "Beta-9", "Gamma-5", "Delta-11", "Epsilon-2",
		"Zeta-12", "Theta-8", "Omega-3", "Sigma-4", "Kappa-6"}

	originPortals = []string{"Meridian Crossing", "Coastal Junction", "Highland Terminus",
		"Solstice Approach", "Umbral Causeway", "Tidewater Arch"}

	departureHours = []int{6, 8, 9, 11, 13, 15, 17, 19, 21}

	catalogue = buildCatalogue()
)

const catalogueSize = 18

func buildCatalogue() []listing {
	rng := rand.New(rand.NewSource(catalogueSeed))

	built := make([]listing, 0, catalogueSize)
	for i := range catalogueSize {
		hour := departureHours[rng.Intn(len(departureHours))]
		built = append(built, listing{
			id:              fmt.Sprintf("SLOT-%04d", 1000+i),
			destination:     destinations[i%len(destinations)],
			portal:          originPortals[i%len(originPortals)],
			dayOffset:       1 + i/3,
			departAfterDay:  time.Duration(hour) * time.Hour,
			durationMinutes: 45 + rng.Intn(6)*15,
			seatsAvailable:  1 + rng.Intn(6),
			fareCredits:     120 + rng.Intn(18)*10,
		})
	}
	return built
}

// Available returns the whole catalogue, soonest departure first.
func Available() []Slot {
	return availableAt(time.Now())
}

func availableAt(now time.Time) []Slot {
	open := make([]Slot, 0, len(catalogue))
	for _, entry := range catalogue {
		open = append(open, entry.slotAt(now))
	}
	return open
}

// Find returns the slot with the given ID. The catalogue is fixed, so a miss
// means the client sent an ID that never existed.
func Find(id string) (Slot, bool) {
	return findAt(id, time.Now())
}

func findAt(id string, now time.Time) (Slot, bool) {
	for _, entry := range catalogue {
		if entry.id == id {
			return entry.slotAt(now), true
		}
	}
	return Slot{}, false
}

func (l listing) slotAt(now time.Time) Slot {
	midnight := now.UTC().Truncate(24 * time.Hour)
	return Slot{
		ID:              l.id,
		Destination:     l.destination,
		Portal:          l.portal,
		DepartsAt:       midnight.AddDate(0, 0, l.dayOffset).Add(l.departAfterDay),
		DurationMinutes: l.durationMinutes,
		SeatsAvailable:  l.seatsAvailable,
		FareCredits:     l.fareCredits,
	}
}

// SimulateOutcome decides whether a submission succeeds. Deliberately
// non-deterministic: the point is that clients cannot assume success. Use
// GET /chaos/probe instead when a reproducible failure is needed for a test.
func SimulateOutcome() Outcome {
	return outcomeFrom(rand.Float64(), rand.Intn(2))
}

func outcomeFrom(failureRoll float64, failureKind int) Outcome {
	if failureRoll >= submissionFailureRate {
		return OutcomeConfirmed
	}
	if failureKind == 0 {
		return OutcomeSlotTaken
	}
	return OutcomeCorridorUnstable
}
