package fsd

import (
	"bufio"
	"context"
	"go.uber.org/atomic"
	"net"
	"strconv"
	"strings"
)

type Client struct {
	conn      net.Conn
	scanner   *bufio.Scanner
	ctx       context.Context
	cancelCtx func()
	sendChan  chan string

	lat, lon, visRange atomic.Float64

	flightPlan         atomic.String
	assignedBeaconCode atomic.String

	frequency   atomic.String // OnlineUserATC frequency
	altitude    atomic.Int32  // OnlineUserPilot altitude
	groundspeed atomic.Int32  // OnlineUserPilot ground speed
	transponder atomic.String // Active pilot transponder
	heading     atomic.Int32  // OnlineUserPilot heading
	lastUpdated atomic.Time   // Last updated time

	facilityType int // OnlineUserATC facility type. This value is only relevant for OnlineUserATC
	loginData

	authState vatsimAuthState
}

func newClient(ctx context.Context, conn net.Conn, scanner *bufio.Scanner, loginData loginData) (client *Client) {
	clientCtx, cancel := context.WithCancel(ctx)
	return &Client{
		conn:      conn,
		scanner:   scanner,
		ctx:       clientCtx,
		cancelCtx: cancel,
		sendChan:  make(chan string, 32),
		loginData: loginData,
	}
}

func (c *Client) senderWorker() {
	defer c.conn.Close()
	defer c.cancelCtx()

	for {
		select {
		case packet := <-c.sendChan:
			if _, err := c.conn.Write([]byte(packet)); err != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// sendError sends an FSD error packet to a Client with the specified code and message.
// It returns an error if writing to the connection fails.
//
// This call is thread-safe
func (c *Client) sendError(code int, message string) (err error) {
	packet := strings.Builder{}
	packet.Grow(128)
	packet.WriteString("$ERserver:unknown:")
	codeBuf := make([]byte, 0, 8)
	codeBuf = strconv.AppendInt(codeBuf, int64(code), 10)
	packet.Write(codeBuf)
	packet.WriteString("::")
	packet.WriteString(message)
	packet.WriteString("\r\n")

	return c.send(packet.String())
}

// send sends a packet string to a Client.
// This call queues the packet in the Client's outbound send channel.
// This call will block until the packet can be queued in the send channel.
// Returns a context error if the Client's context has elapsed.
func (c *Client) send(packet string) (err error) {
	select {
	case c.sendChan <- packet:
		return
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

func (s *Server) eventLoop(client *Client) {
	defer client.cancelCtx()

	go client.senderWorker()

	for {
		if !client.scanner.Scan() {
			return
		}

		// Reference the next packet
		packet := client.scanner.Bytes()
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

func (c *Client) latLon() [2]float64 {
	return [2]float64{c.lat.Load(), c.lon.Load()}
}
