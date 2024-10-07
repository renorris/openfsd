package client

import (
	"errors"
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/protocol/vatsimauth"
)

type FSDClient struct {
	connection *Connection

	authVerify       *vatsimauth.VatsimAuth // Auth state to verify browser's auth responses
	pendingChallenge string                 // store the pending challenge sent to the browser
	authSelf         *vatsimauth.VatsimAuth // Auth state for interrogating browser

	// General information
	callsign      string
	cid           int
	networkRating protocol.NetworkRating
	pilotRating   protocol.PilotRating
	realName      string
	simulatorType int

	spatialState *spatialState

	currentGeohash  postoffice.Geohash
	sendFastEnabled bool

	kill    chan string // Signal to disconnect this client
	mailbox chan string // Incoming messages
}

func NewFSDClient(connection *Connection, clientIdentPDU *protocol.ClientIdentificationPDU,
	addPilotPDU *protocol.AddPilotPDU, initialServerChallenge string, pilotRating protocol.PilotRating) *FSDClient {

	client := FSDClient{
		connection: connection,

		authVerify: vatsimauth.NewVatsimAuth(
			clientIdentPDU.ClientID,
			vatsimauth.Keys[clientIdentPDU.ClientID]),
		authSelf: vatsimauth.NewVatsimAuth(
			clientIdentPDU.ClientID,
			vatsimauth.Keys[clientIdentPDU.ClientID]),

		callsign:      clientIdentPDU.From,
		cid:           clientIdentPDU.CID,
		networkRating: addPilotPDU.NetworkRating,
		pilotRating:   pilotRating,
		simulatorType: addPilotPDU.SimulatorType,
		realName:      addPilotPDU.RealName,

		spatialState: &spatialState{},

		kill:    make(chan string, 1),
		mailbox: make(chan string, 32),
	}

	client.authSelf.SetInitialChallenge(clientIdentPDU.InitialChallenge)
	client.authVerify.SetInitialChallenge(initialServerChallenge)

	return &client
}

// postoffice.Address implementations:

func (c *FSDClient) Name() string {
	return c.callsign
}

func (c *FSDClient) SendMail(packet string) {
	// Non-blocking send
	select {
	case c.mailbox <- packet:
	default:
	}
}

func (c *FSDClient) SendKill(packet string) error {
	// Non-blocking send
	select {
	case c.kill <- packet:
		return nil
	default:
		return errors.New("client unavailable")
	}
}

func (c *FSDClient) NetworkRating() protocol.NetworkRating {
	return c.networkRating
}

func (c *FSDClient) Geohash() postoffice.Geohash {
	return c.currentGeohash
}

func (c *FSDClient) State() postoffice.AddressState {
	state := postoffice.AddressState{
		CID:         c.cid,
		RealName:    c.realName,
		PilotRating: c.pilotRating,
	}

	c.spatialState.lock.RLock()

	state.Latitude = c.spatialState.latitude
	state.Longitude = c.spatialState.longitude
	state.Altitude = c.spatialState.altitude
	state.Groundspeed = c.spatialState.groundspeed
	state.Transponder = c.spatialState.transponder
	state.Heading = c.spatialState.heading
	state.LastUpdated = c.spatialState.lastUpdated

	c.spatialState.lock.RUnlock()

	return state
}

func (c *FSDClient) SetAddressState(state *postoffice.AddressState) {
	c.spatialState.lock.Lock()

	c.spatialState.latitude = state.Latitude
	c.spatialState.longitude = state.Longitude
	c.spatialState.groundspeed = state.Groundspeed
	c.spatialState.altitude = state.Altitude
	c.spatialState.heading = state.Heading
	c.spatialState.transponder = state.Transponder
	c.spatialState.lastUpdated = state.LastUpdated

	c.spatialState.lock.Unlock()
}

// handler.Invoker implementations:

func (c *FSDClient) Callsign() string {
	return c.callsign
}

func (c *FSDClient) AuthSelf() *vatsimauth.VatsimAuth {
	return c.authSelf
}

func (c *FSDClient) AuthVerify() *vatsimauth.VatsimAuth {
	return c.authVerify
}

func (c *FSDClient) PendingChallenge() string {
	return c.pendingChallenge
}

func (c *FSDClient) SetPendingChallenge(s string) {
	c.pendingChallenge = s
}

func (c *FSDClient) CID() int {
	return c.cid
}

func (c *FSDClient) SetGeohash(h postoffice.Geohash) {
	c.currentGeohash = h
}

func (c *FSDClient) SendFastEnabled() bool {
	return c.sendFastEnabled
}

func (c *FSDClient) SetSendFastEnabled(b bool) {
	c.sendFastEnabled = b
}

func (c *FSDClient) Address() postoffice.Address {
	return c
}

func (c *FSDClient) RemoteNetworkAddrString() string {
	return c.connection.conn.RemoteAddr().String()
}
