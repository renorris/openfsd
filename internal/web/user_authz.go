package web

import (
	"github.com/renorris/openfsd/pkg/protocol"
)

// User-admin privilege helpers.
//
// - Instructor1–3 (and above): browse directory; adjust network + pilot ratings
//   up to the actor's own ceilings (any target).
// - Supervisor+: full mutation (create, name, password) in addition to ratings.
//   Full profile mutation still cannot target users with a higher network rating.

const (
	// pilotRatingMin/Max bound stored pilot_rating values.
	pilotRatingMin = 0
	pilotRatingMax = 5
)

func canAccessUserEditor(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingInstructor1
}

func canFullMutateUsers(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingSupervisor
}

func canAdjustUserRatings(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingInstructor1
}

// canFullMutateTarget is true when the actor may change name/password (and
// create is separate). Requires SUP+ and target.network_rating <= actor.
func canFullMutateTarget(actor, targetNetworkRating protocol.NetworkRating) bool {
	return canFullMutateUsers(actor) && targetNetworkRating <= actor
}

func pilotRatingLabel(v int) string {
	switch v {
	case 0:
		return "None (0)"
	case 1:
		return "P1"
	case 2:
		return "P2"
	case 3:
		return "P3"
	case 4:
		return "P4"
	case 5:
		return "P5"
	default:
		return "Unknown"
	}
}

// pilotRatingOptionsUpTo returns pilot rating select options 0..maxInclusive
// (clamped to pilotRatingMax).
func pilotRatingOptionsUpTo(maxInclusive int, selected int) []ratingOption {
	if maxInclusive > pilotRatingMax {
		maxInclusive = pilotRatingMax
	}
	if maxInclusive < pilotRatingMin {
		maxInclusive = pilotRatingMin
	}
	out := make([]ratingOption, 0, maxInclusive-pilotRatingMin+1)
	for v := pilotRatingMin; v <= maxInclusive; v++ {
		out = append(out, ratingOption{
			Value:    v,
			Label:    pilotRatingLabel(v),
			Selected: v == selected,
		})
	}
	return out
}
