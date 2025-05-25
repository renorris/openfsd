package fsd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// FSD error codes
const (
	CallsignInUseError                       = 1  // Callsign is already in use
	CallsignInvalidError                     = 2  // Callsign is invalid
	AlreadyRegisteredError                   = 3  // Client is already registered
	SyntaxError                              = 4  // Packet syntax is invalid
	SourceInvalidError                       = 5  // Packet source is invalid
	InvalidLogonError                        = 6  // Login credentials or token are invalid
	NoSuchCallsignError                      = 7  // Specified callsign does not exist
	NoFlightPlanError                        = 8  // No flight plan found for the Client
	NoWeatherProfileError                    = 9  // No weather profile available
	InvalidProtocolRevisionError             = 10 // Client uses an unsupported protocol version
	RequestedLevelTooHighError               = 11 // Requested access level is too high
	ServerFullError                          = 12 // Server has reached capacity
	CertificateSuspendedError                = 13 // Client's certificate is suspended
	InvalidControlError                      = 14 // Invalid control command
	InvalidPositionForRatingError            = 15 // Position not allowed for Client's rating
	UnauthorizedSoftwareError                = 16 // Client software is not authorized
	ClientAuthenticationResponseTimeoutError = 17 // Authentication response timed out
)

// FSD Network Ratings

type NetworkRating int

const (
	NetworkRatingInactive NetworkRating = iota - 1
	NetworkRatingSuspended
	NetworkRatingObserver
	NetworkRatingStudent1
	NetworkRatingStudent2
	NetworkRatingStudent3
	NetworkRatingController1
	NetworkRatingController2
	NetworkRatingController3
	NetworkRatingInstructor1
	NetworkRatingInstructor2
	NetworkRatingInstructor3
	NetworkRatingSupervisor
	NetworkRatingAdministator
)

func countFields(packet []byte) int {
	return bytes.Count(packet, []byte(":")) + 1
}

func rebaseToNextField(packet []byte) []byte {
	return packet[bytes.IndexByte(packet, ':')+1:]
}

func getField(packet []byte, index int) []byte {
	for range index {
		packet = rebaseToNextField(packet)
	}

	if i := bytes.IndexByte(packet, ':'); i != -1 {
		packet = packet[:i]
	}

	packet, _ = bytes.CutSuffix(packet, []byte("\r\n"))

	return packet
}

// mostLikelyJwt returns whether a given byte slice is most likely a JWT token
func mostLikelyJwt(token []byte) bool {
	tmp := token
	dotCount := 0
	for {
		if i := bytes.IndexByte(tmp, '.'); i > -1 {
			tmp = tmp[i+1:]
			dotCount++
			if dotCount > 2 {
				return false
			}
			continue
		}
		break
	}
	if dotCount != 2 {
		return false
	}

	rawJwtHeader := token[:bytes.IndexByte(token, '.')]

	buf := make([]byte, 0, 256)
	buf, err := base64.StdEncoding.AppendDecode(buf, rawJwtHeader)
	if err != nil {
		return false
	}

	type jwtHeader struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}

	header := jwtHeader{}
	if err = json.Unmarshal(buf, &header); err != nil {
		return false
	}

	if header.Alg == "" || header.Typ == "" {
		return false
	}

	return true
}

func isValidCallsignLength(callsign []byte) bool {
	return len(callsign) <= 10 && len(callsign) >= 2
}

var reservedCallsigns = []string{
	"SERVER",
	"CLIENT",
	"FP",
}

func isValidClientCallsign(callsign []byte) bool {
	if !isValidCallsignLength(callsign) {
		return false
	}

	// Only uppercase alphanumeric characters and/or hyphen/underscores
	for i := range callsign {
		b := callsign[i]
		if (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b == '-' || b == '_') {
			continue
		}
		return false
	}

	// Check against reserved callsigns
	if slices.Contains(reservedCallsigns, string(callsign)) {
		return false
	}

	return true
}

// isAllowedFacilityType checks if a given network rating is allowed to connect as a given facility type.
func isAllowedFacilityType(rating NetworkRating, facilityType int) bool {
	// Observer facility type is allowed for all ratings
	if facilityType == 0 {
		return true
	}

	// Map of facility types to minimum required rating protocol values
	minRating := map[int]NetworkRating{
		1: NetworkRatingController1, // Flight Service Station (FSS) - C1 and above
		2: NetworkRatingStudent1,    // Delivery (DEL) - S1 and above
		3: NetworkRatingStudent1,    // Ground (GND) - S1 and above
		4: NetworkRatingStudent2,    // Tower (TWR) - S2 and above
		5: NetworkRatingStudent3,    // Approach (APP) - S3 and above
		6: NetworkRatingController1, // Centre (CTR) - C1 and above
	}[facilityType]

	// Return false for invalid facility types
	if minRating == 0 {
		return false
	}

	// Check if the rating meets the minimum requirement
	return rating >= minRating
}

// parseLatLon extracts two base-10-encoded float64 values from a packet at the specified field indices
func parseLatLon(packet []byte, latIndex, lonIndex int) (lat float64, lon float64, ok bool) {
	rawLat := getField(packet, latIndex)
	rawLon := getField(packet, lonIndex)
	lat, err := strconv.ParseFloat(string(rawLat), 64)
	if err != nil {
		return
	}
	lon, err = strconv.ParseFloat(string(rawLon), 64)
	if err != nil {
		return
	}

	ok = true
	return
}

// parseVisRange parses an FSD-encoded visibility range and returns the distance in meters
func parseVisRange(packet []byte, index int) (visRange float64, ok bool) {
	visRangeNauticalMiles, err := strconv.ParseFloat(string(getField(packet, index)), 64)
	if err != nil {
		return
	}

	// Convert to meters
	visRange = visRangeNauticalMiles * 1852.0

	ok = true
	return
}

// forwardClientQuery freely routes a client query packet depending on the recipient.
func forwardClientQuery(po *postOffice, client *Client, packet []byte) {
	recipient := getField(packet, 1)

	if len(recipient) < 2 {
		client.sendError(NoSuchCallsignError, "Invalid recipient")
		return
	}

	switch string(recipient) {
	case "@94835":
		// Broadcast to in-range ATC
		broadcastRangedAtcOnly(po, client, packet)
		return
	case "@94836":
		// Broadcast to all in-range clients
		broadcastRanged(po, client, packet)
		return
	}

	sendDirectOrErr(po, client, recipient, packet)
}

// broadcastRanged broadcasts a packet to all clients in range
func broadcastRanged(po *postOffice, client *Client, packet []byte) {
	packetStr := string(packet)
	po.search(client, func(recipient *Client) bool {
		recipient.send(packetStr)
		return true
	})
}

// broadcastRangedVelocity broadcasts a packet to all clients in range
// supporting the Vatsim2022 (101) protocol revision.
func broadcastRangedVelocity(po *postOffice, client *Client, packet []byte) {
	packetStr := string(packet)
	po.search(client, func(recipient *Client) bool {
		if recipient.protoRevision != 101 {
			return true
		}
		recipient.send(packetStr)
		return true
	})
}

// broadcastRangedAtcOnly broadcasts a packet to all ATC clients in range
func broadcastRangedAtcOnly(po *postOffice, client *Client, packet []byte) {
	packetStr := string(packet)
	po.search(client, func(recipient *Client) bool {
		if !recipient.isAtc {
			return true
		}
		recipient.send(packetStr)
		return true
	})
}

// broadcastAll broadcasts a packet to the entire server
func broadcastAll(po *postOffice, client *Client, packet []byte) {
	packetStr := string(packet)
	po.all(client, func(recipient *Client) bool {
		recipient.send(packetStr)
		return true
	})
}

// broadcastAllATC broadcasts a packet to all ATC on entire server
func broadcastAllATC(po *postOffice, client *Client, packet []byte) {
	packetStr := string(packet)
	po.all(client, func(recipient *Client) bool {
		if !recipient.isAtc {
			return true
		}
		recipient.send(packetStr)
		return true
	})
}

// broadcastAll broadcasts a packet to all supervisors on the server
func broadcastAllSupervisors(po *postOffice, client *Client, packet []byte) {
	packetStr := string(packet)
	po.all(client, func(recipient *Client) bool {
		if recipient.networkRating < NetworkRatingSupervisor {
			return true
		}
		recipient.send(packetStr)
		return true
	})
}

// sendDirectOrErr attempts to send a packet directly to a recipient.
// If the post office responds with an ErrCallsignDoesNotExist, the client
// is notified with a NoSuchCallsignError.
func sendDirectOrErr(po *postOffice, client *Client, recipient []byte, packet []byte) {
	if err := po.send(string(recipient), string(packet)); err != nil {
		client.sendError(NoSuchCallsignError, "No such callsign")
		return
	}
}

// extractFlightplanInfoSection extracts the useful flightplan information from an $FP or $AM packet
func extractFlightplanInfoSection(packet []byte) (fpl string) {
	switch getPacketType(packet) {
	case PacketTypeFlightPlan:
		for range 2 {
			packet = rebaseToNextField(packet)
		}
	default: // PacketTypeFlightPlanAmendment
		for range 3 {
			packet = rebaseToNextField(packet)
		}
	}

	packet, _ = bytes.CutSuffix(packet, []byte("\r\n"))
	return string(packet)
}

// buildFileFlightplanPacket builds an $FP packet
func buildFileFlightplanPacket(source, recipient, fplInfo string) (packet string) {
	prefix := strings.Builder{}
	prefix.WriteString("$FP")
	prefix.WriteString(source)
	prefix.WriteByte(':')
	prefix.WriteString(recipient)
	prefix.WriteByte(':')

	return buildFlightplanPacket(prefix.String(), fplInfo)
}

// buildAmendFlightplanPacket builds an $AM packet
func buildAmendFlightplanPacket(source, recipient, targetCallsign, fplInfo string) (packet string) {
	prefix := strings.Builder{}
	prefix.Grow(36)
	prefix.WriteString("$AM")
	prefix.WriteString(source)
	prefix.WriteByte(':')
	prefix.WriteString(recipient)
	prefix.WriteByte(':')
	prefix.WriteString(targetCallsign)
	prefix.WriteByte(':')

	return buildFlightplanPacket(prefix.String(), fplInfo)
}

func buildFlightplanPacket(prefix, fplInfo string) (packet string) {
	builder := strings.Builder{}
	builder.Grow(len(prefix) + len(fplInfo) + 2)
	builder.WriteString(prefix)
	builder.WriteString(fplInfo)
	builder.WriteString("\r\n")

	return builder.String()
}

func buildBeaconCodePacket(source, recipient, targetCallsign, beaconCode string) (packet string) {
	builder := strings.Builder{}
	builder.Grow(48)
	builder.WriteString("#PC")
	builder.WriteString(source)
	builder.WriteByte(':')
	builder.WriteString(recipient)
	builder.WriteString(":CCP:BC:")
	builder.WriteString(targetCallsign)
	builder.WriteByte(':')
	builder.WriteString(beaconCode)
	builder.WriteString("\r\n")

	return builder.String()
}

func pitchBankHeading(packed uint32) (pitch float64, bank float64, heading float64) {
	// Map 11 bits of resolution to degrees [0..359]
	const conversionRatio float64 = 359.0 / 1023.0
	const mask uint32 = 1023 // 0b1111111111

	pitch = float64(packed>>22&mask) * conversionRatio
	bank = float64(packed>>12&mask) * conversionRatio
	heading = float64(packed>>2&mask) * conversionRatio

	return
}

func strPtr(str string) *string {
	return &str
}

// sendEnableSendFastPacket sends an 'enable' $SF Send Fast packet to the client
func sendEnableSendFastPacket(client *Client) {
	sendSendFastPacket(client, true)
}

// sendDisableSendFastPacket sends a 'disable' $SF Send Fast packet to the client
func sendDisableSendFastPacket(client *Client) {
	sendSendFastPacket(client, false)
}

// sendSendFastPacket sends a $SF Send Fast packet to the client
func sendSendFastPacket(client *Client, enabled bool) {
	builder := strings.Builder{}
	builder.Grow(32)
	builder.WriteString("$SFSERVER:")
	builder.WriteString(client.callsign)
	builder.WriteByte(':')
	if enabled {
		builder.WriteByte('1')
	} else {
		builder.WriteByte('0')
	}
	builder.WriteString("\r\n")

	client.send(builder.String())
}
