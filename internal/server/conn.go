package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// sendError writes an FSD error packet to an io.Writer (login-phase only).
// It returns an error if writing to the connection fails.
//
// This function must only be used during the login phase,
// as it synchronously writes the error directly to the
// connection socket. Post-login code must use session.Session.SendError.
func sendError(conn io.Writer, code int, message string) (err error) {
	return protocol.WriteError(conn, protocol.ErrorCode(code), message)
}

// handleConn manages a single client connection.
// If any errors occur during the process, it sends an error to the client and closes the connection.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		if err := recover(); err != nil {
			s.logger.Error("FSD connection goroutine panicked", "err", err)
		}
	}()

	defer conn.Close()

	if err := sendServerIdent(conn); err != nil {
		s.logger.Debug("error sending server ident", "err", err)
		return
	}

	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 4096)
	scanner.Buffer(buf, len(buf))

	data, token, err := readLoginPackets(conn, scanner, s.clock)
	if err != nil {
		return
	}

	// Check if the requested callsign is OK
	if !isValidClientCallsign([]byte(data.Callsign)) {
		sendError(conn, CallsignInvalidError, "Callsign invalid")
		return
	}

	client := session.New(ctx, conn, scanner, data)
	client.Auth = &auth.AuthState{}

	// Attempt to authenticate connection (login-phase errors still use sendError on conn)
	if err = s.attemptAuthentication(client, token); err != nil {
		return
	}

	// Attempt to register to registry
	if err = s.registry.Register(client); err != nil {
		if errors.Is(err, ErrCallsignInUse) {
			sendError(conn, CallsignInUseError, "Callsign already in use")
		}
		return
	}
	defer s.registry.Release(client)

	// Start sender before any post-login outbound traffic (MOTD, etc.).
	// After this point, all writes go through client.Send → SenderWorker.
	// Direct conn.Write is forbidden outside SenderWorker.
	go client.SenderWorker()

	// Send hello message to client
	if err = s.sendMotd(client); err != nil {
		client.Cancel()
		return
	}

	// Broadcast add packet to entire server
	s.broadcastAddPacket(client)
	defer s.broadcastDisconnectPacket(client)

	s.eventLoop(client)
}

// eventLoop reads packets from the session and dispatches handlers.
// SenderWorker must already be running before eventLoop is entered.
func (s *Server) eventLoop(client *session.Session) {
	defer client.Cancel()

	for {
		if !client.Scanner.Scan() {
			return
		}

		// Reference the next packet
		packet := client.Scanner.Bytes()
		packet = append(packet, '\r', '\n') // Re-append delimiter

		// Verify packet and obtain type
		packetType, ok := verifyPacket(packet, client)
		if !ok {
			continue
		}

		// Run handler
		handler := s.getHandler(packetType)
		handler(client, packet)
	}
}

// sendServerIdent sends the initial server identification packet to the client.
// It returns an error if writing to the connection fails.
func sendServerIdent(conn io.Writer) (err error) {
	packet := protocol.ServerIdent{
		Version:      "openfsd",
		ChallengeKey: "6f70656e667364",
	}.Marshal()
	_, err = conn.Write(packet)
	return
}

// ErrInvalidAddPacket is returned when the add packet from the client is invalid.
var ErrInvalidAddPacket = errors.New("invalid add packet")

// ErrInvalidIDPacket is returned when the ID packet from the client is invalid.
var ErrInvalidIDPacket = errors.New("invalid ID packet")

// readLoginPackets reads the two expected login packets from the client:
// the client identification packet and the add packet.
// It parses these packets to extract the client's data and returns it in a LoginData struct.
// If any errors occur during reading or parsing, it sends an error to the client and returns an error.
func readLoginPackets(conn net.Conn, scanner *bufio.Scanner, clock Clock) (data session.LoginData, token string, err error) {
	// Client ident
	if !scanner.Scan() {
		err = ErrInvalidIDPacket
		sendError(conn, SyntaxError, "Error reading Client ident packet")
		return
	}
	idPacket := append([]byte{}, scanner.Bytes()...)

	// Add packet
	if !scanner.Scan() {
		err = ErrInvalidAddPacket
		sendError(conn, SyntaxError, "Error reading add packet")
		return
	}
	addPacket := append([]byte{}, scanner.Bytes()...)

	// Check if the client sent a challenge field
	if countFields(idPacket) == 9 {
		// Extract the challenge
		data.ClientChallenge = string(getField(idPacket, 8))

		// Extract the client ID
		var clientId uint64
		clientId, err = strconv.ParseUint(string(getField(idPacket, 2)), 16, 16)
		if err != nil {
			err = ErrInvalidIDPacket
			sendError(conn, SyntaxError, "Error parsing client ID")
			return
		}
		data.ClientID = uint16(clientId)
	}

	if len(addPacket) < 16 {
		err = ErrInvalidAddPacket
		sendError(conn, SyntaxError, "Invalid add packet")
		return
	}

	// Determine client type
	var prefix string
	switch string(addPacket[:3]) {
	case "#AA":
		data.IsAtc = true
		prefix = "#AA"
	case "#AP":
		prefix = "#AP"
	default:
		err = ErrInvalidAddPacket
		sendError(conn, SyntaxError, "Invalid add packet prefix")
		return
	}

	if data.IsAtc {
		if countFields(addPacket) != 7 {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid number of fields in ATC add packet")
			return
		}
	} else {
		if countFields(addPacket) != 8 {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid number of fields in pilot add packet")
			return
		}
	}

	if callsign, found := bytes.CutPrefix(getField(addPacket, 0), []byte(prefix)); found {
		data.Callsign = string(callsign)
	} else {
		sendError(conn, SyntaxError, "Invalid callsign in add packet")
		err = ErrInvalidAddPacket
		return
	}

	if data.IsAtc {
		data.RealName = string(getField(addPacket, 2))
		if data.CID, err = strconv.Atoi(string(getField(addPacket, 3))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid CID in ATC add packet")
			return
		}
		token = string(getField(addPacket, 4))
		var networkRating int
		if networkRating, err = strconv.Atoi(string(getField(addPacket, 5))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid network rating in pilot add packet")
			return
		}
		data.NetworkRating = NetworkRating(networkRating)
		if data.ProtoRevision, err = strconv.Atoi(string(getField(addPacket, 6))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid protocol revision in ATC add packet")
			return
		}
	} else {
		if data.CID, err = strconv.Atoi(string(getField(addPacket, 2))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid CID in pilot add packet")
			return
		}
		token = string(getField(addPacket, 3))
		var networkRating int
		if networkRating, err = strconv.Atoi(string(getField(addPacket, 4))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid network rating in pilot add packet")
			return
		}
		data.NetworkRating = NetworkRating(networkRating)
		if data.ProtoRevision, err = strconv.Atoi(string(getField(addPacket, 5))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid protocol revision in pilot add packet")
			return
		}
		data.RealName = string(getField(addPacket, 7))
	}

	if data.ProtoRevision < 100 || data.ProtoRevision > 101 {
		err = ErrInvalidAddPacket
		sendError(conn, InvalidProtocolRevisionError, "Invalid protocol revision")
		return
	}

	data.LoginTime = clock.Now()

	return
}

func (s *Server) attemptAuthentication(client *session.Session, token string) (err error) {
	// Check vatsim auth compatibility
	if client.ClientChallenge != "" {
		if err = client.Auth.Initialize(
			client.ClientID,
			[]byte(client.ClientChallenge),
		); err != nil {
			err = ErrInvalidIDPacket
			sendError(client.Conn, UnauthorizedSoftwareError, "Client incompatible with auth challenges")
			return
		}
	}

	const invalidLogonMsg = "Invalid CID/password"

	// Check if the provided token is actually a JWT
	if mostLikelyJwt([]byte(token)) {
		var jwtSecret string
		if jwtSecret, err = s.configKV.Get(db.ConfigJwtSecretKey); err != nil {
			return
		}

		var jwtToken *auth.JwtToken
		if jwtToken, err = auth.ParseJwtToken(token, []byte(jwtSecret)); err != nil {
			err = ErrInvalidAddPacket
			sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
			return
		}

		claims := jwtToken.CustomClaims()

		if claims.TokenType != "fsd" {
			err = ErrInvalidAddPacket
			sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
			return
		}

		if client.CID != claims.CID {
			err = ErrInvalidAddPacket
			sendError(client.Conn, RequestedLevelTooHighError, invalidLogonMsg)
			return
		}
		if client.NetworkRating > claims.NetworkRating {
			err = ErrInvalidAddPacket
			sendError(client.Conn, RequestedLevelTooHighError, "Requested level too high")
			return
		}
		if client.NetworkRating < NetworkRatingObserver {
			err = ErrInvalidAddPacket
			sendError(client.Conn, CertificateSuspendedError, "Certificate inactive or suspended")
			return
		}
		client.MaxNetworkRating = claims.NetworkRating

		return
	}

	// Otherwise, treat it as a plaintext password
	password := token

	// Attempt to fetch user
	user, err := s.users.GetUserByCID(client.CID)
	if err != nil {
		err = ErrInvalidAddPacket
		sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
		return
	}

	// Verify password hash
	if !s.users.VerifyPasswordHash(password, user.Password) {
		err = ErrInvalidAddPacket
		sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
		return
	}

	// Verify network rating
	if client.NetworkRating > NetworkRating(user.NetworkRating) {
		err = ErrInvalidAddPacket
		sendError(client.Conn, RequestedLevelTooHighError, "Requested level too high")
		return
	}
	if client.NetworkRating < NetworkRatingObserver {
		err = ErrInvalidAddPacket
		sendError(client.Conn, CertificateSuspendedError, "Certificate inactive or suspended")
		return
	}
	client.MaxNetworkRating = NetworkRating(user.NetworkRating)

	return
}

func (s *Server) broadcastAddPacket(client *session.Session) {
	var packet string
	if client.IsAtc {
		packet = fmt.Sprintf(
			"#AA%s:SERVER:%s:%d::%d:%d\r\n",
			client.Callsign,
			client.RealName,
			client.CID,
			client.NetworkRating,
			client.ProtoRevision)
	} else {
		packet = fmt.Sprintf(
			"#AP%s:SERVER:%d::%d:%d:1:%s\r\n",
			client.Callsign,
			client.CID,
			client.NetworkRating,
			client.ProtoRevision,
			client.RealName)
	}

	broadcastAll(s.registry, client, []byte(packet))
}

func (s *Server) broadcastDisconnectPacket(client *session.Session) {
	packet := strings.Builder{}
	if client.IsAtc {
		packet.WriteString("#DA")
	} else {
		packet.WriteString("#DP")
	}

	packet.WriteString(client.Callsign)
	packet.WriteString(":SERVER:")
	packet.WriteString(strconv.Itoa(client.CID))
	packet.WriteString("\r\n")

	broadcastAll(s.registry, client, []byte(packet.String()))
}

func (s *Server) sendMotd(client *session.Session) (err error) {
	welcomeMsg, _ := s.configKV.Get(db.ConfigWelcomeMessage)
	if welcomeMsg != "" {
		lines := strings.Split(welcomeMsg, "\n")
		for i := range lines {
			if err = s.sendServerTextMessage(client, lines[i]); err != nil {
				return
			}
		}
	} else {
		if err = s.sendServerTextMessage(client, "Connected to openfsd"); err != nil {
			return
		}
	}
	return
}

// sendServerTextMessage enqueues a server #TM via the session send channel.
func (s *Server) sendServerTextMessage(client *session.Session, msg string) (err error) {
	packet := strings.Builder{}
	packet.Grow(32 + len(msg))
	packet.WriteString("#TMserver:")
	packet.WriteString(client.Callsign)
	packet.WriteByte(':')
	packet.WriteString(msg)
	packet.WriteString("\r\n")

	return client.Send(packet.String())
}
