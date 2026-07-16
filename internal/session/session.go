// Package session owns a connected FSD participant after login.
//
// This package must not import postoffice, internal/server, or any
// higher-level orchestration — server depends on session, not the reverse.
package session

import (
	"bufio"
	"context"
	"net"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
	"go.uber.org/atomic"
)

// Sender is anything that can enqueue outbound FSD text.
type Sender interface {
	Send(packet string) error
}

// Auth is the optional VATSIM client-auth challenge state for a session.
// Implemented by fsd's vatsimAuthState (auth remains in fsd until PR4).
type Auth interface {
	Initialize(clientID uint16, initialChallenge []byte) error
	IsInitialized() bool
	GetResponseForChallenge(challenge []byte) [32]byte
	UpdateState(d *[32]byte)
}

// LoginData holds the data extracted from the client's login packets.
type LoginData struct {
	ClientChallenge  string                 // Optional client challenge for authentication
	Callsign         string                 // Callsign of the client
	CID              int                    // Cert ID
	RealName         string                 // Real name
	NetworkRating    protocol.NetworkRating // Network rating of the client
	MaxNetworkRating protocol.NetworkRating // Maximum allowed network rating (from DB/JWT)
	ProtoRevision    int                    // Protocol revision
	LoginTime        time.Time              // Time of login
	ClientID         uint16                 // Client ID (from ident packet)
	IsAtc            bool                   // True if the client is ATC, false if a pilot
}

// LatLon is a geographic coordinate pair stored in Session.Coords.
type LatLon struct {
	Lat, Lon float64
}

// Session is a connected FSD participant after successful login.
//
// # Field ownership
//
// Writer: owning read loop only (eventLoop / packet handlers on this connection).
// Concurrent readers without extra sync are allowed for the fields below where
// noted; they may observe a torn value — acceptable for snapshot/privilege checks:
//   - FacilityType — writer: read loop (handleATCPosition); concurrent readers
//     (HTTP online-users snapshot, CQ ATC checks on other sessions) may observe a
//     torn int; acceptable. Switch to atomic later if stronger consistency is needed.
//   - SendFastEnabled, ClosestVelocityClientDistance — read-loop only; not read
//     across sessions.
//   - Auth (Initialize / challenge handling) — read-loop only.
//   - Scanner — owned exclusively by the read loop.
//
// Atomic / concurrent-safe (may be read by postoffice, HTTP service, or other
// session goroutines; writers are typically the owning read loop or postoffice
// position updates):
//   - Coords (via LatLon/SetLatLon), VisRange
//   - FlightPlan, AssignedBeaconCode
//   - Frequency (ATC % position field 1; stored by handleATCPosition), Altitude,
//     Groundspeed, Transponder, Heading, LastUpdated
//
// Immutable after login (set during login; safe to read concurrently afterward):
//   - LoginData fields (Callsign, CID, RealName, NetworkRating, ProtoRevision, …)
//   - MaxNetworkRating is fixed once authentication completes
//
// Context / outbound path:
//   - Ctx, Cancel — lifecycle; Cancel is safe from any goroutine
//   - sendChan — private; producers must call Send; only SenderWorker writes to Conn
//
// # Outbound I/O rule
//
// After login, all packet writes to the client MUST go through Send → sendChan →
// SenderWorker. Direct Conn.Write outside SenderWorker is forbidden post-login
// (login-phase errors may still use protocol.WriteError on the raw connection
// before SenderWorker is started). Conn remains exported for RemoteAddr and
// login-phase WriteError; do not use Conn.Write after SenderWorker starts.
// Future CI may allowlist only SenderWorker + login-phase files for Conn.Write.
type Session struct {
	// Conn is the underlying network connection.
	// Exported for RemoteAddr and login-phase protocol.WriteError only.
	// Post-login packet writes MUST use Send, not Conn.Write.
	Conn     net.Conn
	Scanner  *bufio.Scanner
	Ctx      context.Context
	Cancel   context.CancelFunc
	sendChan chan string

	// Coords stores LatLon; use LatLon/SetLatLon.
	Coords                        atomic.Value
	VisRange                      atomic.Float64
	ClosestVelocityClientDistance float64 // Closest Velocity-compatible client distance in meters

	FlightPlan         atomic.String
	AssignedBeaconCode atomic.String

	Frequency   atomic.String // ATC frequency (raw % field 1, e.g. "28550")
	Altitude    atomic.Int32  // Pilot altitude
	Groundspeed atomic.Int32  // Pilot ground speed
	Transponder atomic.String // Active pilot transponder
	Heading     atomic.Int32  // Pilot heading
	LastUpdated atomic.Time   // Last position/state update time

	// FacilityType is ATC facility type (ATC only). Writer: read loop.
	// Concurrent snapshot readers may see a torn int; acceptable.
	FacilityType int
	LoginData

	Auth            Auth // Optional; set by server when client auth is used
	SendFastEnabled bool

	// sendEnqueueObs is an optional test/stress hook invoked after a successful
	// enqueue on sendChan (not under any postoffice lock). nil is a no-op.
	sendEnqueueObs SendEnqueueObserver
}

// SendEnqueueObserver is invoked after a packet is successfully enqueued on a
// session's outbound channel. Used by stress tests to measure handler-to-enqueue
// timing. Implementations must not block or call back into session/postoffice
// under lock.
type SendEnqueueObserver func(callsign string, enqueuedAt time.Time, queueDepth int)

// New constructs a Session with a cancellable child context and outbound buffer.
// conn may be nil in unit tests that only exercise Send/state.
func New(ctx context.Context, conn net.Conn, scanner *bufio.Scanner, data LoginData) *Session {
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &Session{
		Conn:      conn,
		Scanner:   scanner,
		Ctx:       sessionCtx,
		Cancel:    cancel,
		sendChan:  make(chan string, 32),
		LoginData: data,
	}
	s.SetLatLon(0, 0)
	return s
}

// SenderWorker drains sendChan and writes packets to Conn until the context ends
// or a write fails. It is the only post-login code path allowed to Conn.Write.
// On exit it closes Conn and cancels the session context.
func (s *Session) SenderWorker() {
	if s.Conn != nil {
		defer s.Conn.Close()
	}
	defer s.Cancel()

	for {
		select {
		case packet := <-s.sendChan:
			if s.Conn == nil {
				continue
			}
			if _, err := s.Conn.Write([]byte(packet)); err != nil {
				return
			}
		case <-s.Ctx.Done():
			return
		}
	}
}

// SendError enqueues an FSD $ER packet via the outbound send channel.
// Thread-safe; must only be used after SenderWorker is running (post-login).
func (s *Session) SendError(code int, message string) error {
	return s.Send(protocol.FormatError(protocol.ErrorCode(code), message))
}

// Send queues a packet on the session's outbound channel.
// Blocks until the packet is queued or the session context is done.
func (s *Session) Send(packet string) error {
	select {
	case s.sendChan <- packet:
		if obs := s.sendEnqueueObs; obs != nil {
			// queueDepth is approximate (len after enqueue); safe for stress only.
			obs(s.Callsign, time.Now(), len(s.sendChan))
		}
		return nil
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	}
}

// SetSendEnqueueObserver installs a test/stress hook for successful Send enqueues.
// Pass nil to clear. Not safe to call concurrently with Send.
func (s *Session) SetSendEnqueueObserver(obs SendEnqueueObserver) {
	s.sendEnqueueObs = obs
}

// LatLon returns the current [lat, lon] coordinates.
func (s *Session) LatLon() [2]float64 {
	ll := s.Coords.Load().(LatLon)
	return [2]float64{ll.Lat, ll.Lon}
}

// SetLatLon stores the current coordinates atomically.
func (s *Session) SetLatLon(lat, lon float64) {
	s.Coords.Store(LatLon{Lat: lat, Lon: lon})
}

// DequeueOutbound non-blockingly takes one queued outbound packet.
// Intended for unit tests that assert on enqueued wire text without a Conn.
func (s *Session) DequeueOutbound() (packet string, ok bool) {
	select {
	case packet = <-s.sendChan:
		ok = true
	default:
	}
	return
}
