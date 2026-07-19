package server

import (
	"strconv"
	"unsafe"

	"github.com/renorris/openfsd/internal/session"
)

// btoa returns a string view of b without allocation. b must not be mutated
// while the string is in use (OK for immediate strconv/atoi).
func btoa(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (s *Server) handleATCPosition(client *session.Session, packet []byte) {
	// Verify and set facility type
	facilityType, err := strconv.ParseInt(string(getField(packet, 2)), 10, 32)
	if err != nil {
		client.SendError(SyntaxError, "Invalid facility type")
		return
	}

	if !isAllowedFacilityType(client.NetworkRating, int(facilityType)) {
		client.SendError(InvalidPositionForRatingError, "Invalid position for rating")
		client.Cancel()
		return
	}

	client.FacilityType.Store(int32(facilityType))

	// ATC frequency field (raw &-delimited wire value, e.g. "28550")
	client.Frequency.Store(string(getField(packet, 1)))

	// Extract location and visibility range
	lat, lon, ok := parseLatLon(packet, 5, 6)
	if !ok {
		client.SendError(SyntaxError, "Invalid latitude/longitude")
		return
	}
	visRange, ok := parseVisRange(packet, 3)
	if !ok {
		client.SendError(SyntaxError, "Invalid visibility range")
		return
	}

	// Update registry position
	s.registry.UpdatePosition(client, [2]float64{lat, lon}, visRange)

	// Broadcast position update
	broadcastRanged(s.registry, client, packet)

	client.LastUpdated.Store(s.clock.Now())
}

// handlePilotPosition handles logic for 0.2hz `@` pilot position updates.
//
// Wire format: @MODE:CALLSIGN:XPDR:RATING:LAT:LON:ALT:GS:PBH:CORR...
// Fields are scanned once (amortized) instead of repeated getField walks.
func (s *Server) handlePilotPosition(client *session.Session, packet []byte) {
	// Need fields 2,4,5,6,7,8 — walk once up to index 8.
	var fields [9][]byte
	if !splitFieldsN(packet, fields[:]) {
		client.SendError(SyntaxError, "Invalid position packet")
		return
	}

	lat, err := strconv.ParseFloat(btoa(fields[4]), 64)
	if err != nil {
		client.SendError(SyntaxError, "Invalid latitude/longitude")
		return
	}
	lon, err := strconv.ParseFloat(btoa(fields[5]), 64)
	if err != nil {
		client.SendError(SyntaxError, "Invalid latitude/longitude")
		return
	}

	const pilotVisRange = 50.0 * 1852.0 // 50 nautical miles

	// Update registry position then fan-out (hot path).
	// packet is an owned immutable copy (eventLoop / gnet dispatch).
	s.registry.UpdatePosition(client, [2]float64{lat, lon}, pilotVisRange)
	broadcastRanged(s.registry, client, packet)

	// Update state from the same field split (copy for atomics that store string).
	client.Transponder.Store(string(fields[2]))

	if groundspeed, err := strconv.Atoi(btoa(fields[7])); err == nil {
		client.Groundspeed.Store(int32(groundspeed))
	}
	if altitude, err := strconv.Atoi(btoa(fields[6])); err == nil {
		client.Altitude.Store(int32(altitude))
	}
	if pbhUint, err := strconv.ParseUint(btoa(fields[8]), 10, 32); err == nil {
		_, _, heading := pitchBankHeading(uint32(pbhUint))
		client.Heading.Store(int32(heading))
	}

	client.LastUpdated.Store(s.clock.Now())

	// Check if we need to update the sendfast state
	if client.ProtoRevision == 101 {
		if client.SendFastEnabled {
			if (client.ClosestVelocityClientDistance / 1852.0) > 5.0 { // 5.0 nautical miles
				client.SendFastEnabled = false
				sendDisableSendFastPacket(client)
			}
		} else {
			if (client.ClosestVelocityClientDistance / 1852.0) < 5.0 { // 5.0 nautical miles
				client.SendFastEnabled = true
				sendEnableSendFastPacket(client)
			}
		}
	}
}

// handleFastPilotPosition handles logic for fast `^`, stopped `#ST`, and slow `#SL` pilot position updates
func (s *Server) handleFastPilotPosition(client *session.Session, packet []byte) {
	// Broadcast position update
	broadcastRangedVelocity(s.registry, client, packet)
}

// splitFieldsN fills dst[i] with field i of a colon-delimited FSD packet (without
// requiring a trailing colon after the last needed field). Returns false if the
// packet has fewer than len(dst) fields.
func splitFieldsN(packet []byte, dst [][]byte) bool {
	start := 0
	field := 0
	for i := 0; i < len(packet) && field < len(dst); i++ {
		c := packet[i]
		if c == ':' {
			dst[field] = packet[start:i]
			field++
			start = i + 1
			continue
		}
		// Treat CR/LF as end of packet body.
		if c == '\r' || c == '\n' {
			if field < len(dst) {
				dst[field] = packet[start:i]
				field++
			}
			return field >= len(dst)
		}
	}
	if field < len(dst) && start <= len(packet) {
		// Remainder after last colon (or whole packet if no colon).
		end := len(packet)
		for end > start && (packet[end-1] == '\r' || packet[end-1] == '\n') {
			end--
		}
		dst[field] = packet[start:end]
		field++
	}
	return field >= len(dst)
}
