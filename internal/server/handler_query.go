package server

import (
	"fmt"

	"github.com/renorris/openfsd/internal/session"
)

// handleSquawkbox handles logic for Squawkbox `#SB` packets
func (s *Server) handleSquawkbox(client *session.Session, packet []byte) {
	// Forward packet to recipient
	recipient := getField(packet, 1)
	sendDirectOrErr(s.registry, client, recipient, packet)
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

		sendDirectOrErr(s.registry, client, recipient, packet)

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
			broadcastRangedAtcOnly(s.registry, client, packet)
		} else {
			sendDirectOrErr(s.registry, client, recipient, packet)
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
		case "CAPS":
			// $CQ{cs}:SERVER:CAPS → advertise server features.
			// $CR{cs}:SERVER:CAPS:… (client announce) is accepted and ignored.
			if getPacketType(packet) == PacketTypeClientQuery {
				s.handleClientQueryCAPSRequest(client, packet)
			}
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
		forwardClientQuery(s.registry, client, packet)

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
		forwardClientQuery(s.registry, client, packet)

	// Allow aircraft configuration queries from any client
	case "ACC", "CAPS", "C?", "RN", "ATIS", "SV":
		forwardClientQuery(s.registry, client, packet)

	// INF queries
	case "INF":
		// Allow responses from any client
		if getPacketType(packet) == PacketTypeClientQueryResponse {
			sendDirectOrErr(s.registry, client, recipient, packet)
			return
		}

		// Require >= SUP for interrogations
		if client.NetworkRating < NetworkRatingSupervisor {
			client.SendError(InvalidControlError, "Invalid control")
			return
		}
		forwardClientQuery(s.registry, client, packet)
	}
}

func (s *Server) handleClientQueryATCRequest(client *session.Session, packet []byte) {
	if countFields(packet) != 4 {
		client.SendError(SyntaxError, "Invalid ATC request")
		return
	}

	targetCallsign := getField(packet, 3)
	targetClient, err := s.registry.Find(string(targetCallsign))
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

// handleClientQueryCAPSRequest answers $CQ{callsign}:SERVER:CAPS with the
// openfsd server capability set. Clients (notably vatSys) send this immediately
// after login and gate features such as SECPOS / FASTPOS on the reply.
func (s *Server) handleClientQueryCAPSRequest(client *session.Session, _ []byte) {
	p := fmt.Sprintf("$CRSERVER:%s:CAPS:%s\r\n", client.Callsign, serverCapabilitiesWire)
	client.Send(p)
}

func (s *Server) handleClientQueryIPRequest(client *session.Session, packet []byte) {
	ip := client.RemoteIP()
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
	targetClient, err := s.registry.Find(targetCallsign)
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
