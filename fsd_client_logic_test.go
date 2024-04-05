package main

import (
	"bufio"
	"context"
	"github.com/renorris/openfsd/protocol"
	"github.com/stretchr/testify/assert"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type clientStruct struct {
	callsign         string
	cid              int
	password         string
	clientID         uint16
	clientName       string
	majorVersion     int
	minorVersion     int
	sysUID           int
	initialChallenge string
	networkRating    int
	protocolRevsion  int
	simulatorType    int
	realName         string

	preliminaryTestPackets []protocol.PDU // Packets to send after logging in, but before the next client logs in
	preliminaryWantPackets []protocol.PDU // Expected packets to receive before the next client logs in
	testPackets            []protocol.PDU // Packets to send in normal post-login state
	wantPackets            []protocol.PDU // Expected packets to receive
}

// TestFSDClientLogic focuses on post-login logic
func TestFSDClientLogic(t *testing.T) {
	SC = &ServerConfig{
		FsdListenAddr:  "localhost:6809",
		HttpListenAddr: "localhost:9086",
		HttpsEnabled:   false,
		DatabaseFile:   "./test.db",
		MOTD:           "openfsd",
	}

	// Delete any existing database file so a new one is created
	os.Remove(SC.DatabaseFile)
	defer os.Remove(SC.DatabaseFile)

	// Run configuration helpers and start the FSD server
	configureJwt()
	configurePostOffice()
	configureProtocolValidator()
	configureDatabase()

	// Add test users
	addUserToDatabase(t, 1000000, "12345", protocol.NetworkRatingOBS)
	addUserToDatabase(t, 1000001, "12345", protocol.NetworkRatingOBS)
	addUserToDatabase(t, 1000002, "12345", protocol.NetworkRatingSUP)

	// Start FSD server
	fsdCtx, cancelFsd := context.WithCancel(context.Background())
	go StartFSDServer(fsdCtx)
	defer cancelFsd()

	// Start http server
	httpCtx, cancelHttp := context.WithCancel(context.Background())
	go StartHttpServer(httpCtx)
	defer cancelHttp()
	time.Sleep(50 * time.Millisecond)

	tests := []struct {
		testName string
		clients  []clientStruct
	}{
		{
			testName: "Ping ($PI) request",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.PingPDU{
							From:      "N123",
							To:        protocol.ServerCallsign,
							Timestamp: "1234567890",
						},
					},
					wantPackets: []protocol.PDU{
						&protocol.PongPDU{
							From:      protocol.ServerCallsign,
							To:        "N123",
							Timestamp: "1234567890",
						},
					},
				},
			},
		},
		{
			testName: "IP request ($CQCLIENT:SERVER:IP)",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.ClientQueryPDU{
							From:      "N123",
							To:        protocol.ServerCallsign,
							QueryType: protocol.ClientQueryPublicIP,
							Payload:   "",
						},
					},
					wantPackets: []protocol.PDU{
						&protocol.ClientQueryResponsePDU{
							From:      protocol.ServerCallsign,
							To:        "N123",
							QueryType: protocol.ClientQueryPublicIP,
							Payload:   "127.0.0.1",
						},
					},
				},
			},
		},
		{
			testName: "Fast pilot position broadcast",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N123",
							SquawkCode:       "1200",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
						&protocol.PingPDU{
							From:      "N123",
							To:        protocol.ServerCallsign,
							Timestamp: "1234567890",
						},
					},
					preliminaryWantPackets: []protocol.PDU{
						&protocol.PongPDU{
							From:      protocol.ServerCallsign,
							To:        "N123",
							Timestamp: "1234567890",
						},
					},
					testPackets: []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "N124",
							To:               protocol.ServerCallsign,
							CID:              1000001,
							Token:            "",
							NetworkRating:    1,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1201",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
						&protocol.FastPilotPositionPDU{
							Type:         protocol.FastPilotPositionTypeFast,
							From:         "N124",
							Lat:          45,
							Lng:          45,
							AltitudeTrue: 10,
							AltitudeAgl:  10,
							Pitch:        0,
							Heading:      0,
							Bank:         0,
							PositionalVelocityVector: protocol.VelocityVector{
								X: 10,
								Y: 10,
								Z: 10,
							},
							RotationalVelocityVector: protocol.VelocityVector{
								X: 10,
								Y: 10,
								Z: 10,
							},
							NoseGearAngle: 15,
						},
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1201",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
					},
				},
				{
					callsign:         "N124",
					cid:              1000001,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Foo Bar",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1201",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
						&protocol.FastPilotPositionPDU{
							Type:         protocol.FastPilotPositionTypeFast,
							From:         "N124",
							Lat:          45,
							Lng:          45,
							AltitudeTrue: 10,
							AltitudeAgl:  10,
							Pitch:        0,
							Heading:      0,
							Bank:         0,
							PositionalVelocityVector: protocol.VelocityVector{
								X: 10,
								Y: 10,
								Z: 10,
							},
							RotationalVelocityVector: protocol.VelocityVector{
								X: 10,
								Y: 10,
								Z: 10,
							},
							NoseGearAngle: 15,
						},
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1201",
							NetworkRating:    1,
							Lat:              -45.0,
							Lng:              -45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
						&protocol.FastPilotPositionPDU{
							Type:         protocol.FastPilotPositionTypeFast,
							From:         "N124",
							Lat:          -45,
							Lng:          -45,
							AltitudeTrue: 10,
							AltitudeAgl:  10,
							Pitch:        0,
							Heading:      0,
							Bank:         0,
							PositionalVelocityVector: protocol.VelocityVector{
								X: 10,
								Y: 10,
								Z: 10,
							},
							RotationalVelocityVector: protocol.VelocityVector{
								X: 10,
								Y: 10,
								Z: 10,
							},
							NoseGearAngle: 15,
						},
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1201",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
		{
			testName: "Pilot position broadcast",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N123",
							SquawkCode:       "1200",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
					},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets:            []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "N124",
							To:               protocol.ServerCallsign,
							CID:              1000001,
							Token:            "",
							NetworkRating:    1,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1200",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
					},
				},
				{
					callsign:         "N124",
					cid:              1000001,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Foo Bar",

					preliminaryTestPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.PilotPositionPDU{
							SquawkingModeC:   false,
							Identing:         false,
							From:             "N124",
							SquawkCode:       "1200",
							NetworkRating:    1,
							Lat:              45.0,
							Lng:              45.0,
							TrueAltitude:     50,
							PressureAltitude: 50,
							GroundSpeed:      0,
							Pitch:            0,
							Heading:          0,
							Bank:             0,
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
		{
			testName: "Client Query Real Name ($CQN123:N124:RN)",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets:            []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "N124",
							To:               protocol.ServerCallsign,
							CID:              1000001,
							Token:            "",
							NetworkRating:    1,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.ClientQueryPDU{
							From:      "N124",
							To:        "N123",
							QueryType: protocol.ClientQueryRealName,
							Payload:   "",
						},
					},
				},
				{
					callsign:         "N124",
					cid:              1000001,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Foo Bar",

					preliminaryTestPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.ClientQueryPDU{
							From:      "N124",
							To:        "N123",
							QueryType: protocol.ClientQueryRealName,
							Payload:   "",
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
		{
			testName: "Client Query Real Name Response ($CRN124:N123:RN:Foo Bar)",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					testPackets:            []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "N124",
							To:               protocol.ServerCallsign,
							CID:              1000001,
							Token:            "",
							NetworkRating:    1,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.ClientQueryResponsePDU{
							From:      "N124",
							To:        "N123",
							QueryType: protocol.ClientQueryRealName,
							Payload:   "Foo Bar",
						},
					},
				},
				{
					callsign:         "N124",
					cid:              1000001,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Foo Bar",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.ClientQueryResponsePDU{
							From:      "N124",
							To:        "N123",
							QueryType: protocol.ClientQueryRealName,
							Payload:   "Foo Bar",
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
		{
			testName: "Authentication challenge ($ZC)",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "30984979d8caed23",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.AuthChallengePDU{
							From:      "N123",
							To:        protocol.ServerCallsign,
							Challenge: "de6acb8e",
						},
					},
					wantPackets: []protocol.PDU{
						&protocol.AuthChallengeResponsePDU{
							From:              protocol.ServerCallsign,
							To:                "N123",
							ChallengeResponse: "f8ee97157f66455ed6108fccef6ccf5f",
						},
					},
				},
			},
		},
		{
			testName: "Kill ($!!)",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets:            []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "SUP",
							To:               protocol.ServerCallsign,
							CID:              1000002,
							Token:            "",
							NetworkRating:    protocol.NetworkRatingSUP,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Supervisor",
						},
						&protocol.KillRequestPDU{
							From:   "SUP",
							To:     "N123",
							Reason: "ur banned",
						},
					},
				},
				{
					callsign:         "SUP",
					cid:              1000002,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    protocol.NetworkRatingSUP,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Supervisor",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.KillRequestPDU{
							From:   "SUP",
							To:     "N123",
							Reason: "ur banned",
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
		{
			testName: "Kill not allowed",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets:            []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "N124",
							To:               protocol.ServerCallsign,
							CID:              1000001,
							Token:            "",
							NetworkRating:    protocol.NetworkRatingOBS,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.TextMessagePDU{
							From:    "N124",
							To:      "N123",
							Message: "hello",
						},
					},
				},
				{
					callsign:         "N124",
					cid:              1000001,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    protocol.NetworkRatingOBS,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Foo Bar",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.KillRequestPDU{
							From:   "N124",
							To:     "N123",
							Reason: "ur banned",
						},
						&protocol.TextMessagePDU{
							From:    "N124",
							To:      "N123",
							Message: "hello",
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
		{
			testName: "Delete pilot broadcast",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              1000000,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    1,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "John Doe",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets:            []protocol.PDU{},
					wantPackets: []protocol.PDU{
						&protocol.AddPilotPDU{
							From:             "N124",
							To:               protocol.ServerCallsign,
							CID:              1000001,
							Token:            "",
							NetworkRating:    protocol.NetworkRatingOBS,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.DeletePilotPDU{
							From: "N124",
							CID:  1000001,
						},
					},
				},
				{
					callsign:         "N124",
					cid:              1000001,
					password:         "12345",
					clientID:         35044,
					clientName:       "vPilot",
					majorVersion:     3,
					minorVersion:     8,
					sysUID:           -99999,
					initialChallenge: "abcdef",
					networkRating:    protocol.NetworkRatingOBS,
					protocolRevsion:  protocol.ProtoRevisionVatsim2022,
					simulatorType:    2,
					realName:         "Foo Bar",

					preliminaryTestPackets: []protocol.PDU{},
					preliminaryWantPackets: []protocol.PDU{},
					testPackets: []protocol.PDU{
						&protocol.DeletePilotPDU{
							From: "N124",
							CID:  1000001,
						},
					},
					wantPackets: []protocol.PDU{},
				},
			},
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.testName, func(t *testing.T) {
			var doneWg sync.WaitGroup
			// Spawn each client
			for _, client := range tc.clients {
				c := client
				doneWg.Add(1)

				var loggedIn sync.WaitGroup
				loggedIn.Add(1)

				go func() {
					defer doneWg.Done()

					// Log in the client.
					// Test cases are meant to be executed after the client has logged in.
					// Get a JWT token first
					jwtResponse := doJwtRequest(t, "http://localhost:9086/api/fsd-jwt", c.cid, c.password)

					conn, err := net.Dial("tcp4", SC.FsdListenAddr)
					assert.Nil(t, err)

					err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
					assert.Nil(t, err)
					defer func() {
						closeErr := conn.Close()
						assert.Nil(t, closeErr)
					}()

					reader := bufio.NewReader(conn)
					serverIdent, err := reader.ReadString('\n')
					assert.Nil(t, err)
					assert.NotEmpty(t, serverIdent)
					assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

					clientIdentPDU := protocol.ClientIdentificationPDU{
						From:             c.callsign,
						To:               protocol.ServerCallsign,
						ClientID:         c.clientID,
						ClientName:       c.clientName,
						MajorVersion:     c.majorVersion,
						MinorVersion:     c.minorVersion,
						CID:              c.cid,
						SysUID:           c.sysUID,
						InitialChallenge: c.initialChallenge,
					}

					addPilotPDU := protocol.AddPilotPDU{
						From:             c.callsign,
						To:               protocol.ServerCallsign,
						CID:              c.cid,
						Token:            jwtResponse.Token,
						NetworkRating:    c.networkRating,
						ProtocolRevision: c.protocolRevsion,
						SimulatorType:    c.simulatorType,
						RealName:         c.realName,
					}

					_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
					assert.Nil(t, err)
					_, err = conn.Write([]byte(addPilotPDU.Serialize()))
					assert.Nil(t, err)

					motdMsg, err := reader.ReadString('\n')
					assert.Nil(t, err)
					assert.NotEmpty(t, motdMsg)

					motdReceivedPDU, err := protocol.ParseTextMessagePDU(motdMsg)
					assert.Nil(t, err)

					expectedMOTD := protocol.TextMessagePDU{
						From:    protocol.ServerCallsign,
						To:      c.callsign,
						Message: SC.MOTD,
					}

					assert.Equal(t, expectedMOTD.Serialize(), motdReceivedPDU.Serialize())

					// Send post-login packets before signalling that we're done logging in
					for _, packet := range c.preliminaryTestPackets {
						_, writeErr := conn.Write([]byte(packet.Serialize()))
						assert.Nil(t, writeErr)
					}

					// Verify post-login returned packets are correct
					for _, packet := range c.preliminaryWantPackets {
						deadlineErr := conn.SetReadDeadline(time.Now().Add(2 * time.Second))
						assert.Nil(t, deadlineErr)

						recvPacket, recvErr := reader.ReadString('\n')
						assert.Nil(t, recvErr)

						assert.Equal(t, packet.Serialize(), recvPacket)
					}

					// Signal we're done logging in
					loggedIn.Done()

					// Send test packets
					for _, packet := range c.testPackets {
						_, writeErr := conn.Write([]byte(packet.Serialize()))
						assert.Nil(t, writeErr)
					}

					// Verify returned packets are correct
					for _, packet := range c.wantPackets {
						deadlineErr := conn.SetReadDeadline(time.Now().Add(2 * time.Second))
						assert.Nil(t, deadlineErr)

						recvPacket, recvErr := reader.ReadString('\n')
						assert.Nil(t, recvErr)

						assert.Equal(t, packet.Serialize(), recvPacket)
					}
				}()

				// Wait for the preceding client to finish logging in before spawning another
				loggedIn.Wait()
			}

			// Wait for all goroutines to return
			doneWg.Wait()
		})
	}
}
