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
// create is separate). Requires SUP+ and target.network_rating ≤ actor.
func canFullMutateTarget(actor, targetNetworkRating protocol.NetworkRating) bool {
	return canFullMutateUsers(actor) && targetNetworkRating <= actor
}

func pilotRatingLabel(v int) string {
	// "PPL — Private Pilot License" style for selects / tooltips.
	short := protocol.PilotRatingShort(v)
	long := protocol.PilotRatingLong(v)
	if short == "?" {
		return long
	}
	return short + " — " + long
}

func pilotRatingShort(v int) string {
	return protocol.PilotRatingShort(v)
}

// pilotRatingOptionsUpTo returns official pilot rating options at or below
// maxInclusive (VATSIM scale IDs). maxInclusive that is not an official value
// is treated as the highest official ID ≤ maxInclusive (or P0).
func pilotRatingOptionsUpTo(maxInclusive int, selected int) []ratingOption {
	ceiling := maxValidPilotRatingAtMost(maxInclusive)
	out := make([]ratingOption, 0, len(protocol.PilotRatingScale))
	for _, p := range protocol.PilotRatingScale {
		id := int(p)
		if id > ceiling {
			break
		}
		out = append(out, ratingOption{
			Value:    id,
			Label:    pilotRatingLabel(id),
			Selected: id == selected,
		})
	}
	return out
}

// maxValidPilotRatingAtMost returns the highest official pilot rating ID ≤ n.
// If n is below P0, returns P0.
func maxValidPilotRatingAtMost(n int) int {
	best := int(protocol.PilotRatingNone)
	for _, p := range protocol.PilotRatingScale {
		id := int(p)
		if id <= n && id >= best {
			best = id
		}
	}
	return best
}

func isValidPilotRating(v int) bool {
	return protocol.IsValidPilotRating(v)
}
