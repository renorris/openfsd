package client

import (
	"bufio"
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	auth2 "github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/protocol/vatsimauth"
	"github.com/renorris/openfsd/servercontext"
	"golang.org/x/crypto/bcrypt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

const maxFSDPacketSize = 1536
const writeFlushInterval = 200 * time.Millisecond

var AlwaysImmediate = false

type writePayload struct {
	packet    string
	immediate bool
}

type Connection struct {
	ctx       context.Context
	cancelCtx func()
	conn      *net.TCPConn

	readerChan chan string
	writerChan chan writePayload

	doneSig chan struct{}
}

func NewConnection(ctx context.Context, conn *net.TCPConn) (*Connection, error) {
	// Ensure any lingering data will be flushed
	if err := conn.SetLinger(1); err != nil {
		return nil, err
	}

	c, cancel := context.WithCancel(ctx)

	connection := Connection{
		ctx:       c,
		cancelCtx: cancel,
		conn:      conn,

		readerChan: make(chan string),
		writerChan: make(chan writePayload),

		doneSig: make(chan struct{}),
	}

	// Start reader
	go func() {
		defer connection.cancelCtx()

		if err := connection.reader(); err != nil {
			// Attempt to send the error to the client if it implements FSDError
			var fsdError *protocol.FSDError
			if errors.As(err, &fsdError) {
				connection.WritePacket(fsdError.Serialize())
			}
		}
	}()

	// Start writer
	go func() {
		defer close(connection.doneSig)
		defer connection.cancelCtx()

		if err := connection.writer(); err != nil {
			// TODO: properly handle error
		}
	}()

	return &connection, nil
}

func (c *Connection) writePacket(packet string, immediate bool) error {

	payload := writePayload{
		packet:    packet,
		immediate: immediate,
	}

	select {
	case <-c.ctx.Done():
		return errors.New("context closed")
	case c.writerChan <- payload:
		return nil
	}
}

func (c *Connection) WritePacket(packet string) error {
	return c.writePacket(packet, AlwaysImmediate)
}

func (c *Connection) WritePacketImmediately(packet string) error {
	return c.writePacket(packet, true)
}

func (c *Connection) ReadPacket() (packet string, err error) {
	select {
	case <-c.ctx.Done():
		return "", errors.New("context closed")
	case packet = <-c.readerChan:
		return packet, nil
	}
}

func (c *Connection) reader() error {

	reader := bufio.NewReaderSize(c.conn, maxFSDPacketSize)

	for {
		if err := c.conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			// TODO: properly handle error
			return err
		}

		var packet string

		if buf, err := reader.ReadSlice('\n'); err != nil {
			return err
		} else if len(buf) > maxFSDPacketSize {
			return protocol.NewGenericFSDError(protocol.SyntaxError, "", "packet too long")
		} else if !strings.HasSuffix(string(buf), protocol.PacketDelimiter) {
			return protocol.NewGenericFSDError(protocol.SyntaxError, "", "invalid packet delimeter")
		} else {
			// Copy the buffer since ReadSlice() will overwrite it eventually.
			bufCopy := make([]byte, len(buf))
			copy(bufCopy, buf)
			packet = string(buf)
		}

		// Send the packet over the channel
		select {
		case <-c.ctx.Done():
			return nil
		case c.readerChan <- packet:
		}
	}
}

func (c *Connection) writer() error {

	writer := bufio.NewWriter(c.conn)
	defer writer.Flush()

	ticker := time.NewTicker(writeFlushInterval)
	defer ticker.Stop()
	ticker.Stop()
	tickerActive := false

	for {
		// Read incoming packet
		select {
		case <-c.ctx.Done():
			return nil
		case payload, ok := <-c.writerChan:
			if !ok {
				return nil
			}

			if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				// TODO: properly handle error
				return err
			}

			if _, err := writer.WriteString(payload.packet); err != nil {
				// TODO: properly handle error
				return err
			}

			if payload.immediate {
				if err := writer.Flush(); err != nil {
					// TODO: properly handle error
					return err
				}
			}

			if !tickerActive {
				tickerActive = true
				ticker.Reset(writeFlushInterval)
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				// TODO: properly handle error
				return err
			}

			if err := writer.Flush(); err != nil {
				// TODO: properly handle error
				return err
			}

			ticker.Stop()
			tickerActive = false
		}
	}
}

// Start handles the logical lifetime of this connection
func (c *Connection) Start() {
	// Ensure connection is closed when finished
	defer c.conn.Close()

	// Wait until connection state cleans up before closing connection
	defer func() {
		<-c.doneSig
	}()

	// Ensure context is always cancelled
	defer c.cancelCtx()

	// Attempt to login
	var fsdClient *FSDClient
	var err error
	if fsdClient, err = c.attemptLogin(); err != nil {
		var fsdError *protocol.FSDError
		if errors.As(err, &fsdError) {
			c.WritePacketImmediately(fsdError.Serialize())
		}
		return
	}

	// Register to post office
	if err = servercontext.PostOffice().RegisterAddress(fsdClient); err != nil {
		if errors.Is(err, postoffice.KeyInUseError) {
			c.WritePacketImmediately(protocol.NewGenericFSDError(protocol.CallsignInUseError, fsdClient.callsign, "").Serialize())
		}
		return
	}
	defer servercontext.PostOffice().DeregisterAddress(fsdClient)

	// Broadcast add pilot message
	fsdClient.broadcastAddPilot()
	defer fsdClient.broadcastDeletePilot()

	if err = fsdClient.sendMOTD(); err != nil {
		return
	}

	// Now that we're logged in, run the event loop until an error occurs
	fsdClient.EventLoop()
}

// attemptLogin attempts to log in the connection
func (c *Connection) attemptLogin() (fsdClient *FSDClient, err error) {
	// Generate the initial challenge
	var initChallenge string
	if initChallenge, err = vatsimauth.GenerateChallenge(); err != nil {
		log.Println("error calling vatsimauth.GenerateChallenge(): " + err.Error())
		fsdErr := protocol.NewGenericFSDError(protocol.SyntaxError, "", "internal server error (error generating initial challenge)")
		return nil, fsdErr
	}

	// Generate server identification packet
	serverIdentPDU := protocol.ServerIdentificationPDU{
		From:             protocol.ServerCallsign,
		To:               "CLIENT",
		Version:          servercontext.VersionIdentifier,
		InitialChallenge: initChallenge,
	}
	serverIdentPacket := serverIdentPDU.Serialize()

	// Write server identification packet
	if err = c.WritePacketImmediately(serverIdentPacket); err != nil {
		fsdErr := protocol.NewGenericFSDError(protocol.SyntaxError, "", "internal server error (error writing $DI server identification packet)")
		return nil, fsdErr
	}

	// Read the first expected packet: client identification
	var packet string
	if packet, err = c.ReadPacket(); err != nil {
		fsdErr := protocol.NewGenericFSDError(protocol.SyntaxError, "", "error reading $ID client identification packet")
		return nil, fsdErr
	}

	// Parse it
	var clientIdentPDU protocol.ClientIdentificationPDU
	if err = clientIdentPDU.Parse(packet); err != nil {
		return nil, err
	}

	// Read the second expected packet: add pilot
	if packet, err = c.ReadPacket(); err != nil {
		fsdErr := protocol.NewGenericFSDError(protocol.SyntaxError, "", "error reading #AP add pilot packet")
		return nil, fsdErr
	}

	var addPilotPDU protocol.AddPilotPDU
	if err = addPilotPDU.Parse(packet); err != nil {
		return nil, err
	}

	// Handle authentication
	var networkRating protocol.NetworkRating
	var pilotRating protocol.PilotRating
	if servercontext.Config().PlaintextPasswords { // Treat token field as a plaintext password
		if networkRating, pilotRating, err = verifyPassword(clientIdentPDU.CID, addPilotPDU.Token); err != nil {
			fsdErr := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid CID and/or password")
			return nil, fsdErr
		}

		// Check if the account is suspended or inactive
		if addPilotPDU.NetworkRating <= protocol.NetworkRatingSUS {
			fsdErr := protocol.NewGenericFSDError(protocol.InvalidLogonError, strconv.Itoa(int(addPilotPDU.NetworkRating)), "account suspended/inactive")
			return nil, fsdErr
		}

		// Check if the requested PDU rating exceeds their user record
		if addPilotPDU.NetworkRating > networkRating {
			fsdErr := protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError, strconv.Itoa(int(addPilotPDU.NetworkRating)), "try again at or below your assigned rating")
			return nil, fsdErr
		}

	} else { // Treat token field as a JWT token
		var token *jwt.Token
		var verifier auth2.DefaultVerifier
		if token, err = verifier.VerifyJWT(addPilotPDU.Token); err != nil {
			fsdErr := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token")
			return nil, fsdErr
		}

		claims := auth2.FSDJWTClaims{}
		if err = claims.Parse(token); err != nil {
			fsdErr := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token claims")
			return nil, fsdErr
		}

		if claims.CID() != clientIdentPDU.CID {
			fsdErr := protocol.NewGenericFSDError(protocol.InvalidLogonError, "", "invalid token claims (CID)")
			return nil, fsdErr
		}

		if claims.ControllerRating() < addPilotPDU.NetworkRating {
			fsdErr := protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError, strconv.Itoa(int(claims.ControllerRating())), "try again at or below your assigned rating")
			return nil, fsdErr
		}

		networkRating = claims.ControllerRating()
		pilotRating = claims.PilotRating()
	}

	// Check for disallowed callsign
	switch clientIdentPDU.From {
	case protocol.ServerCallsign, protocol.ClientQueryBroadcastRecipient, protocol.ClientQueryBroadcastRecipientPilots:
		fsdErr := protocol.NewGenericFSDError(protocol.CallsignInvalidError, clientIdentPDU.From, "forbidden callsign")
		return nil, fsdErr
	}

	// Verify protocol revision
	if addPilotPDU.ProtocolRevision != protocol.ProtoRevisionVatsim2022 {
		fsdErr := protocol.NewGenericFSDError(protocol.InvalidProtocolRevisionError, strconv.Itoa(addPilotPDU.ProtocolRevision), "please connect with a client that supports the Vatsim2022 (101) protocol revision")
		return nil, fsdErr
	}

	// Verify if this browser is supported by vatsimauth
	if _, ok := vatsimauth.Keys[clientIdentPDU.ClientID]; !ok {
		fsdErr := protocol.NewGenericFSDError(protocol.UnauthorizedSoftwareError, "", "provided client ID is not supported by vatsimauth")
		return nil, fsdErr
	}

	fsdClient = NewFSDClient(c, &clientIdentPDU, &addPilotPDU, initChallenge, pilotRating)

	return fsdClient, nil
}

// verifyPassword verifies a password in the cases when PLAINTEXT_PASSWORDS is in use.
func verifyPassword(cid int, password string) (networkRating protocol.NetworkRating, pilotRating protocol.PilotRating, err error) {
	var userRecord database.FSDUserRecord
	if err = userRecord.LoadByCID(servercontext.DB(), cid); err != nil {
		return -1, -1, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(userRecord.FSDPassword), []byte(password))
	if err != nil {
		return -1, -1, err
	}

	return userRecord.NetworkRating, userRecord.PilotRating, nil
}
