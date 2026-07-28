package server

import (
	"fmt"
	"math"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// wireMeshHandlers registers HomeRPC / wire / position / peer-death callbacks on mesh.
func (s *Server) wireMeshHandlers() {
	if s.mesh == nil {
		return
	}
	s.mesh.OnHomeRPC(s.handleMeshHomeRPC)
	s.mesh.OnDirectWire(s.handleMeshDirectWire)
	s.mesh.OnPositionBatch(s.handleMeshPositionBatch)
	s.mesh.OnPeerDead(s.handleMeshPeerDead)
}

func (s *Server) handleMeshHomeRPC(op cluster.HomeOp, callsign string, payload []byte) ([]byte, error) {
	target, err := s.registry.Find(callsign)
	if err != nil {
		return nil, cluster.ErrHomeRPCNotFound
	}
	switch op {
	case cluster.HomeOpMutateFlightPlan:
		fpl := cluster.SanitizeMeshString(string(payload), 2048)
		target.FlightPlan.Store(fpl)
		if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
			hr.Mesh().NotifyLocalFPL(callsign, fpl)
		}
		return nil, nil
	case cluster.HomeOpAssignBeacon:
		code := cluster.SanitizeMeshString(string(payload), 8)
		if isValidBeaconCode(code) {
			target.AssignedBeaconCode.Store(code)
			if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
				hr.Mesh().NotifyLocalBeacon(callsign, code)
			}
		}
		return nil, nil
	case cluster.HomeOpQuerySessionMeta:
		return cluster.EncodeSessionMeta(cluster.SessionMeta{
			FPLInfo: target.FlightPlan.Load(),
			Beacon:  target.AssignedBeaconCode.Load(),
			IsATC:   target.IsAtc,
			Rating:  int(target.NetworkRating),
		}), nil
	case cluster.HomeOpForceDisconnect:
		if target.Synthetic && s.sweatbox != nil {
			_ = s.sweatbox.Remove(target.Callsign)
			return nil, nil
		}
		target.Disconnect()
		return nil, nil
	default:
		return nil, cluster.ErrHomeRPCConflict
	}
}

func (s *Server) handleMeshDirectWire(fromNode string, wire []byte, class cluster.WireClass) {
	wire = cluster.SanitizeWireBytes(wire)
	switch class {
	case cluster.WireDirect:
		recipient := string(getField(wire, 1))
		if recipient == "" {
			return
		}
		if target, err := s.registry.Find(recipient); err == nil {
			_ = target.TrySend(string(wire))
		}
	case cluster.WireJoinLeave:
		s.registry.All(nil, func(sess *session.Session) bool {
			if sess.Synthetic {
				return true
			}
			_ = sess.TrySend(string(wire))
			return true
		})
	case cluster.WireBroadcast:
		// payload: [class byte][wire...] from BroadcastClass encoding
		if len(wire) < 1 {
			return
		}
		bclass := cluster.BroadcastClass(wire[0])
		body := wire[1:]
		s.registry.All(nil, func(sess *session.Session) bool {
			if sess.Synthetic {
				return true
			}
			switch bclass {
			case cluster.BroadcastATC:
				if !sess.IsAtc {
					return true
				}
			case cluster.BroadcastSupervisor:
				if sess.NetworkRating < protocol.NetworkRatingSupervisor {
					return true
				}
			}
			_ = sess.TrySend(string(body))
			return true
		})
	case cluster.WireRanged:
		// Frequency / ranged text: decode sender boxes and re-fan with overlap (R3-4).
		boxes, body, err := cluster.DecodeTextRangedPayload(wire)
		if err != nil {
			// Legacy payload without box prefix — deliver as-is to all.
			body = wire
			boxes = nil
		}
		s.registry.All(nil, func(sess *session.Session) bool {
			if sess.Synthetic {
				return true
			}
			if len(boxes) > 0 && !sessionOverlapsSenderBoxes(sess, boxes) {
				return true
			}
			_ = sess.TrySend(string(body))
			return true
		})
	}
}

func (s *Server) handleMeshPositionBatch(fromNode string, batch cluster.PositionBatch) {
	snaps := s.registry.Snapshot()
	minLocalDist := math.Inf(1)
	for _, wire := range batch.Wires {
		packetStr := string(cluster.SanitizeWireBytes(wire))
		for _, sess := range snaps {
			if sess.Synthetic {
				continue
			}
			if batch.Velocity && sess.ProtoRevision != 101 {
				continue
			}
			if !sessionOverlapsSenderBoxes(sess, batch.SenderBoxes) {
				continue
			}
			_ = sess.SendPosition(packetStr)
			if sess.ProtoRevision == 101 && !sess.IsAtc {
				ll := sess.LatLon()
				if len(batch.SenderBoxes) > 0 {
					b := batch.SenderBoxes[0]
					clat := (b.MinLat + b.MaxLat) / 2
					clon := (b.MinLon + b.MaxLon) / 2
					d := geo.ApproxDistance(ll[0], ll[1], clat, clon)
					if d < minLocalDist {
						minLocalDist = d
					}
				}
			}
		}
	}
	// $SF multi-peer: SendProximityHint to sender's home (fromNode).
	if s.mesh != nil && batch.SenderCallsign != "" && !math.IsInf(minLocalDist, 1) {
		s.mesh.SendProximityHint(fromNode, batch.SenderCallsign, minLocalDist)
	}
}

func sessionOverlapsSenderBoxes(sess *session.Session, boxes []cluster.AABB) bool {
	if len(boxes) == 0 {
		return true
	}
	sMin, sMax := sess.VisBox()
	for _, b := range boxes {
		if geo.AABBOverlap(sMin, sMax,
			[2]float64{b.MinLat, b.MinLon},
			[2]float64{b.MaxLat, b.MaxLon}) {
			return true
		}
	}
	ok := false
	sess.EachSecondaryVisBox(func(min, max [2]float64) {
		for _, b := range boxes {
			if geo.AABBOverlap(min, max,
				[2]float64{b.MinLat, b.MinLon},
				[2]float64{b.MaxLat, b.MaxLon}) {
				ok = true
				return
			}
		}
	})
	return ok
}

func (s *Server) handleMeshPeerDead(nodeID string, metas []cluster.DirMeta) {
	// metas captured with IsATC before directory remove (issue 6).
	for _, meta := range metas {
		var pkt string
		if meta.IsATC {
			pkt = fmt.Sprintf("#DA%s:SERVER:0\r\n", meta.Callsign)
		} else {
			pkt = fmt.Sprintf("#DP%s:SERVER:0\r\n", meta.Callsign)
		}
		s.registry.All(nil, func(sess *session.Session) bool {
			if sess.Synthetic {
				return true
			}
			_ = sess.TrySend(pkt)
			return true
		})
	}
	s.logger.Info("cluster peer dead: synthetic leave flood", "node", nodeID, "n", len(metas))
}
