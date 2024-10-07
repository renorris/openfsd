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
	0:  "Basic Member",
	1:  "Private Pilot License",
	3:  "Instrument Rating",
	7:  "Commercial Multi-Engine License",
	15: "Airline Transport Pilot License",
	31: "Flight Instructor",
	63: "Flight Examiner",
}

var pilotRatingToShortString = map[PilotRating]string{
	0:  "NEW",
	1:  "PPL",
	3:  "IR",
	7:  "CMEL",
	15: "ATPL",
	31: "FI",
	63: "FE",
}

func ForEachPilotRating(f func(id PilotRating, shortString, longString string)) {
	for k, v := range pilotRatingToShortString {
		f(k, v, pilotRatingToLongString[k])
	}
}
