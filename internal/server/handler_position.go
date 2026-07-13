package server

import (
	"strconv"

	"github.com/renorris/openfsd/internal/session"
)

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

	client.FacilityType = int(facilityType)

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

// handlePilotPosition handles logic for 0.2hz `@` pilot position updates
func (s *Server) handlePilotPosition(client *session.Session, packet []byte) {
	lat, lon, ok := parseLatLon(packet, 4, 5)
	if !ok {
		client.SendError(SyntaxError, "Invalid latitude/longitude")
		return
	}

	const pilotVisRange = 50.0 * 1852.0 // 50 nautical miles

	// Update registry position
	s.registry.UpdatePosition(client, [2]float64{lat, lon}, pilotVisRange)

	// Broadcast position update
	broadcastRanged(s.registry, client, packet)

	// Update state
	client.Transponder.Store(string(getField(packet, 2)))

	groundspeed, _ := strconv.Atoi(string(getField(packet, 7)))
	client.Groundspeed.Store(int32(groundspeed))

	altitude, _ := strconv.Atoi(string(getField(packet, 6)))
	client.Altitude.Store(int32(altitude))

	pbhUint, _ := strconv.ParseUint(string(getField(packet, 8)), 10, 32)
	_, _, heading := pitchBankHeading(uint32(pbhUint))
	client.Heading.Store(int32(heading))

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
