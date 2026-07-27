package web

import (
	"strings"

	"github.com/renorris/openfsd/pkg/protocol"
)

// User-admin privilege helpers (post user-dashboard self-service design).
//
// - Supervisor+: user editor directory, create, rating + full profile mutation.
//   Full profile mutation still cannot target users with a higher network rating.
// - Instructor1+: sweatbox control plane (HTML + PE JSON).
// - Administrator: config + airport editor (unchanged).

func canAccessUserEditor(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingSupervisor
}

func canFullMutateUsers(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingSupervisor
}

// canAdjustUserRatings gates JSON updateUser and rating POSTs. Same threshold
// as canAccessUserEditor (SUP+) after the I1 rating-only tier was removed.
func canAdjustUserRatings(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingSupervisor
}

// canFullMutateTarget is true when the actor may change name/password (and
// create is separate). Requires SUP+ and target.network_rating ≤ actor.
func canFullMutateTarget(actor, targetNetworkRating protocol.NetworkRating) bool {
	return canFullMutateUsers(actor) && targetNetworkRating <= actor
}

func canAccessSweatbox(r protocol.NetworkRating) bool {
	return r >= protocol.NetworkRatingInstructor1
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

// pilotRatingOptionsAll returns the full official pilot scale (P0…FE).
// Used by the user editor so SUP+ can assign any pilot rating regardless of
// their own flying quals (KD-6).
func pilotRatingOptionsAll(selected int) []ratingOption {
	return pilotRatingOptionsUpTo(int(protocol.PilotRatingFE), selected)
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

// validateNewPassword returns a user-visible error string, or "" if OK.
// Used by account change-password and user-editor create/update (when password non-empty).
func validateNewPassword(pw string) string {
	if len(pw) < 8 {
		return "Password must be at least 8 characters"
	}
	if strings.Contains(pw, ":") {
		return "Password cannot contain colon characters"
	}
	return ""
}
