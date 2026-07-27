package server

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
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

func (s *Server) attemptAuthentication(client *session.Session, token string) (err error) {
	ip := client.RemoteIP()
	now := s.clock.Now()

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

	// Rate-limit auth attempts before expensive work.
	if !s.authFails.allowed(ip, now) {
		err = ErrInvalidAddPacket
		sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
		s.logger.Debug("auth rate limited", "ip", ip, "cid", client.CID)
		return
	}

	// Check if the provided token is actually a JWT
	if mostLikelyJwt([]byte(token)) {
		var jwtSecret string
		if jwtSecret, err = s.configKV.Get(db.ConfigJwtSecretKey); err != nil {
			return
		}

		var jwtToken *auth.JwtToken
		if jwtToken, err = auth.ParseJwtToken(token, []byte(jwtSecret)); err != nil {
			s.authFails.recordFailure(ip, now)
			err = ErrInvalidAddPacket
			sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
			return
		}

		claims := jwtToken.CustomClaims()

		if claims.TokenType != "fsd" {
			s.authFails.recordFailure(ip, now)
			err = ErrInvalidAddPacket
			sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
			return
		}

		if client.CID != claims.CID {
			s.authFails.recordFailure(ip, now)
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

		// Pilot PPL gate needs DB pilot_rating (JWT claims do not carry it).
		user, userErr := s.users.GetUserByCID(client.CID)
		if userErr != nil {
			s.authFails.recordFailure(ip, now)
			err = ErrInvalidAddPacket
			sendError(client.Conn, InvalidLogonError, invalidLogonMsg)
			return
		}
		if err = s.enforcePilotPPLRequirement(client, user); err != nil {
			return
		}
		setSessionPilotRating(client, user)

		return
	}

	// Otherwise, treat it as a plaintext password
	password := token

	// Attempt to fetch user; always run a bcrypt compare (dummy on miss) to
	// reduce CID-existence timing oracle.
	user, userErr := s.users.GetUserByCID(client.CID)
	hash := dummyBcryptHash
	if userErr == nil && user != nil {
		hash = user.Password
	}
	ok := s.users.VerifyPasswordHash(password, hash)
	if userErr != nil || !ok {
		s.authFails.recordFailure(ip, now)
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

	if err = s.enforcePilotPPLRequirement(client, user); err != nil {
		return
	}
	setSessionPilotRating(client, user)

	return
}

// setSessionPilotRating stores the certificate pilot rating on the session.
// Invalid / unknown ratings become 0 (P0).
func setSessionPilotRating(client *session.Session, user *db.User) {
	if client == nil || user == nil {
		return
	}
	if protocol.IsValidPilotRating(user.PilotRating) {
		client.PilotRating = user.PilotRating
		return
	}
	client.PilotRating = 0
}

// enforcePilotPPLRequirement rejects pilot (#AP) logins when REQUIRE_PILOT_PPL is
// enabled and the certificate's pilot_rating is below PPL. ATC is unaffected.
func (s *Server) enforcePilotPPLRequirement(client *session.Session, user *db.User) error {
	if client.IsAtc {
		return nil
	}
	if s.configKV == nil {
		return nil
	}
	raw, err := s.configKV.Get(db.ConfigRequirePilotPPL)
	if err != nil || !db.ParseBoolConfig(raw) {
		return nil
	}
	if user == nil || !protocol.MeetsMinimumPilotRating(user.PilotRating, protocol.PilotRatingPPL) {
		err := ErrInvalidAddPacket
		sendError(client.Conn, RequestedLevelTooHighError, "Pilot rating PPL or higher required")
		return err
	}
	return nil
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
	// Idempotent: client #DA/#DP path may have already notified peers.
	if !client.DisconnectNotified.CompareAndSwap(false, true) {
		return
	}

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
