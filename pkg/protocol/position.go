package protocol

import (
	"strconv"
	"strings"
)

// formatFloat formats a float for wire without unnecessary trailing zeros
// while preserving reasonable precision for lat/lon style values.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// PilotPosition is an @ pilot position update packet.
type PilotPosition struct {
	TransponderMode    string // Y / N / S (ident / mode C / mode A style wire char)
	Callsign           string
	TransponderCode    string
	NetworkRating      NetworkRating
	Latitude           float64
	Longitude          float64
	TrueAltitude       int
	Groundspeed        int
	PitchBankHeading   uint32
	AltitudeCorrection int
}

// Marshal encodes the @ packet.
func (p PilotPosition) Marshal() []byte {
	var b strings.Builder
	b.Grow(96 + len(p.Callsign))
	b.WriteByte('@')
	b.WriteString(p.TransponderMode)
	b.WriteByte(':')
	b.WriteString(p.Callsign)
	b.WriteByte(':')
	b.WriteString(p.TransponderCode)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(p.NetworkRating)))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.Latitude))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.Longitude))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.TrueAltitude))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.Groundspeed))
	b.WriteByte(':')
	b.WriteString(strconv.FormatUint(uint64(p.PitchBankHeading), 10))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.AltitudeCorrection))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// ParsePilotPosition parses an @ line.
func ParsePilotPosition(line []byte) (PilotPosition, error) {
	if TypeOf(line) != PacketTypePilotPosition {
		return PilotPosition{}, errPacket("pilot position: wrong type")
	}
	// Accept historical min (9) or full docs layout (10).
	if CountFields(line) < 9 {
		return PilotPosition{}, errPacket("pilot position: too few fields")
	}
	modeField := Field(line, 0)
	mode := strings.TrimPrefix(string(modeField), "@")
	rating, err := strconv.Atoi(string(Field(line, 3)))
	if err != nil {
		return PilotPosition{}, errPacket("pilot position: bad network rating")
	}
	lat, err := strconv.ParseFloat(string(Field(line, 4)), 64)
	if err != nil {
		return PilotPosition{}, errPacket("pilot position: bad latitude")
	}
	lon, err := strconv.ParseFloat(string(Field(line, 5)), 64)
	if err != nil {
		return PilotPosition{}, errPacket("pilot position: bad longitude")
	}
	alt, err := strconv.Atoi(string(Field(line, 6)))
	if err != nil {
		return PilotPosition{}, errPacket("pilot position: bad altitude")
	}
	gs, err := strconv.Atoi(string(Field(line, 7)))
	if err != nil {
		return PilotPosition{}, errPacket("pilot position: bad groundspeed")
	}
	pbh, err := strconv.ParseUint(string(Field(line, 8)), 10, 32)
	if err != nil {
		return PilotPosition{}, errPacket("pilot position: bad pitch/bank/heading")
	}
	var corr int
	if CountFields(line) >= 10 {
		corr, err = strconv.Atoi(string(Field(line, 9)))
		if err != nil {
			return PilotPosition{}, errPacket("pilot position: bad altitude correction")
		}
	}
	return PilotPosition{
		TransponderMode:    mode,
		Callsign:           string(Field(line, 1)),
		TransponderCode:    string(Field(line, 2)),
		NetworkRating:      NetworkRating(rating),
		Latitude:           lat,
		Longitude:          lon,
		TrueAltitude:       alt,
		Groundspeed:        gs,
		PitchBankHeading:   uint32(pbh),
		AltitudeCorrection: corr,
	}, nil
}

// ATCPosition is a % ATC position update packet.
type ATCPosition struct {
	Callsign        string
	Frequencies     string // raw &-delimited frequency field
	FacilityType    int
	VisibilityRange int
	NetworkRating   NetworkRating
	Latitude        float64
	Longitude       float64
	UnknownZero     int // static trailing field, always 0 on the wire
}

// Marshal encodes the % packet.
func (p ATCPosition) Marshal() []byte {
	var b strings.Builder
	b.Grow(80 + len(p.Callsign) + len(p.Frequencies))
	b.WriteByte('%')
	b.WriteString(p.Callsign)
	b.WriteByte(':')
	b.WriteString(p.Frequencies)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.FacilityType))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.VisibilityRange))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(p.NetworkRating)))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.Latitude))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.Longitude))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.UnknownZero))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// ParseATCPosition parses a % line.
func ParseATCPosition(line []byte) (ATCPosition, error) {
	if TypeOf(line) != PacketTypeATCPosition {
		return ATCPosition{}, errPacket("atc position: wrong type")
	}
	if CountFields(line) < 7 {
		return ATCPosition{}, errPacket("atc position: too few fields")
	}
	callsign, ok := cutPrefixField(Field(line, 0), "%")
	if !ok {
		return ATCPosition{}, errPacket("atc position: missing % prefix")
	}
	fac, err := strconv.Atoi(string(Field(line, 2)))
	if err != nil {
		return ATCPosition{}, errPacket("atc position: bad facility type")
	}
	vis, err := strconv.Atoi(string(Field(line, 3)))
	if err != nil {
		return ATCPosition{}, errPacket("atc position: bad visibility range")
	}
	rating, err := strconv.Atoi(string(Field(line, 4)))
	if err != nil {
		return ATCPosition{}, errPacket("atc position: bad network rating")
	}
	lat, err := strconv.ParseFloat(string(Field(line, 5)), 64)
	if err != nil {
		return ATCPosition{}, errPacket("atc position: bad latitude")
	}
	lon, err := strconv.ParseFloat(string(Field(line, 6)), 64)
	if err != nil {
		return ATCPosition{}, errPacket("atc position: bad longitude")
	}
	var zero int
	if CountFields(line) >= 8 {
		zero, err = strconv.Atoi(string(Field(line, 7)))
		if err != nil {
			return ATCPosition{}, errPacket("atc position: bad trailing field")
		}
	}
	return ATCPosition{
		Callsign:        callsign,
		Frequencies:     string(Field(line, 1)),
		FacilityType:    fac,
		VisibilityRange: vis,
		NetworkRating:   NetworkRating(rating),
		Latitude:        lat,
		Longitude:       lon,
		UnknownZero:     zero,
	}, nil
}
