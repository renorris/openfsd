package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/vatsimauth"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

type FSDClient struct {
	Ctx       context.Context
	CancelCtx func()

	Conn   *net.TCPConn
	Reader *bufio.Reader

	AuthVerify                 *vatsimauth.VatsimAuth // Auth state to verify client's auth responses
	PendingAuthVerifyChallenge string                 // Store the pending challenge sent to the client
	AuthSelf                   *vatsimauth.VatsimAuth // Auth state for interrogating client

	Callsign        string
	CID             int
	NetworkRating   int
	SimulatorType   int
	RealName        string
	CurrentGeohash  uint64
	SendFastEnabled bool

	Kill    chan string // Signal to disconnect this client
	Mailbox chan string // Incoming messages
}

// EventLoop runs the main event loop for an FSD client.
// All clients that reach this stage are logged in
func EventLoop(client *FSDClient) {

	// Setup reader goroutine
	incomingPackets := make(chan string)
	go func(ctx context.Context, packetChan chan string) {
		defer close(packetChan)

		for {
			// Reset the deadline
			err := client.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			if err != nil {
				return
			}

			var buf []byte
			buf, err = client.Reader.ReadSlice('\n')
			if err != nil {
				return
			}

			packet := string(buf)

			// Validate delimiter
			if len(packet) < 2 || string(packet[len(packet)-2:]) != "\r\n" {
				return
			}

			// Send the packet over the channel
			// (also watch for context.Done)
			select {
			case <-ctx.Done():
				return
			case packetChan <- packet:
			}
		}
	}(client.Ctx, incomingPackets)

	// Defer "delete pilot" broadcast
	defer func(client *FSDClient) {
		deletePilotPDU := protocol.DeletePilotPDU{
			From: client.Callsign,
			CID:  client.CID,
		}

		mail := NewMail(client)
		mail.SetType(MailTypeBroadcastAll)
		mail.AddPacket(deletePilotPDU.Serialize())
		PO.SendMail([]Mail{*mail})
	}(client)

	// Main loop
	for {
		select {
		case <-client.Ctx.Done(): // Check for context cancel
			return

		case packet, ok := <-incomingPackets: // Read incoming packets
			// Check if the reader closed
			if !ok {
				return
			}

			// Find the processor for this packet
			processor, err := GetProcessor(packet)
			if err != nil {
				sendSyntaxError(client.Conn)
				return
			}

			result := processor(client, packet)

			// Send replies to the client
			for _, replyPacket := range result.Replies {
				err := client.writePacket(5*time.Second, replyPacket)
				if err != nil {
					return
				}
			}

			// Send mail
			PO.SendMail(result.Mail)

			// Disconnect the client if flagged
			if result.ShouldDisconnect {
				return
			}

		case mailPacket := <-client.Mailbox: // Read incoming mail messages
			err := client.writePacket(5*time.Second, mailPacket)
			if err != nil {
				return
			}

		case killPacket, ok := <-client.Kill: // Read incoming kill signals
			if !ok {
				return
			}

			// Write the kill packet
			err := client.writePacket(5*time.Second, killPacket)
			if err != nil {
				return
			}

			// Close connection
			return
		}
	}
}

func sendSyntaxError(conn *net.TCPConn) {
	conn.Write([]byte(protocol.NewGenericFSDError(protocol.SyntaxError).Serialize()))
}

func HandleConnection(conn *net.TCPConn) {
	// Set the linger value to 1 second
	err := conn.SetLinger(1)
	if err != nil {
		log.Printf("error setting connection linger value")
		return
	}

	// Defer connection close
	defer func(conn *net.TCPConn) {
		err := conn.Close()
		if err != nil {
			log.Println("error closing connection: " + err.Error())
		}
	}(conn)

	// Generate the initial challenge
	initChallenge, err := vatsimauth.GenerateChallenge()
	if err != nil {
		log.Printf("Error generating challenge string:\n%s", err.Error())
		return
	}

	serverIdentPDU := protocol.ServerIdentificationPDU{
		From:             protocol.ServerCallsign,
		To:               "CLIENT",
		Version:          "openfsd",
		InitialChallenge: initChallenge,
	}
	serverIdentPacket := serverIdentPDU.Serialize()

	// The client has 5 seconds to log in
	if err = conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	if err = conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}

	_, err = io.Copy(conn, bytes.NewReader([]byte(serverIdentPacket)))
	if err != nil {
		return
	}

	reader := bufio.NewReaderSize(conn, 1024)
	var buf []byte

	buf, err = reader.ReadSlice('\n')
	if err != nil {
		sendSyntaxError(conn)
		return
	}
	packet := string(buf)

	// Validate delimiter
	if len(packet) < 2 || string(packet[len(packet)-2:]) != "\r\n" {
		sendSyntaxError(conn)
		return
	}

	clientIdentPDU, err := protocol.ParseClientIdentificationPDU(packet)
	if err != nil {
		var fsdError *protocol.FSDError
		if errors.As(err, &fsdError) {
			conn.Write([]byte(fsdError.Serialize()))
		}
		return
	}

	buf, err = reader.ReadSlice('\n')
	if err != nil {
		sendSyntaxError(conn)
		return
	}
	packet = string(buf)

	// Validate delimiter
	if len(packet) < 2 || string(packet[len(packet)-2:]) != "\r\n" {
		sendSyntaxError(conn)
		return
	}

	addPilotPDU, err := protocol.ParseAddPilotPDU(packet)
	if err != nil {
		var fsdError *protocol.FSDError
		if errors.As(err, &fsdError) {
			conn.Write([]byte(fsdError.Serialize()))
		}
		return
	}

	// Handle authentication
	var networkRating int
	if SC.PlaintextPasswords { // Treat token field as a plaintext password
		plaintextPassword := addPilotPDU.Token
		networkRating = 0

		userRecord, err := GetUserRecord(DB, clientIdentPDU.CID)
		if err != nil { // Check for error
			log.Printf("error fetching user record: " + err.Error())
			return
		}

		if userRecord == nil {
			conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
			return
		}

		if userRecord.Rating < addPilotPDU.NetworkRating {
			conn.Write([]byte(protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError).Serialize()))
			return
		}

		err = bcrypt.CompareHashAndPassword([]byte(userRecord.Password), []byte(plaintextPassword))
		if err != nil {
			conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
			return
		}
	} else { // Treat token field as a JWT token
		networkRating, err = verifyJWTToken(clientIdentPDU.CID, addPilotPDU.NetworkRating, addPilotPDU.Token)
		if err != nil {
			var fsdError *protocol.FSDError
			if errors.As(err, &fsdError) {
				conn.Write([]byte(fsdError.Serialize()))
			} else {
				conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
			}
			return
		}
	}

	// Verify callsign
	switch clientIdentPDU.From {
	case protocol.ServerCallsign, protocol.ClientQueryBroadcastRecipient, protocol.ClientQueryBroadcastRecipientPilots:
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.CallsignInvalidError).Serialize()))
		return
	}

	// Verify protocol revision
	if addPilotPDU.ProtocolRevision != protocol.ProtoRevisionVatsim2022 {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidProtocolRevisionError).Serialize()))
		return
	}

	// Verify if we support this client
	_, ok := vatsimauth.Keys[clientIdentPDU.ClientID]
	if !ok {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.UnauthorizedSoftwareError).Serialize()))
		return
	}

	// Verify fields in login PDUs
	if clientIdentPDU.From != addPilotPDU.From ||
		clientIdentPDU.CID != addPilotPDU.CID {
		sendSyntaxError(conn)
		return
	}

	// Configure client
	ctx, cancelCtx := context.WithCancel(context.Background())

	// Defer context close
	defer func(cancelCtx func()) {
		cancelCtx()
	}(cancelCtx)

	fsdClient := FSDClient{
		Ctx:                        ctx,
		CancelCtx:                  cancelCtx,
		Conn:                       conn,
		Reader:                     reader,
		AuthVerify:                 &vatsimauth.VatsimAuth{},
		PendingAuthVerifyChallenge: "",
		AuthSelf:                   &vatsimauth.VatsimAuth{},
		Callsign:                   clientIdentPDU.From,
		CID:                        clientIdentPDU.CID,
		NetworkRating:              networkRating,
		SimulatorType:              addPilotPDU.SimulatorType,
		RealName:                   addPilotPDU.RealName,
		CurrentGeohash:             0,
		SendFastEnabled:            false,
		Kill:                       make(chan string, 1),
		Mailbox:                    make(chan string, 16),
	}

	// Register callsign to the post office. End the connection if callsign already exists
	err = PO.RegisterCallsign(clientIdentPDU.From, &fsdClient)
	if err != nil {
		if errors.Is(err, CallsignAlreadyRegisteredError) {
			pdu := protocol.NewGenericFSDError(protocol.CallsignInUseError)
			conn.Write([]byte(pdu.Serialize()))
		}
		return
	}

	// Defer deregistration
	defer func(callsign string) {
		err := PO.DeregisterCallsign(callsign)
		if err != nil {
			log.Printf("error deregistering callsign: " + err.Error())
		}
	}(clientIdentPDU.From)

	// Configure vatsim auth states
	fsdClient.AuthSelf = vatsimauth.NewVatsimAuth(clientIdentPDU.ClientID, vatsimauth.Keys[clientIdentPDU.ClientID])
	fsdClient.AuthSelf.SetInitialChallenge(clientIdentPDU.InitialChallenge)
	fsdClient.AuthVerify = vatsimauth.NewVatsimAuth(clientIdentPDU.ClientID, vatsimauth.Keys[clientIdentPDU.ClientID])
	fsdClient.AuthVerify.SetInitialChallenge(initChallenge)

	// Broadcast AddPilot packet to network
	addPilotPDU.Token = ""
	mail := NewMail(&fsdClient)
	mail.SetType(MailTypeBroadcastAll)
	mail.AddPacket(addPilotPDU.Serialize())
	PO.SendMail([]Mail{*mail})

	// Send MOTD
	lines := strings.Split(SC.MOTD, "\n")
	for _, line := range lines {
		pdu := protocol.TextMessagePDU{
			From:    protocol.ServerCallsign,
			To:      clientIdentPDU.From,
			Message: line,
		}
		_, err := conn.Write([]byte(pdu.Serialize()))
		if err != nil {
			return
		}
	}

	// Start the event loop
	EventLoop(&fsdClient)
}

// writePacket writes a packet to this client's connection
// timeout sets the write deadline (relative to time.Now). No deadline will be set if timeout = -1
func (c *FSDClient) writePacket(timeout time.Duration, packet string) error {
	// Reset the deadline
	if timeout > 0 {
		err := c.Conn.SetWriteDeadline(time.Now().Add(timeout * time.Second))
		if err != nil {
			return err
		}
	}

	// Attempt to write the packet
	_, err := io.Copy(c.Conn, bytes.NewReader([]byte(packet)))
	if err != nil {
		return err
	}

	return nil
}

// verifyJWTToken compares the claimed fields token `token` to cid and networkRating (from the plaintext FSD packet)
// Returns the signed network rating on success
func verifyJWTToken(cid, networkRating int, token string) (signedNetworkRating int, err error) {
	// Validate token signature
	claims := jwt.MapClaims{}
	t, err := jwt.ParseWithClaims(token, &claims, func(token *jwt.Token) (interface{}, error) {
		return JWTKey, nil
	})
	if err != nil {
		return -1, protocol.NewGenericFSDError(protocol.InvalidLogonError)
	}

	// Check for expiry
	exp, err := t.Claims.GetExpirationTime()
	if err != nil {
		return -1, protocol.NewGenericFSDError(protocol.InvalidLogonError)
	}
	if time.Now().After(exp.Time) {
		return -1, protocol.NewGenericFSDError(protocol.InvalidLogonError)
	}

	// Verify claimed CID
	claimedCID, err := claims.GetSubject()
	if err != nil {
		return -1, protocol.NewGenericFSDError(protocol.InvalidLogonError)
	}

	cidInt, err := strconv.Atoi(claimedCID)
	if err != nil {
		return -1, errors.Join(errors.New("error parsing CID"))
	}

	if cidInt != cid {
		return -1, protocol.NewGenericFSDError(protocol.InvalidLogonError)
	}

	// Verify controller rating
	claimedRating, ok := claims["controller_rating"].(float64)
	if !ok {
		return -1, protocol.NewGenericFSDError(protocol.InvalidLogonError)
	}

	if networkRating > int(claimedRating) {
		return -1, protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError)
	}

	return int(claimedRating), nil
}
