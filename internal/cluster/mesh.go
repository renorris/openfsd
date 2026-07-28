// Package cluster implements the optional multi-node FSD mesh: framing,
// membership, owner-node callsign claims, directory, interest, HomeRPC,
// and position forwarding.
//
// Import rules (AGENTS.md): may import internal/geo and stdlib only.
// Must not import server, web, db, postoffice, sweatbox, metar, serviceapi.
package cluster

import (
	"context"
	"errors"
	"time"
)

// Common mesh errors.
var (
	ErrClaimInUse      = errors.New("cluster: callsign claim in use")
	ErrClaimNotFound   = errors.New("cluster: claim not found")
	ErrClaimFence      = errors.New("cluster: fence mismatch or expired")
	ErrClaimTimeout    = errors.New("cluster: claim RPC timeout")
	ErrPeerDown        = errors.New("cluster: peer unreachable")
	ErrNotRoutable     = errors.New("cluster: callsign not routable")
	ErrHomeRPCNotFound = errors.New("cluster: home RPC target not found")
	ErrHomeRPCConflict = errors.New("cluster: home RPC conflict")
	ErrHomeRPCTimeout  = errors.New("cluster: home RPC timeout")
	ErrDirectNotFound  = errors.New("cluster: direct target not found")
	ErrMeshNotStarted  = errors.New("cluster: mesh not started")
	ErrInvalidConfig   = errors.New("cluster: invalid config")
)

// Mesh is the inter-node fabric surface used by HybridRegistry / handlers.
type Mesh interface {
	Start(ctx context.Context) error
	Stop() error
	NodeID() string
	// PeerIDs returns configured ring peer IDs (sorted stable ring).
	PeerIDs() []string

	// OwnerNode returns the consistent-hash owner for callsign.
	OwnerNode(callsign string) string

	// Claim protocol (owner-node).
	ClaimReserve(ctx context.Context, callsign string, meta ClaimMeta) (fence string, err error)
	ClaimCommit(ctx context.Context, callsign, fence string) error
	ClaimAbort(callsign, fence string)
	ClaimRelease(callsign, fence string)

	// Directory lookup — only returns ok when routable=true.
	Lookup(callsign string) (nodeID string, meta DirMeta, ok bool)

	// Wire delivery (must be non-blocking enqueue on production TCP mesh — KD-12).
	SendDirect(callsign string, wire []byte) error
	// ForwardRanged enqueues a ranged position packet. senderCallsign required for $SF.
	// Do NOT use for text (R2-5) — use ForwardTextRanged / BroadcastClass.
	ForwardRanged(wire []byte, senderBoxes []AABB, class RangeClass, senderCallsign string)
	// ForwardTextRanged sends text/ranged chat without position coalesce (reliable queue).
	ForwardTextRanged(wire []byte, senderBoxes []AABB)
	PublishInterest(sum InterestSummary)
	BroadcastJoinLeave(wire []byte)
	BroadcastClass(wire []byte, class BroadcastClass)
	// SendProximityHint notifies the home node of a remote closest distance for $SF.
	SendProximityHint(homeNode, targetCallsign string, distanceM float64)

	// Home-node mutations / queries.
	// payload for ForceDisconnect should include originator network rating (binary u32).
	HomeRPC(ctx context.Context, callsign string, op HomeOp, payload []byte) ([]byte, error)

	// Local hooks: invoke when this node hosts a session lifecycle event
	// that must update the local directory view (post-Commit join / leave).
	// For local-owner claims these are applied by the claim owner path.
	NotifyLocalJoin(meta DirMeta)
	NotifyLocalLeave(callsign string)
	NotifyLocalFPL(callsign, fplInfo string)
	NotifyLocalBeacon(callsign, beacon string)

	// OnPeerDead registers a callback for hard-dead peers after grace.
	// metas lists directory entries (with IsATC) that lived on the dead peer,
	// captured BEFORE directory remove so survivors can inject #DA/#DP correctly.
	OnPeerDead(fn func(nodeID string, metas []DirMeta))

	// OnDirectWire registers a handler for inbound DirectPacket / JoinLeave
	// wire destined for local re-fan / local clients.
	OnDirectWire(fn func(fromNode string, wire []byte, class WireClass))
	// OnHomeRPC registers the local home-node RPC handler.
	OnHomeRPC(fn func(op HomeOp, callsign string, payload []byte) (resp []byte, err error))
	// OnPositionBatch registers re-fan handler for remote positions.
	OnPositionBatch(fn func(fromNode string, batch PositionBatch))
	// OnProximityHint is used by $SF multi-peer path.
	OnProximityHint(fn func(fromNode string, targetCallsign string, distanceM float64))

	// SetLocalInterest is invoked by the node to publish interest (also triggers mesh send).
	// SetRemoteClosest is not on Mesh — server owns remoteHintMap.

	// PeerDeathGrace returns the configured peer-death grace period.
	PeerDeathGrace() time.Duration
	// ClaimTimeout returns the claim RPC timeout.
	ClaimTimeout() time.Duration
}

// ClaimMeta accompanies ClaimReserve.
type ClaimMeta struct {
	NodeID string
	CID    int
	IsATC  bool
	Fence  string // optional pre-chosen fence; empty → generate
	TTL    time.Duration
}

// DirMeta is directory metadata for a routable callsign.
// Fence is intentionally omitted from mesh directory broadcasts (security:
// ClaimRelease must only be issued by the claim holder with secret fence).
type DirMeta struct {
	Callsign       string
	NodeID         string
	CID            int
	IsATC          bool
	FPLInfo        string
	AssignedBeacon string
	// Fence is local-only / owner-only; never required for remote Lookup.
	Fence    string
	Routable bool
	// Optional last known geo (rate-limited; not used for routing).
	Lat, Lon float64
}

// SanitizeMeshString strips CR/LF and truncates oversize mesh text (anti injection).
func SanitizeMeshString(s string, max int) string {
	if max <= 0 {
		max = 4096
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' {
			continue
		}
		b = append(b, c)
		if len(b) >= max {
			break
		}
	}
	return string(b)
}

// SanitizeWireBytes strips bare CR/LF interior bytes but keeps a single trailing CRLF if present.
func SanitizeWireBytes(wire []byte) []byte {
	if len(wire) == 0 {
		return wire
	}
	// Preserve final \r\n for FSD packets.
	suffix := []byte{}
	if len(wire) >= 2 && wire[len(wire)-2] == '\r' && wire[len(wire)-1] == '\n' {
		suffix = []byte{'\r', '\n'}
		wire = wire[:len(wire)-2]
	}
	out := make([]byte, 0, len(wire)+2)
	for _, c := range wire {
		if c == '\r' || c == '\n' {
			continue
		}
		out = append(out, c)
	}
	return append(out, suffix...)
}

// HomeOp enumerates HomeRPC operations.
type HomeOp uint8

const (
	HomeOpMutateFlightPlan HomeOp = iota + 1
	HomeOpAssignBeacon
	HomeOpQuerySessionMeta
	HomeOpForceDisconnect
)

// RangeClass classifies ranged forwards.
type RangeClass uint8

const (
	RangeClassPosition RangeClass = iota
	RangeClassVelocity
	RangeClassText
	RangeClassOther
)

// BroadcastClass for flood messages.
type BroadcastClass uint8

const (
	BroadcastJoinLeave BroadcastClass = iota
	BroadcastSupervisor
	BroadcastAll
	BroadcastATC
)

// WireClass for inbound wire delivery callbacks.
type WireClass uint8

const (
	WireDirect WireClass = iota
	WireJoinLeave
	WireBroadcast
	WireRanged
)

// AABB is an axis-aligned geographic bounding box (degrees).
type AABB struct {
	MinLat, MinLon float64
	MaxLat, MaxLon float64
}

// InterestSummary is published ~1 Hz or on dirty coalesce.
type InterestSummary struct {
	NodeID string
	Boxes  []AABB
}

// PositionBatch is a coalesced set of position/ranged packets for one peer.
type PositionBatch struct {
	SenderCallsign string
	SenderBoxes    []AABB
	// Velocity is true for ^ / #ST / #SL style.
	Velocity bool
	// Wires are complete FSD packet lines (with CRLF).
	Wires [][]byte
	// ClosestDistanceM optional proximity for $SF (0 = unknown).
	ClosestDistanceM float64
}
