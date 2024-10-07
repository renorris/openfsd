package test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/bootstrap"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"github.com/stretchr/testify/assert"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
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

	preliminaryTestPackets []protocol.PDU // Packets to send after logging in, but before the next browser logs in
	preliminaryWantPackets []protocol.PDU // Expected packets to receive before the next browser logs in
	testPackets            []protocol.PDU // Packets to send in normal post-login state
	wantPackets            []protocol.PDU // Expected packets to receive
}

// TestFSDClientLogic focuses on post-login logic
func TestFSDClientLogic(t *testing.T) {
	if err := os.Setenv("IN_MEMORY_DB", "true"); err != nil {
		t.Fatal(err)
	}

	// Start the server
	ctx, cancelCtx := context.WithCancel(context.Background())
	b := bootstrap.NewDefaultBootstrap()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Add test user
	user1 := database.FSDUserRecord{
		Email:         "example@mail.com",
		FirstName:     "Test User 1",
		LastName:      "Test User 1 Lastname",
		Password:      "54321",
		FSDPassword:   "12345",
		NetworkRating: protocol.NetworkRatingOBS,
		PilotRating:   0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	var err error
	user1.CID, err = user1.Insert(servercontext.DB())
	assert.Nil(t, err)

	user2 := database.FSDUserRecord{
		Email:         "example@mail.com",
		FirstName:     "Test User 2",
		LastName:      "Test User 2 Lastname",
		Password:      "54321",
		FSDPassword:   "12345",
		NetworkRating: protocol.NetworkRatingOBS,
		PilotRating:   0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	user2.CID, err = user2.Insert(servercontext.DB())
	assert.Nil(t, err)

	user3 := database.FSDUserRecord{
		Email:         "example@mail.com",
		FirstName:     "Test User 3",
		LastName:      "Test User 3 Lastname",
		Password:      "54321",
		FSDPassword:   "12345",
		NetworkRating: protocol.NetworkRatingSUP,
		PilotRating:   0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	user3.CID, err = user3.Insert(servercontext.DB())
	assert.Nil(t, err)

	tests := []struct {
		testName string
		clients  []clientStruct
	}{
		{
			testName: "Ping ($PI) request",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              user1.CID,
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
					cid:              user1.CID,
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
					cid:              user1.CID,
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
							CID:              user2.CID,
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
					cid:              user2.CID,
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
					cid:              user1.CID,
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
							CID:              user2.CID,
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
					cid:              user2.CID,
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
					cid:              user1.CID,
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
							CID:              user2.CID,
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
					cid:              user2.CID,
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
					cid:              user1.CID,
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
							CID:              user2.CID,
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
					cid:              user2.CID,
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
					cid:              user2.CID,
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
			testName: "kill ($!!)",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              user1.CID,
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
							CID:              user3.CID,
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
					cid:              user3.CID,
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
			testName: "kill not allowed",
			clients: []clientStruct{
				{
					callsign:         "N123",
					cid:              user1.CID,
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
							CID:              user2.CID,
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
					cid:              user2.CID,
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
					callsign:         "DEL",
					cid:              user1.CID,
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
							CID:              user2.CID,
							Token:            "",
							NetworkRating:    protocol.NetworkRatingOBS,
							ProtocolRevision: protocol.ProtoRevisionVatsim2022,
							SimulatorType:    2,
							RealName:         "Foo Bar",
						},
						&protocol.DeletePilotPDU{
							From: "N124",
							CID:  user2.CID,
						},
					},
				},
				{
					callsign:         "N124",
					cid:              user2.CID,
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
							CID:  user2.CID,
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
			// Spawn each browser
			for _, client := range tc.clients {
				c := client
				doneWg.Add(1)

				var loggedIn sync.WaitGroup
				loggedIn.Add(1)

				go func() {
					defer doneWg.Done()

					// Log in the browser.
					// Test cases are meant to be executed after the browser has logged in.
					// Load a JWT token first
					var token string
					var err error
					token, err = getJWTToken(c.cid, c.password)
					assert.Nil(t, err)

					conn, err := net.Dial("tcp4", servercontext.Config().FSDListenAddress)
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
						Token:            token,
						NetworkRating:    protocol.NetworkRating(c.networkRating),
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

					motdReceivedPDU := protocol.TextMessagePDU{}
					err = motdReceivedPDU.Parse(motdMsg)
					assert.Nil(t, err)

					expectedMOTD := protocol.TextMessagePDU{
						From:    protocol.ServerCallsign,
						To:      c.callsign,
						Message: servercontext.Config().MOTD,
					}

					assert.Equal(t, expectedMOTD.Serialize(), motdReceivedPDU.Serialize())

					// Send post-login packets before signalling that we're done logging in
					for _, packet := range c.preliminaryTestPackets {
						_, writeErr := conn.Write([]byte(packet.Serialize()))
						assert.Nil(t, writeErr)
					}

					// Verify post-login returned packets are correct
					for _, packet := range c.preliminaryWantPackets {
						deadlineErr := conn.SetReadDeadline(time.Now().Add(30 * time.Second))
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
						deadlineErr := conn.SetReadDeadline(time.Now().Add(30 * time.Second))
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

	// Close context
	cancelCtx()

	// Wait for bootstrap done
	<-b.Done
}

func getJWTToken(cid int, password string) (token string, err error) {
	reqPayload := auth.FSDJWTRequest{
		CID:      strconv.Itoa(cid),
		Password: password,
	}

	var reqPayloadBytes []byte
	if reqPayloadBytes, err = json.Marshal(reqPayload); err != nil {
		return
	}

	var addr *net.TCPAddr
	if addr, err = net.ResolveTCPAddr("tcp4", servercontext.Config().HTTPListenAddress); err != nil {
		return
	}

	var resp *http.Response
	if resp, err = http.Post("http://localhost:"+strconv.Itoa(addr.Port)+"/api/v1/fsd-jwt", "application/json", bytes.NewReader(reqPayloadBytes)); err != nil {
		return
	}

	if resp.StatusCode != 200 {
		err = errors.New("HTTP status " + resp.Status)
		return
	}

	var respBody []byte
	if respBody, err = io.ReadAll(resp.Body); err != nil {
		return
	}

	var respPayload auth.FSDJWTResponse
	if err = json.Unmarshal(respBody, &respPayload); err != nil {
		return
	}

	if !respPayload.Success {
		err = errors.New("response payload unsuccessful " + respPayload.ErrorMsg)
		return
	}

	return respPayload.Token, nil
}
