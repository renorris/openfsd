package protocol

type PilotRating int

const (
	PilotRatingNEW  = 0
	PilotRatingPPL  = 1
	PilotRatingIR   = 3
	PilotRatingCMEL = 7
	PilotRatingATPL = 15
	PilotRatingFI   = 31
	PilotRatingFE   = 63
)

var pilotRatingToLongString = map[PilotRating]string{
	PilotRatingNEW:  "Basic Member",
	PilotRatingPPL:  "Private Pilot License",
	PilotRatingIR:   "Instrument Rating",
	PilotRatingCMEL: "Commercial Multi-Engine License",
	PilotRatingATPL: "Airline Transport Pilot License",
	PilotRatingFI:   "Flight Instructor",
	PilotRatingFE:   "Flight Examiner",
}

var pilotRatingToShortString = map[PilotRating]string{
	PilotRatingNEW:  "NEW",
	PilotRatingPPL:  "PPL",
	PilotRatingIR:   "IR",
	PilotRatingCMEL: "CMEL",
	PilotRatingATPL: "ATPL",
	PilotRatingFI:   "FI",
	PilotRatingFE:   "FE",
}

func ForEachPilotRating(f func(id PilotRating, shortString, longString string)) {
	for k, v := range pilotRatingToShortString {
		f(k, v, pilotRatingToLongString[k])
	}
}
