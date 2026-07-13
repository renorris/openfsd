package fsdclient

import (
	"context"
	"strconv"

	"github.com/renorris/openfsd/pkg/protocol"
)

// defaultProtoRevision is used when PilotLogin/ATCLogin leave ProtoRevision zero.
// Protocol 100 is the classic pilot (@ only) revision.
const defaultProtoRevision = 100

// PilotLogin holds fields for a pilot (#AP) login.
// Token is sent as the plaintext token field (password or JWT).
//
// When ClientIdent is non-nil, a $ID packet is sent before #AP. Empty
// Callsign/CID on ClientIdent are filled from the login fields.
type PilotLogin struct {
	Callsign      string
	CID           string
	Token         string
	NetworkRating protocol.NetworkRating
	ProtoRevision int // 0 => 100
	SimulatorType int
	RealName      string
	ClientIdent   *protocol.ClientIdent // optional $ID
}

// ATCLogin holds fields for an ATC (#AA) login.
// Token is sent as the plaintext token field (password or JWT).
type ATCLogin struct {
	Callsign      string
	CID           string
	Token         string
	NetworkRating protocol.NetworkRating
	ProtoRevision int // 0 => 100
	RealName      string
	ClientIdent   *protocol.ClientIdent // optional $ID
}

// LoginPilot sends optional $ID then #AP under writeMu so concurrent Send
// cannot interleave, and only one login succeeds (CAS on loggedIn).
// Token may be a password or JWT.
func (c *Client) LoginPilot(ctx context.Context, p PilotLogin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Callsign == "" {
		return errf("fsdclient: pilot login: empty callsign")
	}

	protoRev := p.ProtoRevision
	if protoRev == 0 {
		protoRev = defaultProtoRevision
	}

	// Build packets outside the lock.
	var idWire []byte
	if p.ClientIdent != nil {
		id := fillClientIdent(*p.ClientIdent, p.Callsign, p.CID)
		idWire = ensureCRLF(id.Marshal())
	}
	add := protocol.AddPilot{
		Callsign:      p.Callsign,
		To:            "SERVER",
		CID:           p.CID,
		Token:         p.Token,
		NetworkRating: p.NetworkRating,
		ProtoRevision: protoRev,
		SimulatorType: p.SimulatorType,
		RealName:      p.RealName,
	}
	apWire := ensureCRLF(add.Marshal())

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed.Load() {
		return ErrClosed
	}
	if c.conn == nil {
		return ErrNotDialed
	}
	if !c.loggedIn.CompareAndSwap(false, true) {
		return ErrAlreadyLoggedIn
	}

	if idWire != nil {
		if err := c.sendLocked(idWire); err != nil {
			c.loggedIn.Store(false)
			return err
		}
	}
	if err := c.sendLocked(apWire); err != nil {
		c.loggedIn.Store(false)
		return err
	}

	c.callsign.Store(p.Callsign)
	c.isATC.Store(false)
	return nil
}

// LoginATC sends optional $ID then #AA under writeMu (same atomicity as LoginPilot).
// Token may be a password or JWT.
func (c *Client) LoginATC(ctx context.Context, p ATCLogin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Callsign == "" {
		return errf("fsdclient: atc login: empty callsign")
	}

	protoRev := p.ProtoRevision
	if protoRev == 0 {
		protoRev = defaultProtoRevision
	}

	var idWire []byte
	if p.ClientIdent != nil {
		id := fillClientIdent(*p.ClientIdent, p.Callsign, p.CID)
		idWire = ensureCRLF(id.Marshal())
	}
	add := protocol.AddATC{
		Callsign:      p.Callsign,
		To:            "SERVER",
		RealName:      p.RealName,
		CID:           p.CID,
		Token:         p.Token,
		NetworkRating: p.NetworkRating,
		ProtoRevision: protoRev,
	}
	aaWire := ensureCRLF(add.Marshal())

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed.Load() {
		return ErrClosed
	}
	if c.conn == nil {
		return ErrNotDialed
	}
	if !c.loggedIn.CompareAndSwap(false, true) {
		return ErrAlreadyLoggedIn
	}

	if idWire != nil {
		if err := c.sendLocked(idWire); err != nil {
			c.loggedIn.Store(false)
			return err
		}
	}
	if err := c.sendLocked(aaWire); err != nil {
		c.loggedIn.Store(false)
		return err
	}

	c.callsign.Store(p.Callsign)
	c.isATC.Store(true)
	return nil
}

func fillClientIdent(id protocol.ClientIdent, callsign, cid string) protocol.ClientIdent {
	if id.Callsign == "" {
		id.Callsign = callsign
	}
	if id.To == "" {
		id.To = "SERVER"
	}
	if id.CID == 0 && cid != "" {
		if n, err := strconv.Atoi(cid); err == nil {
			id.CID = n
		}
	}
	// HasChallengeKey follows ChallengeKey when set by caller.
	if id.ChallengeKey != "" {
		id.HasChallengeKey = true
	}
	return id
}
