package server

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/session"
)

// homeRPCTimeout bounds worker HomeRPC waits (off gnet).
const homeRPCTimeout = 400 * time.Millisecond

// meshHomeRPC runs HomeRPC entirely off the gnet loop via authPool.
// onOK / onErr run on the worker (Session.Send is concurrent-safe).
// R3-8: if the pool is full, fail closed (onErr) — never sync-block gnet.
func (s *Server) meshHomeRPC(client *session.Session, callsign string, op cluster.HomeOp, payload []byte, onOK func([]byte), onErr func(error)) {
	hr, ok := s.registry.(*HybridRegistry)
	if !ok || hr.Mesh() == nil {
		if onErr != nil {
			onErr(cluster.ErrHomeRPCNotFound)
		}
		return
	}
	job := func() {
		ctx, cancel := context.WithTimeout(context.Background(), homeRPCTimeout)
		defer cancel()
		resp, err := hr.MeshHomeRPC(ctx, callsign, op, payload)
		if err != nil {
			if onErr != nil {
				onErr(err)
			}
			return
		}
		if onOK != nil {
			onOK(resp)
		}
	}
	if s.authPool == nil {
		if onErr != nil {
			onErr(cluster.ErrMeshNotStarted)
		}
		return
	}
	select {
	case s.authPool <- job:
		return
	default:
		// Fail closed — do not block gnet (R3-8).
		if onErr != nil {
			onErr(cluster.ErrHomeRPCTimeout)
		}
	}
}

// meshForceDisconnectAsync kills a remote callsign via HomeRPC (supervisor rating prepended).
func (s *Server) meshForceDisconnectAsync(client *session.Session, callsign string) {
	var pl [4]byte
	binary.BigEndian.PutUint32(pl[:], uint32(client.NetworkRating))
	s.meshHomeRPC(client, callsign, cluster.HomeOpForceDisconnect, pl[:],
		nil,
		func(err error) {
			if client != nil {
				client.SendError(NoSuchCallsignError, "No such callsign")
			}
		},
	)
}
