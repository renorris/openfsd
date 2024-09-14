package main

import (
	"bufio"
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/protocol"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func addUserToDatabase(t *testing.T, cid int, password string, rating int) {
	bcryptBytes, err := bcrypt.GenerateFromPassword([]byte(password), 4)
	assert.Nil(t, err)
	passwordHash := string(bcryptBytes)

	_, err = AddUserRecord(DB, cid, passwordHash, rating, "Test User")
	assert.Nil(t, err)
}

func TestFSDClientLogin(t *testing.T) {
	// Setup config for testing environment
	SC = &ServerConfig{
		FsdListenAddr:      "localhost:6809",
		HttpListenAddr:     "localhost:9086",
		HttpsEnabled:       false,
		DatabaseFile:       "./test.db",
		MOTD:               "motd line 1\nmotd line 2",
		PlaintextPasswords: false,
	}

	// Delete any existing database file so a new one is created
	os.Remove(SC.DatabaseFile)
	defer os.Remove(SC.DatabaseFile)

	// Run configuration helpers
	configureDatabase()
	configureJwt()
	configurePostOffice()
	configureProtocolValidator()

	// Start FSD listener
	fsdCtx, cancelFsd := context.WithCancel(context.Background())
	defer cancelFsd()
	go StartFSDServer(fsdCtx)

	// Start http server
	httpCtx, cancelHttp := context.WithCancel(context.Background())
	defer cancelHttp()
	go StartHttpServer(httpCtx)
	time.Sleep(50 * time.Millisecond)

	addUserToDatabase(t, 1000000, "12345", 1)

	// Simulate a jwt login token request
	jwtResponse := doJwtRequest(t, "http://localhost:9086/api/fsd-jwt", 1000000, "12345")
	assert.True(t, jwtResponse.Success)
	assert.NotEmpty(t, jwtResponse.Token)
	assert.Empty(t, jwtResponse.ErrorMsg)

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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
			Token:            jwtResponse.Token,
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
			Message: strings.Split(SC.MOTD, "\n")[0],
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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
			Token:            jwtResponse.Token,
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

		assert.Equal(t, protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError).Serialize(), responseMsg)
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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
			Token:            jwtResponse.Token,
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

		assert.Equal(t, protocol.NewGenericFSDError(protocol.InvalidProtocolRevisionError).Serialize(), responseMsg)
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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
			Token:            jwtResponse.Token,
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
			Message: strings.Split(SC.MOTD, "\n")[0],
		}

		assert.Equal(t, expectedPDU1.Serialize(), responseMsg1)
		assert.Equal(t, protocol.NewGenericFSDError(protocol.CallsignInUseError).Serialize(), responseMsg2)

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

		lotsOfNines := make([]byte, 4096)
		for i := 0; i < 4096; i++ {
			lotsOfNines[i] = '9'
		}

		clientIdentPDU := protocol.ClientIdentificationPDU{
			From:             "N123",
			To:               "SERVER",
			ClientID:         35044,
			ClientName:       "vPilot",
			MajorVersion:     3,
			MinorVersion:     8,
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: string(lotsOfNines), // <-- Make this field 4096 bytes long
		}

		_, err = conn.Write([]byte(clientIdentPDU.Serialize()))
		assert.Nil(t, err)

		responseMsg, err := reader.ReadString('\n')
		assert.Nil(t, err)
		expectedPDU := protocol.NewGenericFSDError(protocol.SyntaxError)
		assert.Equal(t, expectedPDU.Serialize(), responseMsg)

		_, err = reader.ReadString('\n')
		assert.NotNil(t, err)
		var opError *net.OpError
		if errors.As(err, &opError) {
			assert.ErrorIs(t, opError.Err, syscall.ECONNRESET)
		} else {
			assert.Fail(t, "wrong error")
		}
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
			CID:              1000000,
			Token:            jwtResponse.Token,
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

		expectedPDU := protocol.NewGenericFSDError(protocol.SyntaxError)

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
			Token:            jwtResponse.Token,
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

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError)

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test token with invalid signature
	{
		// Fake jwt
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "https://auth.vatsim.net/api/fsd-jwt",
				Subject:   "1000000",
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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
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

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError)

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test expired token
	{
		// Expired jwt
		tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "https://auth.vatsim.net/api/fsd-jwt",
				Subject:   "1000000",
				Audience:  []string{"fsd-live"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-60 * time.Second)),
				NotBefore: jwt.NewNumericDate(time.Now().Add(-120 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ID:        "id",
			},
			ControllerRating: 0,
			PilotRating:      0,
		}).SignedString(JWTKey)

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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
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

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError)

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}

	// Test token with invalid CID
	{
		// jwt with invalid CID
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "https://auth.vatsim.net/api/fsd-jwt",
				Subject:   "9999999",
				Audience:  []string{"fsd-live"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(420 * time.Second)),
				NotBefore: jwt.NewNumericDate(time.Now().Add(-120 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ID:        "id",
			},
			ControllerRating: 0,
			PilotRating:      0,
		})

		tokenString, err := token.SignedString(JWTKey)
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
			CID:              1000000,
			SysUID:           -99999,
			InitialChallenge: "0123456789abcdef",
		}

		addPilotPDU := protocol.AddPilotPDU{
			From:             "N123",
			To:               "SERVER",
			CID:              1000000,
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

		expectedPDU := protocol.NewGenericFSDError(protocol.InvalidLogonError)

		assert.Equal(t, expectedPDU.Serialize(), responseMsg)
		conn.Close()
	}
}
