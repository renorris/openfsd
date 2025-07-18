package fsd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/renorris/openfsd/db"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// sendError sends an FSD error packet to an io.Writer with the specified code and message.
// It returns an error if writing to the connection fails.
//
// This function must only be used during the login phase,
// as it synchronously writes the error directly to the
// connection socket.
func sendError(conn io.Writer, code int, message string) (err error) {
	packet := fmt.Sprintf("$ERserver:unknown:%d::%s\r\n", code, message)
	_, err = conn.Write([]byte(packet))
	return
}

// handleConn manages a single Client connection.
// If any errors occur during the process, it sends an error to the Client and closes the connection.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("An FSD connection goroutine panicked:")
			fmt.Println(err)
		}
	}()

	defer conn.Close()

	if err := sendServerIdent(conn); err != nil {
		fmt.Printf("Error sending server ident: %v\n", err)
		return
	}

	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 4096)
	scanner.Buffer(buf, len(buf))

	data, token, err := readLoginPackets(conn, scanner)
	if err != nil {
		return
	}

	// Check if the requested callsign is OK
	if !isValidClientCallsign([]byte(data.callsign)) {
		sendError(conn, CallsignInvalidError, "Callsign invalid")
		return
	}

	client := newClient(ctx, conn, scanner, data)

	// Attempt to authenticate connection
	if err = s.attemptAuthentication(client, token); err != nil {
		return
	}

	// Attempt to register to post office
	if err = s.postOffice.register(client); err != nil {
		if errors.Is(err, ErrCallsignInUse) {
			sendError(conn, CallsignInUseError, "Callsign already in use")
		}
		return
	}
	defer s.postOffice.release(client)

	// Send hello message to client
	if err = s.sendMotd(client); err != nil {
		return
	}

	// Broadcast add packet to entire server
	s.broadcastAddPacket(client)
	defer s.broadcastDisconnectPacket(client)

	s.eventLoop(client)
}

// sendServerIdent sends the initial server identification packet to the Client.
// It returns an error if writing to the connection fails.
func sendServerIdent(conn io.Writer) (err error) {
	packet := "$DISERVER:CLIENT:openfsd:6f70656e667364\r\n"
	_, err = conn.Write([]byte(packet))
	return
}

// loginData holds the data extracted from the Client's login packets.
type loginData struct {
	clientChallenge  string        // Optional Client challenge for authentication
	callsign         string        // Callsign of the Client
	cid              int           // Cert ID
	realName         string        // Real name
	networkRating    NetworkRating // Network rating of the Client
	maxNetworkRating NetworkRating // Maximum allowed network rating (what is stored in the database)
	protoRevision    int           // Protocol revision
	loginTime        time.Time     // Time of login
	clientId         uint16        // Client ID
	isAtc            bool          // True if the Client is an ATC, false if a pilot
}

// ErrInvalidAddPacket is returned when the add packet from the Client is invalid.
var ErrInvalidAddPacket = errors.New("invalid add packet")

// ErrInvalidIDPacket is returned when the ID packet from the Client is invalid.
var ErrInvalidIDPacket = errors.New("invalid ID packet")

// readLoginPackets reads the two expected login packets from the Client:
// the Client identification packet and the add packet.
// It parses these packets to extract the Client's data and returns it in a loginData struct.
// If any errors occur during reading or parsing, it sends an error to the Client and returns an error.
func readLoginPackets(conn net.Conn, scanner *bufio.Scanner) (data loginData, token string, err error) {
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

	// Check if the Client sent a challenge field
	if countFields(idPacket) == 9 {
		// Extract the challenge
		data.clientChallenge = string(getField(idPacket, 8))

		// Extract the client ID
		var clientId uint64
		clientId, err = strconv.ParseUint(string(getField(idPacket, 2)), 16, 16)
		if err != nil {
			err = ErrInvalidIDPacket
			sendError(conn, SyntaxError, "Error parsing client ID")
			return
		}
		data.clientId = uint16(clientId)
	}

	if len(addPacket) < 16 {
		err = ErrInvalidAddPacket
		sendError(conn, SyntaxError, "Invalid add packet")
		return
	}

	// Determine Client type
	var prefix string
	switch string(addPacket[:3]) {
	case "#AA":
		data.isAtc = true
		prefix = "#AA"
	case "#AP":
		prefix = "#AP"
	default:
		err = ErrInvalidAddPacket
		sendError(conn, SyntaxError, "Invalid add packet prefix")
		return
	}

	if data.isAtc {
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
		data.callsign = string(callsign)
	} else {
		sendError(conn, SyntaxError, "Invalid callsign in add packet")
		err = ErrInvalidAddPacket
		return
	}

	if data.isAtc {
		data.realName = string(getField(addPacket, 2))
		if data.cid, err = strconv.Atoi(string(getField(addPacket, 3))); err != nil {
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
		data.networkRating = NetworkRating(networkRating)
		if data.protoRevision, err = strconv.Atoi(string(getField(addPacket, 6))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid protocol revision in ATC add packet")
			return
		}
	} else {
		if data.cid, err = strconv.Atoi(string(getField(addPacket, 2))); err != nil {
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
		data.networkRating = NetworkRating(networkRating)
		if data.protoRevision, err = strconv.Atoi(string(getField(addPacket, 5))); err != nil {
			err = ErrInvalidAddPacket
			sendError(conn, SyntaxError, "Invalid protocol revision in pilot add packet")
			return
		}
		data.realName = string(getField(addPacket, 7))
	}

	if data.protoRevision < 100 || data.protoRevision > 101 {
		err = ErrInvalidAddPacket
		sendError(conn, InvalidProtocolRevisionError, "Invalid protocol revision")
		return
	}

	data.loginTime = time.Now()

	return
}

func (s *Server) attemptAuthentication(client *Client, token string) (err error) {
	// Check vatsim auth compatibility
	if client.clientChallenge != "" {
		if err = client.authState.Initialize(
			client.clientId,
			[]byte(client.clientChallenge),
		); err != nil {
			err = ErrInvalidIDPacket
			sendError(client.conn, UnauthorizedSoftwareError, "Client incompatible with auth challenges")
			return
		}
	}

	const invalidLogonMsg = "Invalid CID/password"

	// Check if the provided token is actually a JWT
	if mostLikelyJwt([]byte(token)) {
		var jwtSecret string
		if jwtSecret, err = s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey); err != nil {
			return
		}

		var jwtToken *JwtToken
		if jwtToken, err = ParseJwtToken(token, []byte(jwtSecret)); err != nil {
			err = ErrInvalidAddPacket
			sendError(client.conn, InvalidLogonError, invalidLogonMsg)
			return
		}

		claims := jwtToken.CustomClaims()

		if claims.TokenType != "fsd" {
			err = ErrInvalidAddPacket
			sendError(client.conn, InvalidLogonError, invalidLogonMsg)
			return
		}

		if client.cid != claims.CID {
			err = ErrInvalidAddPacket
			sendError(client.conn, RequestedLevelTooHighError, invalidLogonMsg)
			return
		}
		if client.networkRating > claims.NetworkRating {
			err = ErrInvalidAddPacket
			sendError(client.conn, RequestedLevelTooHighError, "Requested level too high")
			return
		}
		if client.networkRating < NetworkRatingObserver {
			err = ErrInvalidAddPacket
			sendError(client.conn, CertificateSuspendedError, "Certificate inactive or suspended")
			return
		}
		client.maxNetworkRating = claims.NetworkRating

		return
	}

	// Otherwise, treat it as a plaintext password
	password := token

	// Attempt to fetch user
	user, err := s.dbRepo.UserRepo.GetUserByCID(client.cid)
	if err != nil {
		err = ErrInvalidAddPacket
		sendError(client.conn, InvalidLogonError, invalidLogonMsg)
		return
	}

	// Verify password hash
	if !s.dbRepo.UserRepo.VerifyPasswordHash(password, user.Password) {
		err = ErrInvalidAddPacket
		sendError(client.conn, InvalidLogonError, invalidLogonMsg)
		return
	}

	// Verify network rating
	if client.networkRating > NetworkRating(user.NetworkRating) {
		err = ErrInvalidAddPacket
		sendError(client.conn, RequestedLevelTooHighError, "Requested level too high")
		return
	}
	if client.networkRating < NetworkRatingObserver {
		err = ErrInvalidAddPacket
		sendError(client.conn, CertificateSuspendedError, "Certificate inactive or suspended")
		return
	}
	client.maxNetworkRating = NetworkRating(user.NetworkRating)

	return
}

func (s *Server) broadcastAddPacket(client *Client) {
	var packet string
	if client.isAtc {
		packet = fmt.Sprintf(
			"#AA%s:SERVER:%s:%d::%d:%d\r\n",
			client.callsign,
			client.realName,
			client.cid,
			client.networkRating,
			client.protoRevision)
	} else {
		packet = fmt.Sprintf(
			"#AP%s:SERVER:%d::%d:%d:1:%s\r\n",
			client.callsign,
			client.cid,
			client.networkRating,
			client.protoRevision,
			client.realName)
	}

	broadcastAll(s.postOffice, client, []byte(packet))
}

func (s *Server) broadcastDisconnectPacket(client *Client) {
	packet := strings.Builder{}
	if client.isAtc {
		packet.WriteString("#DA")
	} else {
		packet.WriteString("#DP")
	}

	packet.WriteString(client.callsign)
	packet.WriteString(":SERVER:")
	packet.WriteString(strconv.Itoa(client.cid))
	packet.WriteString("\r\n")

	broadcastAll(s.postOffice, client, []byte(packet.String()))
}

func (s *Server) sendMotd(client *Client) (err error) {
	welcomeMsg := db.GetWelcomeMessage(&s.dbRepo.ConfigRepo)
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

// sendServerTextMessage synchronously sends a server #TM to the client's socket
func (s *Server) sendServerTextMessage(client *Client, msg string) (err error) {
	packet := strings.Builder{}
	packet.Grow(32 + len(msg))
	packet.WriteString("#TMserver:")
	packet.WriteString(client.callsign)
	packet.WriteByte(':')
	packet.WriteString(msg)
	packet.WriteString("\r\n")

	_, err = client.conn.Write([]byte(packet.String()))
	return
}
