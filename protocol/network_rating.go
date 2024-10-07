package protocol

type NetworkRating int

const (
	NetworkRatingINAC = iota - 1
	NetworkRatingSUS
	NetworkRatingOBS
	NetworkRatingS1
	NetworkRatingS2
	NetworkRatingS3
	NetworkRatingC1
	NetworkRatingC2
	NetworkRatingC3
	NetworkRatingI1
	NetworkRatingI2
	NetworkRatingI3
	NetworkRatingSUP
	NetworkRatingADM
)

var networkRatingToLongString = map[NetworkRating]string{
	-1: "Inactive",
	0:  "Suspended",
	1:  "Observer",
	2:  "Tower Trainee",
	3:  "Tower Controller",
	4:  "Senior Student",
	5:  "Enroute Controller",
	6:  "Controller 2",
	7:  "Senior Controller",
	8:  "Instructor",
	9:  "Instructor 2",
	10: "Senior Instructor",
	11: "Supervisor",
	12: "Administrator",
}

var networkRatingToShortString = map[NetworkRating]string{
	-1: "INAC",
	0:  "SUS",
	1:  "OBS",
	2:  "S1",
	3:  "S2",
	4:  "S3",
	5:  "C1",
	6:  "C2",
	7:  "C3",
	8:  "I1",
	9:  "I2",
	10: "I3",
	11: "SUP",
	12: "ADM",
}

func (n NetworkRating) String() string {
	str, ok := networkRatingToLongString[n]
	if !ok {
		return ""
	}

	return str
}

func (n NetworkRating) ShortString() string {
	str, ok := networkRatingToShortString[n]
	if !ok {
		return ""
	}

	return str
}

func ForEachNetworkRating(f func(id NetworkRating, shortString, longString string)) {
	for k, v := range networkRatingToShortString {
		f(k, v, networkRatingToLongString[k])
	}
}

func (n NetworkRating) IsSupervisorOrAbove() bool {
	return n >= NetworkRatingSUP
}
