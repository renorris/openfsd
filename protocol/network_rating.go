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
	NetworkRatingINAC: "Inactive",
	NetworkRatingSUS:  "Suspended",
	NetworkRatingOBS:  "Observer",
	NetworkRatingS1:   "Tower Trainee",
	NetworkRatingS2:   "Tower Controller",
	NetworkRatingS3:   "Senior Student",
	NetworkRatingC1:   "Enroute Controller",
	NetworkRatingC2:   "Controller 2",
	NetworkRatingC3:   "Senior Controller",
	NetworkRatingI1:   "Instructor",
	NetworkRatingI2:   "Instructor 2",
	NetworkRatingI3:   "Senior Instructor",
	NetworkRatingSUP:  "Supervisor",
	NetworkRatingADM:  "Administrator",
}

var networkRatingToShortString = map[NetworkRating]string{
	NetworkRatingINAC: "INAC",
	NetworkRatingSUS:  "SUS",
	NetworkRatingOBS:  "OBS",
	NetworkRatingS1:   "S1",
	NetworkRatingS2:   "S2",
	NetworkRatingS3:   "S3",
	NetworkRatingC1:   "C1",
	NetworkRatingC2:   "C2",
	NetworkRatingC3:   "C3",
	NetworkRatingI1:   "I1",
	NetworkRatingI2:   "I2",
	NetworkRatingI3:   "I3",
	NetworkRatingSUP:  "SUP",
	NetworkRatingADM:  "ADM",
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
