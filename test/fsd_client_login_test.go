package test

import (
	"bufio"
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/bootstrap"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"github.com/stretchr/testify/assert"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFSDClientLogin(t *testing.T) {
	// Setup config for testing environment
	if err := os.Setenv("IN_MEMORY_DB", "true"); err != nil {
		t.Fatal(err)
	}

	// Start the server
	ctx, cancelCtx := context.WithCancel(context.Background())
	b := bootstrap.NewDefaultBootstrap()
	err := b.Start(ctx)
	assert.Nil(t, err)

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

	user1.CID, err = user1.Insert(servercontext.DB())
	assert.Nil(t, err)

	// Simulate a jwt login token request
	var token string
	token, err = getJWTToken(user1.CID, user1.FSDPassword)
	assert.Nil(t, err)

	// Test successful FSD login process
	{
		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            token,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU := protocol.TextMessagePDU{
			From:    protocol.ServerCallsign,
			To:      "N123",
			Message: strings.Split(servercontext.Config().MOTD, "\n")[0],
		}

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Network rating too high
	{
		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            token,
			NetworkRating:    2,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		assert.Equal(t, protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError, "1", "try again at or below your assigned rating").Serialize(), responseMsg)
		conn.Close()
	}

	// Invalid protocol revision
	{
		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            token,
			NetworkRating:    1,
			ProtocolRevision: 100,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		assert.Equal(t, protocol.NewGenericFSDError(protocol.InvalidProtocolRevisionError, "100", "please connect with a client that supports the Vatsim2022 (101) protocol revision").Serialize(), responseMsg)
		conn.Close()
	}

	// Test callsign already in use
	{
		conn1, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)
		conn2, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)
		err = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn1)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		reader2 := bufio.NewReader(conn2)
		serverIdent2, err := reader2.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent2)
		assert.True(t, strings.HasPrefix(serverIdent2, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            token,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn1.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn1.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg1, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		time.Sleep(50 * time.Millisecond)

		_, err = conn2.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn2.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg2, err := reader2.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU1 := protocol.TextMessagePDU{
			From:    protocol.ServerCallsign,
			To:      "N123",
			Message: strings.Split(servercontext.Config().MOTD, "\n")[0],
		}

		assert.Equal(t, expectedPDU1.Serialize(), responseMsg1)
		assert.Equal(t, protocol.NewGenericFSDError(protocol.CallsignInUseError, "N123", "").Serialize(), responseMsg2)

		conn1.Close()
		conn2.Close()
	}

	// Test packet oversize (> 1024bytes)
	{
		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		lotsOfNines := make([]byte, 512)
		for i := 0; i < 512; i++ {
			lotsOfNines[i] = '9'
		}

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: string(lotsOfNines), // <-- Make this field 4096 bytes long
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)

		_, err = reader.ReadString('\n')
		assert.Nil(t, err)
		var fsdError *protocol.FSDError
		if errors.As(err, &fsdError) {
			assert.True(t, strings.Contains(fsdError.Serialize(), "validation error"))
		}

		_, err = reader.ReadString('\n')
		assert.NotNil(t, err)
		assert.ErrorIs(t, err, io.EOF)
	}

	// Test incorrect packet sequence
	{
		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            token,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU := protocol.NewGenericFSDError(protocol.SyntaxError, "", "invalid parameter count")

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test invalid CID
	{
		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              9999999,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              9999999,
			Token:            token,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token claims (CID)")

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test token with invalid signature
	{
		// Fake jwt
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.FSDJWTCustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "openfsd",
				Subject:   strconv.Itoa(user1.CID),
				Audience:  []string{"fsd-live"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(420 * time.Second)),
				NotBefore: jwt.NewNumericDate(time.Now().Add(-120 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ID:        "bogus",
			},
			ControllerRating: 0,
			PilotRating:      0,
		})

		tokenString, err := token.SignedString([]byte("garbage key"))
		assert.Nil(t, err)

		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            tokenString,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token")

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test expired token
	{
		// Expired jwt
		tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.FSDJWTCustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "openfsd",
				Subject:   strconv.Itoa(user1.CID),
				Audience:  []string{"server-live"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-60 * time.Second)),
				NotBefore: jwt.NewNumericDate(time.Now().Add(-120 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ID:        "id",
			},
			ControllerRating: 0,
			PilotRating:      0,
		}).SignedString(servercontext.JWTKey())

		assert.Nil(t, err)

		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            tokenStr,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token")

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test token with invalid CID
	{
		// jwt with invalid CID
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.FSDJWTCustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "openfsd",
				Subject:   "9999999",
				Audience:  []string{"server-live"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(420 * time.Second)),
				NotBefore: jwt.NewNumericDate(time.Now().Add(-120 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ID:        "id",
			},
			ControllerRating: 0,
			PilotRating:      0,
		})

		tokenString, err := token.SignedString(servercontext.JWTKey())
		assert.Nil(t, err)

		conn, err := net.Dial("tcp", "localhost:6809")
		assert.Nil(t, err)

		err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		assert.Nil(t, err)

		reader := bufio.NewReader(conn)
		serverIdent, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)
		assert.True(t, strings.HasPrefix(serverIdent, "$DISERVER:CLIENT:"))

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              user1.CID,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              user1.CID,
			Token:            tokenString,
			NetworkRating:    1,
			ProtocolRevision: 101,
			SimulatorType:    2,
			RealName:         "real name",
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)
		_, err = conn.Write([]byte(addPilotPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		assert.NotEmpty(t, serverIdent)

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token claims (CID)")

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// cancel context
	cancelCtx()

	// wait until done
	<-b.Done
}
