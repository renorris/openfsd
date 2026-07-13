package fsd

import (
	"bytes"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/renorris/openfsd/internal/session"
)

func (s *Server) getHandler(packetType PacketType) handlerFunc {
	switch packetType {
	case PacketTypeTextMessage:
		return s.handleTextMessage
	case PacketTypeATCPosition:
		return s.handleATCPosition
	case PacketTypePilotPosition:
		return s.handlePilotPosition
	case PacketTypePilotPositionFast, PacketTypePilotPositionSlow, PacketTypePilotPositionStopped:
		return s.handleFastPilotPosition
	case PacketTypeDeletePilot, PacketTypeDeleteATC:
		return s.handleDelete
	case PacketTypeSquawkbox:
		return s.handleSquawkbox
	case PacketTypeProController:
		return s.handleProcontroller
	case PacketTypeClientQuery, PacketTypeClientQueryResponse:
		return s.handleClientQuery
	case PacketTypeKillRequest:
		return s.handleKillRequest
	case PacketTypeAuthChallenge:
		return s.handleAuthChallenge
	case PacketTypeHandoffRequest, PacketTypeHandoffAccept:
		return s.handleHandoff
	case PacketTypeMetarRequest:
		return s.handleMetarRequest
	case PacketTypeFlightPlan:
		return s.handleFileFlightplan
	case PacketTypeFlightPlanAmendment:
		return s.handleAmendFlightplan
	default:
		return s.emptyHandler
	}
}

func (s *Server) emptyHandler(client *session.Session, packet []byte) {
	slog.Error("empty handler called")
	return
}

func (s *Server) handleTextMessage(client *session.Session, packet []byte) {
	recipient := getField(packet, 1)

	// ATC chat
	if string(recipient) == "@49999" {
		if !client.IsAtc {
			return
		}
		broadcastRangedAtcOnly(s.postOffice, client, packet)
		return
	}

	// Frequency message
	if bytes.HasPrefix(recipient, []byte("@")) {
		broadcastRanged(s.postOffice, client, packet)
		return
	}

	// Wallop
	if string(recipient) == "*S" {
		broadcastAllSupervisors(s.postOffice, client, packet)
		return
	}

	// Server-wide broadcast message
	if string(recipient) == "*" {
		if client.NetworkRating < NetworkRatingSupervisor {
			return
		}
		broadcastAll(s.postOffice, client, packet)
		return
	}

	if string(recipient) == "FP" {
		// TODO: handle FP
		return
	}

	if string(recipient) == "SERVER" {
		// TODO: handle SERVER
		return
	}

	// Otherwise, treat as direct message
	sendDirectOrErr(s.postOffice, client, recipient, packet)
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

	// Update post office position
	s.postOffice.UpdatePosition(client, [2]float64{lat, lon}, visRange)

	// Broadcast position update
	broadcastRanged(s.postOffice, client, packet)

	client.LastUpdated.Store(time.Now())
}

// handlePilotPosition handles logic for 0.2hz `@` pilot position updates
func (s *Server) handlePilotPosition(client *session.Session, packet []byte) {
	lat, lon, ok := parseLatLon(packet, 4, 5)
	if !ok {
		client.SendError(SyntaxError, "Invalid latitude/longitude")
		return
	}

	const pilotVisRange = 50.0 * 1852.0 // 50 nautical miles

	// Update post office position
	s.postOffice.UpdatePosition(client, [2]float64{lat, lon}, pilotVisRange)

	// Broadcast position update
	broadcastRanged(s.postOffice, client, packet)

	// Update state
	client.Transponder.Store(string(getField(packet, 2)))

	groundspeed, _ := strconv.Atoi(string(getField(packet, 7)))
	client.Groundspeed.Store(int32(groundspeed))

	altitude, _ := strconv.Atoi(string(getField(packet, 6)))
	client.Altitude.Store(int32(altitude))

	pbhUint, _ := strconv.ParseUint(string(getField(packet, 8)), 10, 32)
	_, _, heading := pitchBankHeading(uint32(pbhUint))
	client.Heading.Store(int32(heading))

	client.LastUpdated.Store(time.Now())

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
	broadcastRangedVelocity(s.postOffice, client, packet)
}

// handleDelete handles logic for Delete ATC `#DA` and Delete Pilot `#DP` packets
func (s *Server) handleDelete(client *session.Session, packet []byte) {
	// Broadcast delete packet
	broadcastAll(s.postOffice, client, packet)

	// Cancel context. Writer worker will close the connection
	client.Cancel()
}

// handleSquawkbox handles logic for Squawkbox `#SB` packets
func (s *Server) handleSquawkbox(client *session.Session, packet []byte) {
	// Forward packet to recipient
	recipient := getField(packet, 1)
	sendDirectOrErr(s.postOffice, client, recipient, packet)
}

// handleProcontroller handles logic for Pro Controller `#PC` packets
func (s *Server) handleProcontroller(client *session.Session, packet []byte) {
	// ATC-only packet
	if !client.IsAtc {
		return
	}

	recipient := getField(packet, 1)
	if len(recipient) < 2 {
		client.SendError(SyntaxError, "Invalid recipient")
		return
	}
	pcType := getField(packet, 3)

	switch string(pcType) {

	// Unprivileged requests
	case
		"VER", // Version
		"ID",  // Modern client check
		"DI",  // Modern client check response
		"IC", "IK", "IB", "IO",
		"OC", "OK", "OB", "OO",
		"MC", "MK", "MB", "MO": // Landline commands

		sendDirectOrErr(s.postOffice, client, recipient, packet)

	// Privileged requests
	case
		"IH", // I have
		"SC", // Set scratchpad
		"GD", // Set global data
		"TA", // Set temporary altitude
		"FA", // Set final altitude
		"VT", // Set voice type
		"BC", // Set beacon code
		"HC", // Cancel handoff
		"PT", // Pointout
		"DP", // Push to departure list
		"ST": // Set flight strip

		// Only active ATC above OBS
		if client.FacilityType <= 0 {
			client.SendError(InvalidControlError, "Invalid control")
			return
		}
		if recipient[0] == '@' {
			broadcastRangedAtcOnly(s.postOffice, client, packet)
		} else {
			sendDirectOrErr(s.postOffice, client, recipient, packet)
		}
	}
}

func (s *Server) handleClientQuery(client *session.Session, packet []byte) {
	recipient := getField(packet, 1)
	queryType := getField(packet, 2)

	// Handle queries sent to SERVER
	if string(recipient) == "SERVER" {
		switch string(queryType) {
		case "ATC":
			s.handleClientQueryATCRequest(client, packet)
		case "IP":
			s.handleClientQueryIPRequest(client, packet)
		case "FP":
			s.handleClientQueryFlightplanRequest(client, packet)
		}
		return
	}

	switch string(queryType) {

	// Unprivileged ATC queries
	case
		"BY",      // Request relief
		"HI",      // Cancel request relief
		"HLP",     // Request help
		"NOHLP",   // Cancel request help
		"WH",      // Who has
		"NEWATIS", // Broadcast new ATIS letter
		"NEWINFO": // Broadcast new ATIS info

		// ATC only
		if !client.IsAtc {
			client.SendError(InvalidControlError, "Invalid control")
			return
		}
		forwardClientQuery(s.postOffice, client, packet)

	// Privileged ATC queries
	case
		"IT",  // Initiate track
		"DR",  // Drop track
		"HT",  // Accept handoff
		"TA",  // Set temporary altitude
		"FA",  // Set final altitude
		"BC",  // Set beacon code
		"SC",  // Set scratchpad
		"VT",  // Set voice type
		"EST", // Set estimate time
		"GD",  // Set global data
		"IPC": // Force squawk code change

		// ATC above OBS facility only
		if !client.IsAtc || client.FacilityType <= 0 {
			client.SendError(InvalidControlError, "Invalid control")
			return
		}
		forwardClientQuery(s.postOffice, client, packet)

	// Allow aircraft configuration queries from any client
	case "ACC", "CAPS", "C?", "RN", "ATIS", "SV":
		forwardClientQuery(s.postOffice, client, packet)

	// INF queries
	case "INF":
		// Allow responses from any client
		if getPacketType(packet) == PacketTypeClientQueryResponse {
			sendDirectOrErr(s.postOffice, client, recipient, packet)
			return
		}

		// Require >= SUP for interrogations
		if client.NetworkRating < NetworkRatingSupervisor {
			client.SendError(InvalidControlError, "Invalid control")
			return
		}
		forwardClientQuery(s.postOffice, client, packet)
	}
}

func (s *Server) handleClientQueryATCRequest(client *session.Session, packet []byte) {
	if countFields(packet) != 4 {
		client.SendError(SyntaxError, "Invalid ATC request")
		return
	}

	targetCallsign := getField(packet, 3)
	targetClient, err := s.postOffice.Find(string(targetCallsign))
	if err != nil {
		client.SendError(NoSuchCallsignError, "No such callsign")
		return
	}

	var p string
	if targetClient.FacilityType > 0 {
		p = fmt.Sprintf("$CRSERVER:%s:ATC:Y:%s\r\n", client.Callsign, targetCallsign)
	} else {
		p = fmt.Sprintf("$CRSERVER:%s:ATC:N:%s\r\n", client.Callsign, targetCallsign)
	}
	client.Send(p)
}

func (s *Server) handleClientQueryIPRequest(client *session.Session, packet []byte) {
	ip := strings.SplitN(client.Conn.RemoteAddr().String(), ":", 2)[0]
	p := fmt.Sprintf("$CRSERVER:%s:IP:%s\r\n", client.Callsign, ip)
	client.Send(p)
}

func (s *Server) handleClientQueryFlightplanRequest(client *session.Session, packet []byte) {
	if !client.IsAtc {
		return
	}

	if countFields(packet) != 4 {
		client.SendError(SyntaxError, "Invalid flightplan request syntax")
		return
	}

	targetCallsign := string(getField(packet, 3))
	targetClient, err := s.postOffice.Find(targetCallsign)
	if err != nil {
		client.SendError(NoSuchCallsignError, "No such callsign: "+targetCallsign)
		return
	}

	fplInfo := targetClient.FlightPlan.Load()
	if fplInfo == "" {
		return
	}

	beaconCode := targetClient.AssignedBeaconCode.Load()
	if beaconCode == "" {
		beaconCode = "0"
	}

	// Send flightplan packet
	fplPacket := buildFileFlightplanPacket(targetCallsign, "*A", fplInfo)
	client.Send(fplPacket)

	// Send assigned beacon code
	bcPacket := buildBeaconCodePacket("server", client.Callsign, targetCallsign, beaconCode)
	client.Send(bcPacket)

	// TODO: research any other data that should be sent here
}

func (s *Server) handleMetarRequest(client *session.Session, packet []byte) {
	recipient := getField(packet, 1)
	staticField := getField(packet, 2)
	icaoCode := getField(packet, 3)

	if string(recipient) != "SERVER" || string(staticField) != "METAR" {
		return
	}

	s.metarService.Request(client.Ctx, client, string(icaoCode))
}

func (s *Server) handleKillRequest(client *session.Session, packet []byte) {
	if client.NetworkRating < NetworkRatingSupervisor {
		return
	}

	// Attempt to find the victim client
	recipient := getField(packet, 1)
	victim, err := s.postOffice.Find(string(recipient))
	if err != nil {
		client.SendError(NoSuchCallsignError, "No such callsign")
		return
	}

	// Closing the context of the victim client will eventually cause it to disconnect
	victim.Cancel()
}

func (s *Server) handleAuthChallenge(client *session.Session, packet []byte) {
	if client.ClientChallenge == "" {
		client.SendError(UnauthorizedSoftwareError, "Cannot reply to auth challenge since no initial challenge was recieved")
		return
	}

	challenge := getField(packet, 2)
	resp := client.Auth.GetResponseForChallenge(challenge)
	client.Auth.UpdateState(&resp)

	respPacket := strings.Builder{}
	respPacket.WriteString("$ZRSERVER:")
	respPacket.WriteString(client.Callsign)
	respPacket.WriteByte(':')
	respPacket.Write(resp[:])
	respPacket.WriteString("\r\n")

	client.Send(respPacket.String())
}

func (s *Server) handleHandoff(client *session.Session, packet []byte) {
	// Active >OBS ATC only
	if !client.IsAtc || client.FacilityType <= 1 {
		return
	}

	recipient := getField(packet, 1)
	sendDirectOrErr(s.postOffice, client, recipient, packet)
}

func (s *Server) handleFileFlightplan(client *session.Session, packet []byte) {
	fplInfo := extractFlightplanInfoSection(packet)
	client.FlightPlan.Store(fplInfo)

	broadcastPacket := buildFileFlightplanPacket(client.Callsign, "*A", fplInfo)
	broadcastAllATC(s.postOffice, client, []byte(broadcastPacket))
}

func (s *Server) handleAmendFlightplan(client *session.Session, packet []byte) {
	if !client.IsAtc || client.FacilityType <= 0 {
		return
	}

	fplInfo := extractFlightplanInfoSection(packet)

	targetCallsign := string(getField(packet, 2))
	targetClient, err := s.postOffice.Find(targetCallsign)
	if err != nil {
		client.SendError(NoSuchCallsignError, "No such callsign: "+targetCallsign)
		return
	}
	targetClient.FlightPlan.Store(fplInfo)

	broadcastPacket := buildAmendFlightplanPacket(client.Callsign, "*A", targetCallsign, fplInfo)
	broadcastAllATC(s.postOffice, client, []byte(broadcastPacket))
}
