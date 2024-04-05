package main

import (
	"bufio"
	"context"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/vatsimauth"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

type FSDClient struct {
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
	CurrentGeohash  string
	SendFastEnabled bool

	Kill    chan string // Signal to disconnect this client
	Mailbox chan string // Incoming messages
}

// EventLoop runs the main event loop for an FSD client.
// All clients that reach this stage are logged in
func EventLoop(client *FSDClient) {

	// Reader goroutine
	packetsRead := make(chan string)
	readerCtx, cancelReader := context.WithCancel(context.Background())
	go func(ctx context.Context) {
		for {
			// Reset the deadline
			err := client.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			if err != nil {
				close(packetsRead)
				return
			}

			var buf []byte
			buf, err = client.Reader.ReadSlice('\n')
			if err != nil {
				close(packetsRead)
				return
			}

			packet := string(buf)

			// Validate delimiter
			if len(packet) < 2 || string(packet[len(packet)-2:]) != "\r\n" {
				close(packetsRead)
				return
			}

			// Send the packet over the channel
			// (also watch for context.Done)
			select {
			case <-ctx.Done():
				close(packetsRead)
				return
			case packetsRead <- packet:
			}
		}
	}(readerCtx)

	// Defer reader cancellation
	defer cancelReader()

	// Writer goroutine
	packetsToWrite := make(chan string, 16)
	writerClosed := make(chan struct{})
	go func() {
		for {
			var packet string
			var ok bool
			// Wait for a packet
			select {
			case packet, ok = <-packetsToWrite:
				if !ok {
					close(writerClosed)
					return
				}
			}

			// Reset the deadline
			err := client.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err != nil {
				close(writerClosed)
				// Exhaust packetsToWrite
				for {
					select {
					case _, ok := <-packetsToWrite:
						if !ok {
							return
						}
					}
				}
			}

			// Attempt to write the packet
			_, err = client.Conn.Write([]byte(packet))
			if err != nil {
				close(writerClosed)
				// Exhaust packetsToWrite
				for {
					select {
					case _, ok := <-packetsToWrite:
						if !ok {
							return
						}
					}
				}
			}
		}
	}()

	// Wait max 100 milliseconds for writer to flush before continuing the connection shutdown
	defer func() {
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-writerClosed:
		case <-timer.C:
		}
	}()

	// Defer writer close
	defer close(packetsToWrite)

	// Defer "delete pilot" broadcast
	defer func() {
		deletePilotPDU := protocol.DeletePilotPDU{
			From: client.Callsign,
			CID:  client.CID,
		}

		mail := NewMail(client)
		mail.SetType(MailTypeBroadcastAll)
		mail.AddPacket(deletePilotPDU.Serialize())
		PO.SendMail([]Mail{*mail})
	}()

	// Main loop
	for {
		select {
		case packet, ok := <-packetsRead:
			// Check if the reader closed
			if !ok {
				return
			}

			// Find the processor for this packet
			processor, err := GetProcessor(packet)
			if err != nil {
				packetsToWrite <- protocol.NewGenericFSDError(protocol.SyntaxError).Serialize()
				return
			}

			result := processor(client, packet)

			// Send replies to the client
			for _, replyPacket := range result.Replies {
				packetsToWrite <- replyPacket
			}

			// Send mail
			PO.SendMail(result.Mail)

			// Disconnect the client if flagged
			if result.ShouldDisconnect {
				return
			}
		case <-writerClosed:
			return
		case mailPacket := <-client.Mailbox:
			packetsToWrite <- mailPacket
		case s, ok := <-client.Kill:
			if ok {
				select {
				case packetsToWrite <- s:
				default:
				}
			}
			return
		}
	}
}

func sendSyntaxError(conn *net.TCPConn) {
	conn.Write([]byte(protocol.NewGenericFSDError(protocol.SyntaxError).Serialize()))
}

func HandleConnection(conn *net.TCPConn) {
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Printf("Error closing connection for %s\n%s", conn.RemoteAddr().String(), err.Error())
		}
	}()

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

	// The client has 2 seconds to log in
	if err = conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return
	}
	if err = conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return
	}

	_, err = conn.Write([]byte(serverIdentPacket))
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

	// Validate token signature
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(addPilotPDU.Token, &claims, func(token *jwt.Token) (interface{}, error) {
		return JWTKey, nil
	})
	if err != nil {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
		return
	}

	// Check for expiry
	exp, err := token.Claims.GetExpirationTime()
	if err != nil {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
		return
	}
	if time.Now().After(exp.Time) {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
		return
	}

	// Verify claimed CID
	claimedCID, err := claims.GetSubject()
	if err != nil {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
		return
	}

	cidInt, err := strconv.Atoi(claimedCID)
	if err != nil {
		sendSyntaxError(conn)
		return
	}

	if cidInt != clientIdentPDU.CID {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.InvalidLogonError).Serialize()))
		return
	}

	// Verify controller rating
	claimedRating, ok := claims["controller_rating"].(float64)
	if !ok {
		sendSyntaxError(conn)
		return
	}

	if addPilotPDU.NetworkRating > int(claimedRating) {
		conn.Write([]byte(protocol.NewGenericFSDError(protocol.RequestedLevelTooHighError).Serialize()))
		return
	}

	// Configure client
	fsdClient := FSDClient{
		Conn:                       conn,
		Reader:                     reader,
		AuthVerify:                 &vatsimauth.VatsimAuth{},
		PendingAuthVerifyChallenge: "",
		AuthSelf:                   &vatsimauth.VatsimAuth{},
		Callsign:                   clientIdentPDU.From,
		CID:                        clientIdentPDU.CID,
		NetworkRating:              int(claimedRating),
		SimulatorType:              addPilotPDU.SimulatorType,
		RealName:                   addPilotPDU.RealName,
		CurrentGeohash:             "",
		SendFastEnabled:            false,
		Kill:                       make(chan string, 1),
		Mailbox:                    make(chan string, 16),
	}

	// Register callsign to the post office. End the connection if callsign already exists
	{
		err := PO.RegisterCallsign(clientIdentPDU.From, &fsdClient)
		if err != nil {
			if errors.Is(err, CallsignAlreadyRegisteredError) {
				pdu := protocol.NewGenericFSDError(protocol.CallsignInUseError)
				conn.Write([]byte(pdu.Serialize()))
			}
			return
		}
	}
	defer PO.DeregisterCallsign(clientIdentPDU.From)

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
