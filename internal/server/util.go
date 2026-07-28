package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// FSD error codes — aliases of protocol.ErrorCode for wire values 1–17.
const (
	CallsignInUseError                       = int(protocol.CallsignInUseError)
	CallsignInvalidError                     = int(protocol.CallsignInvalidError)
	AlreadyRegisteredError                   = int(protocol.AlreadyRegisteredError)
	SyntaxError                              = int(protocol.SyntaxError)
	SourceInvalidError                       = int(protocol.SourceInvalidError)
	InvalidLogonError                        = int(protocol.InvalidLogonError)
	NoSuchCallsignError                      = int(protocol.NoSuchCallsignError)
	NoFlightPlanError                        = int(protocol.NoFlightPlanError)
	NoWeatherProfileError                    = int(protocol.NoWeatherProfileError)
	InvalidProtocolRevisionError             = int(protocol.InvalidProtocolRevisionError)
	RequestedLevelTooHighError               = int(protocol.RequestedLevelTooHighError)
	ServerFullError                          = int(protocol.ServerFullError)
	CertificateSuspendedError                = int(protocol.CertificateSuspendedError)
	InvalidControlError                      = int(protocol.InvalidControlError)
	InvalidPositionForRatingError            = int(protocol.InvalidPositionForRatingError)
	UnauthorizedSoftwareError                = int(protocol.UnauthorizedSoftwareError)
	ClientAuthenticationResponseTimeoutError = int(protocol.ClientAuthenticationResponseTimeoutError)
)

// NetworkRating re-exports protocol.NetworkRating (including Administator typo).
type NetworkRating = protocol.NetworkRating

const (
	NetworkRatingInactive     = protocol.NetworkRatingInactive
	NetworkRatingSuspended    = protocol.NetworkRatingSuspended
	NetworkRatingObserver     = protocol.NetworkRatingObserver
	NetworkRatingStudent1     = protocol.NetworkRatingStudent1
	NetworkRatingStudent2     = protocol.NetworkRatingStudent2
	NetworkRatingStudent3     = protocol.NetworkRatingStudent3
	NetworkRatingController1  = protocol.NetworkRatingController1
	NetworkRatingController2  = protocol.NetworkRatingController2
	NetworkRatingController3  = protocol.NetworkRatingController3
	NetworkRatingInstructor1  = protocol.NetworkRatingInstructor1
	NetworkRatingInstructor2  = protocol.NetworkRatingInstructor2
	NetworkRatingInstructor3  = protocol.NetworkRatingInstructor3
	NetworkRatingSupervisor   = protocol.NetworkRatingSupervisor
	NetworkRatingAdministator = protocol.NetworkRatingAdministator
)

func countFields(packet []byte) int {
	return protocol.CountFields(packet)
}

func getField(packet []byte, index int) []byte {
	return protocol.Field(packet, index)
}

// mostLikelyJwt returns whether a given byte slice is most likely a JWT token.
// Accepts standard and base64url header encodings (JWT uses base64url).
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

	decode := func(enc *base64.Encoding) ([]byte, error) {
		buf := make([]byte, 0, 256)
		return enc.AppendDecode(buf, rawJwtHeader)
	}
	buf, err := decode(base64.RawURLEncoding)
	if err != nil {
		buf, err = decode(base64.URLEncoding)
	}
	if err != nil {
		buf, err = decode(base64.RawStdEncoding)
	}
	if err != nil {
		buf, err = decode(base64.StdEncoding)
	}
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

// sanitizeRealName strips delimiters that would break colon-framed FSD packets.
func sanitizeRealName(name string) string {
	if name == "" {
		return name
	}
	name = strings.ReplaceAll(name, ":", " ")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// rewriteField replaces colon-delimited field index with value and returns a new packet.
// Preserves trailing CRLF if present. Returns original packet if index is missing.
func rewriteField(packet []byte, index int, value string) []byte {
	if index < 0 || len(packet) == 0 {
		return packet
	}
	body := packet
	suffix := []byte(nil)
	if bytes.HasSuffix(body, []byte("\r\n")) {
		suffix = []byte("\r\n")
		body = body[:len(body)-2]
	} else if bytes.HasSuffix(body, []byte("\n")) {
		suffix = []byte("\n")
		body = body[:len(body)-1]
	}

	fields := bytes.Split(body, []byte(":"))
	if index >= len(fields) {
		return packet
	}
	fields[index] = []byte(value)
	out := bytes.Join(fields, []byte(":"))
	if len(suffix) > 0 {
		out = append(out, suffix...)
	}
	return out
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

// parseLatLon extracts two base-10-encoded float64 values from a packet at the specified field indices.
// Rejects NaN, Inf, and coordinates outside lat∈[-90,90], lon∈[-180,180].
func parseLatLon(packet []byte, latIndex, lonIndex int) (lat float64, lon float64, ok bool) {
	rawLat := getField(packet, latIndex)
	rawLon := getField(packet, lonIndex)
	lat, err := strconv.ParseFloat(btoa(rawLat), 64)
	if err != nil {
		return
	}
	lon, err = strconv.ParseFloat(btoa(rawLon), 64)
	if err != nil {
		return
	}
	if !validLatLon(lat, lon) {
		return 0, 0, false
	}
	ok = true
	return
}

func validLatLon(lat, lon float64) bool {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) {
		return false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return false
	}
	return true
}

// parseVisRange parses an FSD-encoded visibility range and returns the distance in meters.
// maxNM caps the range (≤0 means 1500 NM). Non-finite / non-positive values fail.
func parseVisRange(packet []byte, index int, maxNM float64) (visRange float64, ok bool) {
	visRangeNauticalMiles, err := strconv.ParseFloat(btoa(getField(packet, index)), 64)
	if err != nil {
		return
	}
	if math.IsNaN(visRangeNauticalMiles) || math.IsInf(visRangeNauticalMiles, 0) || visRangeNauticalMiles <= 0 {
		return
	}
	if maxNM <= 0 {
		maxNM = 1500
	}
	if visRangeNauticalMiles > maxNM {
		visRangeNauticalMiles = maxNM
	}

	// Convert to meters
	visRange = visRangeNauticalMiles * 1852.0

	ok = true
	return
}

// forwardClientQuery freely routes a client query packet depending on the recipient.
func forwardClientQuery(reg Registry, client *session.Session, packet []byte) {
	recipient := getField(packet, 1)

	if len(recipient) < 2 {
		client.SendError(NoSuchCallsignError, "Invalid recipient")
		return
	}

	switch string(recipient) {
	case "@94835":
		// Broadcast to in-range ATC
		broadcastRangedAtcOnly(reg, client, packet)
		return
	case "@94836":
		// Broadcast to all in-range clients
		broadcastRanged(reg, client, packet)
		return
	}

	sendDirectOrErr(reg, client, recipient, packet)
}

// atcRangeSearcher is an optional Registry extension for ATC-only ranged fan-out.
type atcRangeSearcher interface {
	SearchATC(s *session.Session, fn func(*session.Session) bool)
}

// broadcastRanged broadcasts a packet to all clients in range.
// Uses SendPosition (non-blocking, latest-wins) so a slow peer cannot stall
// the sender's position path under dense fan-out.
// Skips Synthetic recipients (sweatbox); direct registry.Send still reaches them
// and relies on SenderWorker drain.
func broadcastRanged(reg Registry, client *session.Session, packet []byte) {
	packetStr := string(packet)
	reg.Search(client, func(recipient *session.Session) bool {
		if recipient.Synthetic {
			return true
		}
		_ = recipient.SendPosition(packetStr)
		return true
	})
}

// broadcastRangedVelocity broadcasts a packet to all clients in range
// supporting the Vatsim2022 (101) protocol revision.
// Skips Synthetic recipients (sweatbox).
func broadcastRangedVelocity(reg Registry, client *session.Session, packet []byte) {
	packetStr := string(packet)
	reg.Search(client, func(recipient *session.Session) bool {
		if recipient.Synthetic {
			return true
		}
		if recipient.ProtoRevision != 101 {
			return true
		}
		_ = recipient.SendPosition(packetStr)
		return true
	})
}

// broadcastRangedAtcOnly broadcasts a packet to all ATC clients in range.
// Skips Synthetic recipients (sweatbox; belt-and-suspenders with IsAtc filter).
// Uses TrySend so a slow peer cannot stall the sender / gnet event loop.
func broadcastRangedAtcOnly(reg Registry, client *session.Session, packet []byte) {
	packetStr := string(packet)
	fn := func(recipient *session.Session) bool {
		if recipient.Synthetic {
			return true
		}
		if !recipient.IsAtc {
			return true
		}
		_ = recipient.TrySend(packetStr)
		return true
	}
	if as, ok := reg.(atcRangeSearcher); ok {
		as.SearchATC(client, func(recipient *session.Session) bool {
			if recipient.Synthetic {
				return true
			}
			_ = recipient.TrySend(packetStr)
			return true
		})
		return
	}
	reg.Search(client, fn)
}

// broadcastAll broadcasts a packet to the entire server.
// Skips Synthetic recipients (sweatbox). Uses non-blocking TrySend.
func broadcastAll(reg Registry, client *session.Session, packet []byte) {
	packetStr := string(packet)
	reg.All(client, func(recipient *session.Session) bool {
		if recipient.Synthetic {
			return true
		}
		_ = recipient.TrySend(packetStr)
		return true
	})
}

// broadcastAllATC broadcasts a packet to all ATC on entire server.
// Skips Synthetic recipients (sweatbox). Uses non-blocking TrySend.
func broadcastAllATC(reg Registry, client *session.Session, packet []byte) {
	packetStr := string(packet)
	reg.All(client, func(recipient *session.Session) bool {
		if recipient.Synthetic {
			return true
		}
		if !recipient.IsAtc {
			return true
		}
		_ = recipient.TrySend(packetStr)
		return true
	})
}

// broadcastAllSupervisors broadcasts a packet to all supervisors on the server.
// Skips Synthetic recipients (sweatbox). Uses non-blocking TrySend.
func broadcastAllSupervisors(reg Registry, client *session.Session, packet []byte) {
	packetStr := string(packet)
	reg.All(client, func(recipient *session.Session) bool {
		if recipient.Synthetic {
			return true
		}
		if recipient.NetworkRating < NetworkRatingSupervisor {
			return true
		}
		_ = recipient.TrySend(packetStr)
		return true
	})
}

// sendDirectOrErr attempts to send a packet directly to a recipient.
// If the registry responds with ErrCallsignDoesNotExist, the client
// is notified with a NoSuchCallsignError.
// Uses TrySend via Find so fan-out does not block on a slow peer.
// When the target is remote (HybridRegistry + mesh directory), forwards via mesh.
func sendDirectOrErr(reg Registry, client *session.Session, recipient []byte, packet []byte) {
	cs := string(recipient)
	target, err := reg.Find(cs)
	if err == nil {
		if !target.TrySend(string(packet)) {
			// Queue full or disconnecting — soft drop for abuse resistance.
		}
		return
	}
	// Remote path
	if hr, ok := reg.(*HybridRegistry); ok && hr.Mesh() != nil {
		if _, _, ok := hr.LookupRemote(cs); ok {
			if meshErr := hr.MeshSendDirect(cs, packet); meshErr != nil {
				client.SendError(NoSuchCallsignError, "No such callsign")
			}
			return
		}
	}
	client.SendError(NoSuchCallsignError, "No such callsign")
}

// extractFlightplanInfoSection extracts the useful flightplan information from an $FP or $AM packet
func extractFlightplanInfoSection(packet []byte) (fpl string) {
	// Advance past SOURCE:DEST (and TARGET for $AM) using the same IndexByte+1
	// slice step as historical rebaseToNextField — kept inline to avoid a
	// second copy of that helper after the protocol extraction.
	skipFields := 2
	if getPacketType(packet) != PacketTypeFlightPlan {
		skipFields = 3 // PacketTypeFlightPlanAmendment
	}
	for range skipFields {
		packet = packet[bytes.IndexByte(packet, ':')+1:]
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
	// Map 10 bits of resolution to degrees [0..359]
	const conversionRatio float64 = 359.0 / 1023.0
	const mask uint32 = 1023 // 0b1111111111

	pitch = float64(packed>>22&mask) * conversionRatio
	bank = float64(packed>>12&mask) * conversionRatio
	heading = float64(packed>>2&mask) * conversionRatio

	return
}

// packPitchBankHeading encodes pitch/bank/heading degrees into the FSD PBH
// uint32 (10 bits each; lowest 2 bits zero). Inverse of pitchBankHeading.
// Angles outside [0, 360) are normalized; values near 360 map to 1023.
func packPitchBankHeading(pitch, bank, heading float64) uint32 {
	const invRatio float64 = 1023.0 / 359.0
	const mask uint32 = 1023

	encode := func(deg float64) uint32 {
		// Normalize to [0, 360).
		for deg < 0 {
			deg += 360
		}
		for deg >= 360 {
			deg -= 360
		}
		// 360° ≡ 0° on the wire scale; map [0, 359] via round.
		if deg > 359 {
			deg = 359
		}
		v := uint32(deg*invRatio + 0.5)
		if v > mask {
			v = mask
		}
		return v
	}
	p := encode(pitch)
	b := encode(bank)
	h := encode(heading)
	return (p << 22) | (b << 12) | (h << 2)
}

func strPtr(str string) *string {
	return &str
}

// sendEnableSendFastPacket sends an 'enable' $SF Send Fast packet to the client
func sendEnableSendFastPacket(client *session.Session) {
	sendSendFastPacket(client, true)
}

// sendDisableSendFastPacket sends a 'disable' $SF Send Fast packet to the client
func sendDisableSendFastPacket(client *session.Session) {
	sendSendFastPacket(client, false)
}

// sendSendFastPacket sends a $SF Send Fast packet to the client
func sendSendFastPacket(client *session.Session, enabled bool) {
	builder := strings.Builder{}
	builder.Grow(32)
	builder.WriteString("$SFSERVER:")
	builder.WriteString(client.Callsign)
	builder.WriteByte(':')
	if enabled {
		builder.WriteByte('1')
	} else {
		builder.WriteByte('0')
	}
	builder.WriteString("\r\n")

	client.Send(builder.String())
}
