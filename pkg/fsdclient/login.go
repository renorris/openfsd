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

// LoginPilot sends optional $ID then #AP. Token may be a password or JWT.
func (c *Client) LoginPilot(ctx context.Context, p PilotLogin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed.Load() {
		return ErrClosed
	}
	if c.conn == nil {
		return ErrNotDialed
	}
	if c.loggedIn.Load() {
		return ErrAlreadyLoggedIn
	}
	if p.Callsign == "" {
		return errf("fsdclient: pilot login: empty callsign")
	}

	protoRev := p.ProtoRevision
	if protoRev == 0 {
		protoRev = defaultProtoRevision
	}

	if p.ClientIdent != nil {
		id := fillClientIdent(*p.ClientIdent, p.Callsign, p.CID)
		if err := c.Send(id.Marshal()); err != nil {
			return err
		}
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
	if err := c.Send(add.Marshal()); err != nil {
		return err
	}

	c.callsign.Store(p.Callsign)
	c.isATC.Store(false)
	c.loggedIn.Store(true)
	return nil
}

// LoginATC sends optional $ID then #AA. Token may be a password or JWT.
func (c *Client) LoginATC(ctx context.Context, p ATCLogin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed.Load() {
		return ErrClosed
	}
	if c.conn == nil {
		return ErrNotDialed
	}
	if c.loggedIn.Load() {
		return ErrAlreadyLoggedIn
	}
	if p.Callsign == "" {
		return errf("fsdclient: atc login: empty callsign")
	}

	protoRev := p.ProtoRevision
	if protoRev == 0 {
		protoRev = defaultProtoRevision
	}

	if p.ClientIdent != nil {
		id := fillClientIdent(*p.ClientIdent, p.Callsign, p.CID)
		if err := c.Send(id.Marshal()); err != nil {
			return err
		}
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
	if err := c.Send(add.Marshal()); err != nil {
		return err
	}

	c.callsign.Store(p.Callsign)
	c.isATC.Store(true)
	c.loggedIn.Store(true)
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
