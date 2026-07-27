// Package session owns a connected FSD participant after login.
//
// This package must not import postoffice, internal/server, or any
// higher-level orchestration — server depends on session, not the reverse.
package session

import (
	"bufio"
	"context"
	"math"
	"net"
	"time"
	"unsafe"

	"github.com/renorris/openfsd/pkg/protocol"
	"go.uber.org/atomic"
)

// Sender is anything that can enqueue outbound FSD text.
type Sender interface {
	Send(packet string) error
}

// Auth is the optional VATSIM client-auth challenge state for a session.
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
	// PilotRating is the certificate pilot rating wire ID set at auth
	// (0,1,3,7,15,31,63). Immutable after login; 0 for synthetic/unknown.
	PilotRating   int
	ProtoRevision int       // Protocol revision
	LoginTime     time.Time // Time of login
	ClientID      uint16    // Client ID (from ident packet)
	IsAtc         bool      // True if the client is ATC, false if a pilot
}

// LatLon is a geographic coordinate pair (API convenience; storage is non-boxing atomics).
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
//   - SendFastEnabled, ClosestVelocityClientDistance — read-loop only; not read
//     across sessions (except ClosestVelocity, written by postoffice Search under
//     sweatbox wireMu when host-originated).
//   - Auth (Initialize / challenge handling) — read-loop only.
//   - Scanner — owned exclusively by the read loop.
//
// Atomic / concurrent-safe (may be read by postoffice, HTTP service, or other
// session goroutines; writers are typically the owning read loop or postoffice
// position updates):
//   - LatLon / SetLatLon / SetGeo / VisRange / VisBox (non-boxing float atomics)
//   - FlightPlan, AssignedBeaconCode
//   - Frequency (ATC % position field 1; stored by handleATCPosition), FacilityType,
//     Altitude, Groundspeed, Transponder, Heading, LastUpdated
//
// Immutable after login (set during login; safe to read concurrently afterward):
//   - LoginData fields (Callsign, CID, RealName, NetworkRating, ProtoRevision, …)
//   - MaxNetworkRating / PilotRating fixed once authentication completes
//   - Synthetic — set before Register for in-process (sweatbox) participants;
//     never set on the TCP login path; concurrent readers OK
//
// Context / outbound path:
//   - Ctx, Cancel — lifecycle; Cancel is safe from any goroutine
//   - sendChan — private channel path used when Outbound is nil
//   - Outbound — optional event-driven sink (gnet); when set, Send/SendPosition
//     route there and SenderWorker must not run for this session
//
// # Outbound I/O rule
//
// After login, all packet writes to the client MUST go through Send / SendPosition.
// Two sinks are supported:
//  1. Channel path: Send → sendChan → SenderWorker → Conn.Write (classic / synthetic).
//  2. Outbound path: Send → Outbound (coalesced AsyncWrite; no per-conn writer goroutine).
//
// Direct Conn.Write outside SenderWorker is forbidden post-login on the channel path
// (login-phase errors may still use protocol.WriteError on the raw connection
// before the post-login sink is active). Conn remains exported for RemoteAddr and
// login-phase WriteError.
//
// Synthetic sessions typically have Conn == nil and Outbound == nil. Fan-out helpers
// skip them as recipients; direct registry.Send still enqueues and relies on
// SenderWorker drain (no network write when Conn is nil).
type Session struct {
	// Conn is the underlying network connection (classic net path).
	// Exported for RemoteAddr and login-phase protocol.WriteError only.
	// Post-login packet writes MUST use Send, not Conn.Write.
	// May be nil for synthetic / unit-test / gnet sessions; use RemoteIP().
	Conn     net.Conn
	Scanner  *bufio.Scanner
	Ctx      context.Context
	Cancel   context.CancelFunc
	sendChan chan string

	// outbound, when non-nil, replaces sendChan+SenderWorker for post-login writes.
	// Set once after login (gnet path); not changed concurrently with Send.
	outbound Outbound

	// remoteIP is a cached host string for RemoteIP() when Conn is nil (gnet).
	remoteIP string

	// Geographic state (non-boxing atomics — hot path for postoffice Search).
	// Prefer SetGeo / SetLatLon / SetVisRange so VisBox stays in sync.
	latBits  atomic.Uint64 // math.Float64bits(lat)
	lonBits  atomic.Uint64 // math.Float64bits(lon)
	VisRange atomic.Float64
	// Cached visibility AABB from last SetGeo/SetLatLon/SetVisRange (float bits).
	boxMinLatBits atomic.Uint64
	boxMinLonBits atomic.Uint64
	boxMaxLatBits atomic.Uint64
	boxMaxLonBits atomic.Uint64

	// secVis holds secondary ATC visibility centers (SECPOS). Nil when none.
	// Writer: owning read loop (ClearSecondaryVisCenters / SetSecondaryVisCenter).
	// Concurrent Search readers load the pointer; may observe a stale snapshot.
	secVis atomic.Pointer[secVisSnapshot]

	ClosestVelocityClientDistance float64 // Closest Velocity-compatible client distance in meters

	FlightPlan         atomic.String
	AssignedBeaconCode atomic.String

	Frequency   atomic.String // ATC frequency (raw % field 1, e.g. "28550")
	Altitude    atomic.Int32  // Pilot altitude
	Groundspeed atomic.Int32  // Pilot ground speed
	Transponder atomic.String // Active pilot transponder
	Heading     atomic.Int32  // Pilot heading
	LastUpdated atomic.Time   // Last position/state update time

	// FacilityType is ATC facility type (ATC only). Writer: read loop
	// (handleATCPosition); concurrent readers (HTTP online-users, CQ ATC).
	FacilityType atomic.Int32
	LoginData

	Auth            Auth // Optional; set by server when client auth is used
	SendFastEnabled bool

	// Synthetic marks an in-process participant (e.g. sweatbox) that registers
	// in the postoffice without a real TCP client. False for normal clients.
	// Set before Register; immutable afterward. Concurrent readers OK.
	// Server fan-out helpers skip Synthetic recipients; direct Send still works
	// and is drained by SenderWorker (drop when Conn is nil).
	Synthetic bool

	// DisconnectNotified is set when a leave broadcast (#DA/#DP) has already been
	// sent for this session (client-sent delete or connection-exit path).
	// Prevents double leave notifications. Any goroutine may CAS it.
	DisconnectNotified atomic.Bool

	// Inbound rate-limit timestamps (UnixNano). Concurrent-safe atomics.
	LastPosRateNs    atomic.Int64
	LastTextRateNs   atomic.Int64
	LastMetarRateNs  atomic.Int64
	LastFPLRateNs    atomic.Int64
	LastWallopRateNs atomic.Int64
	LastQueryRateNs  atomic.Int64
	// LastInboundNs is updated on every post-login packet (idle timeout).
	LastInboundNs atomic.Int64

	// sendEnqueueObs is an optional test/stress hook invoked after a successful
	// enqueue on sendChan (not under any postoffice lock). nil is a no-op.
	sendEnqueueObs SendEnqueueObserver
}

// SendEnqueueObserver is invoked after a packet is successfully enqueued on a
// session's outbound channel. Used by stress tests to measure handler-to-enqueue
// timing. Implementations must not block or call back into session/postoffice
// under lock.
type SendEnqueueObserver func(callsign string, enqueuedAt time.Time, queueDepth int)

// sendChanCap is the outbound buffer depth. Position storms use SendPosition
// (latest-wins) so a slow peer cannot stall the broadcaster.
const sendChanCap = 32

// New constructs a Session with a cancellable child context and outbound buffer.
// conn may be nil in unit tests that only exercise Send/state.
func New(ctx context.Context, conn net.Conn, scanner *bufio.Scanner, data LoginData) *Session {
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &Session{
		Conn:      conn,
		Scanner:   scanner,
		Ctx:       sessionCtx,
		Cancel:    cancel,
		sendChan:  make(chan string, sendChanCap),
		LoginData: data,
	}
	s.SetLatLon(0, 0)
	return s
}

// SetOutbound installs an event-driven post-login sink (gnet path).
// Must be called once after login before any post-login Send; not concurrent with Send.
// When set, do not start SenderWorker for this session.
func (s *Session) SetOutbound(o Outbound) {
	s.outbound = o
}

// Outbound returns the event-driven sink, if any.
func (s *Session) Outbound() Outbound {
	return s.outbound
}

// SetRemoteIP caches the remote host IP for nil-Conn sessions (gnet).
func (s *Session) SetRemoteIP(ip string) {
	s.remoteIP = ip
}

// SenderWorker drains sendChan and writes packets to Conn until the context ends
// or a write fails. Channel-path only: do not run when Outbound is set.
// On exit it closes Conn and cancels the session context.
func (s *Session) SenderWorker() {
	if s.outbound != nil {
		// Event-driven path owns writes; this is a no-op drain for safety.
		<-s.Ctx.Done()
		return
	}
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
			// string → []byte without allocation (packet is not mutated).
			if _, err := s.Conn.Write(unsafeStringBytes(packet)); err != nil {
				return
			}
		case <-s.Ctx.Done():
			return
		}
	}
}

// unsafeStringBytes returns a []byte view of s. The slice must not be mutated.
func unsafeStringBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// SendError enqueues an FSD $ER packet via the outbound send path.
// Thread-safe; must only be used after post-login outbound is active.
func (s *Session) SendError(code int, message string) error {
	return s.Send(protocol.FormatError(protocol.ErrorCode(code), message))
}

// Send queues a reliable packet on the session's outbound path.
// Blocks until the packet is queued or the session context is done.
func (s *Session) Send(packet string) error {
	if o := s.outbound; o != nil {
		if err := s.Ctx.Err(); err != nil {
			return err
		}
		err := o.Send(packet)
		if err != nil {
			if s.Ctx.Err() != nil {
				return s.Ctx.Err()
			}
			return err
		}
		s.fireEnqueueObs()
		return nil
	}
	select {
	case s.sendChan <- packet:
		s.fireEnqueueObs()
		return nil
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	}
}

// TrySend queues a reliable packet without blocking.
// Returns false if the outbound queue is full, the session is done, or the
// sink is closed. Used by fan-out so a slow peer cannot stall gnet event loops.
func (s *Session) TrySend(packet string) bool {
	if err := s.Ctx.Err(); err != nil {
		return false
	}
	if o := s.outbound; o != nil {
		if ts, ok := o.(trySender); ok {
			if err := ts.TrySend(packet); err != nil {
				return false
			}
			s.fireEnqueueObs()
			return true
		}
		// Fallback: non-blocking path unavailable — drop rather than block.
		return false
	}
	select {
	case s.sendChan <- packet:
		s.fireEnqueueObs()
		return true
	case <-s.Ctx.Done():
		return false
	default:
		return false
	}
}

// trySender is implemented by CoalesceOutbound for non-blocking reliable enqueue.
type trySender interface {
	TrySend(packet string) error
}

// Disconnect cancels the session and closes transport sinks.
// Safe from any goroutine; used by kick/kill and limit enforcement.
func (s *Session) Disconnect() {
	if s == nil {
		return
	}
	s.Cancel()
	if o := s.outbound; o != nil {
		_ = o.Close()
	}
	if s.Conn != nil {
		_ = s.Conn.Close()
	}
}

// AllowRate returns true if minInterval has elapsed since the last success
// stored in lastNs (UnixNano atomic). On success, stores now.
func AllowRate(lastNs *atomic.Int64, minInterval time.Duration, now time.Time) bool {
	if lastNs == nil || minInterval <= 0 {
		return true
	}
	nowNs := now.UnixNano()
	minNs := int64(minInterval)
	for {
		prev := lastNs.Load()
		if prev != 0 && nowNs-prev < minNs {
			return false
		}
		if lastNs.CompareAndSwap(prev, nowNs) {
			return true
		}
	}
}

// SendPosition enqueues a high-frequency position (or similar telemetry) packet
// without blocking the broadcaster when the peer is slow.
//
// Channel path — latest-wins when the buffer is full:
//  1. try non-blocking send
//  2. if full, drop one oldest packet and retry once
//  3. if still full, drop the new packet (prefer keeping queued traffic moving)
//
// Outbound path — latest-wins single slot + coalesced flush (see CoalesceOutbound).
//
// Returns nil on drop (soft loss is acceptable for position streams). Returns
// ctx error when the session is shutting down.
func (s *Session) SendPosition(packet string) error {
	// Prefer a fast shutdown path so fan-out does not keep feeding dead peers.
	if err := s.Ctx.Err(); err != nil {
		return err
	}

	if o := s.outbound; o != nil {
		err := o.SendPosition(packet)
		if err != nil {
			if s.Ctx.Err() != nil {
				return s.Ctx.Err()
			}
			// Soft-drop on closed outbound mid-shutdown.
			return nil
		}
		s.fireEnqueueObs()
		return nil
	}

	select {
	case s.sendChan <- packet:
		s.fireEnqueueObs()
		return nil
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	default:
	}

	// Drop oldest to make room for the freshest position.
	select {
	case <-s.sendChan:
	default:
	}

	// Re-check after the drop window; session may have canceled mid-call.
	if err := s.Ctx.Err(); err != nil {
		return err
	}

	select {
	case s.sendChan <- packet:
		s.fireEnqueueObs()
		return nil
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	default:
		// Still full (concurrent producers); drop this update.
		return nil
	}
}

func (s *Session) fireEnqueueObs() {
	if obs := s.sendEnqueueObs; obs != nil {
		// queueDepth is approximate (len after enqueue); safe for stress only.
		obs(s.Callsign, time.Now(), len(s.sendChan))
	}
}

// SetSendEnqueueObserver installs a test/stress hook for successful Send enqueues.
// Pass nil to clear. Not safe to call concurrently with Send.
func (s *Session) SetSendEnqueueObserver(obs SendEnqueueObserver) {
	s.sendEnqueueObs = obs
}

// LatLon returns the current [lat, lon] coordinates.
func (s *Session) LatLon() [2]float64 {
	return [2]float64{
		math.Float64frombits(s.latBits.Load()),
		math.Float64frombits(s.lonBits.Load()),
	}
}

// SetLatLon stores coordinates atomically and refreshes VisBox using current VisRange.
// Secondary center boxes are recomputed with the current VisRange.
func (s *Session) SetLatLon(lat, lon float64) {
	s.latBits.Store(math.Float64bits(lat))
	s.lonBits.Store(math.Float64bits(lon))
	s.refreshVisBox(lat, lon, s.VisRange.Load())
	s.recomputeSecondaryBoxes(s.VisRange.Load())
}

// SetVisRange stores visibility range (meters) and refreshes VisBox.
// Also recomputes secondary center boxes (same range as primary).
func (s *Session) SetVisRange(rangeM float64) {
	s.VisRange.Store(rangeM)
	ll := s.LatLon()
	s.refreshVisBox(ll[0], ll[1], rangeM)
	s.recomputeSecondaryBoxes(rangeM)
}

// SetGeo sets lat, lon, and visibility range together (single VisBox refresh).
// Prefer this over separate SetLatLon + VisRange.Store on hot paths.
// Recomputes secondary center boxes with the new range.
func (s *Session) SetGeo(lat, lon, rangeM float64) {
	s.latBits.Store(math.Float64bits(lat))
	s.lonBits.Store(math.Float64bits(lon))
	s.VisRange.Store(rangeM)
	s.refreshVisBox(lat, lon, rangeM)
	s.recomputeSecondaryBoxes(rangeM)
}

// VisBox returns the cached axis-aligned visibility box [min, max] in degrees.
// Updated by SetLatLon / SetVisRange / SetGeo. Concurrent readers may observe a
// torn combination of edges for a brief window; acceptable for range filtering.
func (s *Session) VisBox() (min, max [2]float64) {
	min = [2]float64{
		math.Float64frombits(s.boxMinLatBits.Load()),
		math.Float64frombits(s.boxMinLonBits.Load()),
	}
	max = [2]float64{
		math.Float64frombits(s.boxMaxLatBits.Load()),
		math.Float64frombits(s.boxMaxLonBits.Load()),
	}
	return min, max
}

// refreshVisBox writes the equirectangular AABB for center/range into atomics.
// Duplicates geo.BoundingBox math to avoid a session → geo import edge.
func (s *Session) refreshVisBox(lat, lon, rangeM float64) {
	minLat, minLon, maxLat, maxLon := aabbAround(lat, lon, rangeM)
	s.boxMinLatBits.Store(math.Float64bits(minLat))
	s.boxMaxLatBits.Store(math.Float64bits(maxLat))
	s.boxMinLonBits.Store(math.Float64bits(minLon))
	s.boxMaxLonBits.Store(math.Float64bits(maxLon))
}

// MaxSecondaryVisCenters is the maximum number of SECPOS secondary centers
// stored per session (zero-based indices 0..Max-1). vatSys UI exposes 4 total
// centers (1 primary + 3 secondary); 4 secondaries leaves headroom.
const MaxSecondaryVisCenters = 4

// secVisSnapshot is an immutable secondary-center set for lock-free Search.
type secVisSnapshot struct {
	// slots[i].valid when index i is set. Sparse indices allowed.
	slots [MaxSecondaryVisCenters]secVisSlot
}

type secVisSlot struct {
	valid          bool
	lat, lon       float64
	minLat, minLon float64
	maxLat, maxLon float64
}

// ClearSecondaryVisCenters drops all SECPOS secondary centers.
// Call on each ATC % position update before the client re-sends secondaries
// (vatSys SendPosition order: % primary, then ' for each secondary).
func (s *Session) ClearSecondaryVisCenters() {
	s.secVis.Store(nil)
}

// SetSecondaryVisCenter stores or replaces secondary center at zero-based index.
// index must be in [0, MaxSecondaryVisCenters). Returns false if out of range.
// Box uses the session's current VisRange (same range as primary).
func (s *Session) SetSecondaryVisCenter(index int, lat, lon float64) bool {
	if index < 0 || index >= MaxSecondaryVisCenters {
		return false
	}
	rangeM := s.VisRange.Load()
	minLat, minLon, maxLat, maxLon := aabbAround(lat, lon, rangeM)

	var next secVisSnapshot
	if prev := s.secVis.Load(); prev != nil {
		next = *prev
	}
	next.slots[index] = secVisSlot{
		valid:  true,
		lat:    lat,
		lon:    lon,
		minLat: minLat,
		minLon: minLon,
		maxLat: maxLat,
		maxLon: maxLon,
	}
	s.secVis.Store(&next)
	return true
}

// SecondaryVisCenterCount returns how many secondary slots are currently set.
func (s *Session) SecondaryVisCenterCount() int {
	snap := s.secVis.Load()
	if snap == nil {
		return 0
	}
	n := 0
	for i := range snap.slots {
		if snap.slots[i].valid {
			n++
		}
	}
	return n
}

// VisBoxesOverlap reports whether any visibility AABB of a overlaps any of b.
// Primary boxes always participate; secondary SECPOS boxes are included when set.
// This is the postoffice Search predicate (historical FSD mutual box overlap).
func VisBoxesOverlap(a, b *Session) bool {
	if a == nil || b == nil {
		return false
	}
	aMin, aMax := a.VisBox()
	bMin, bMax := b.VisBox()
	if aabbOverlap(aMin, aMax, bMin, bMax) {
		return true
	}
	aSnap := a.secVis.Load()
	bSnap := b.secVis.Load()
	if aSnap != nil {
		for i := range aSnap.slots {
			if !aSnap.slots[i].valid {
				continue
			}
			saMin := [2]float64{aSnap.slots[i].minLat, aSnap.slots[i].minLon}
			saMax := [2]float64{aSnap.slots[i].maxLat, aSnap.slots[i].maxLon}
			if aabbOverlap(saMin, saMax, bMin, bMax) {
				return true
			}
			if bSnap != nil {
				for j := range bSnap.slots {
					if !bSnap.slots[j].valid {
						continue
					}
					sbMin := [2]float64{bSnap.slots[j].minLat, bSnap.slots[j].minLon}
					sbMax := [2]float64{bSnap.slots[j].maxLat, bSnap.slots[j].maxLon}
					if aabbOverlap(saMin, saMax, sbMin, sbMax) {
						return true
					}
				}
			}
		}
	}
	if bSnap != nil {
		for j := range bSnap.slots {
			if !bSnap.slots[j].valid {
				continue
			}
			sbMin := [2]float64{bSnap.slots[j].minLat, bSnap.slots[j].minLon}
			sbMax := [2]float64{bSnap.slots[j].maxLat, bSnap.slots[j].maxLon}
			if aabbOverlap(aMin, aMax, sbMin, sbMax) {
				return true
			}
		}
	}
	return false
}

// recomputeSecondaryBoxes rebuilds secondary AABBs after VisRange changes.
func (s *Session) recomputeSecondaryBoxes(rangeM float64) {
	prev := s.secVis.Load()
	if prev == nil {
		return
	}
	var next secVisSnapshot
	any := false
	for i := range prev.slots {
		if !prev.slots[i].valid {
			continue
		}
		any = true
		lat, lon := prev.slots[i].lat, prev.slots[i].lon
		minLat, minLon, maxLat, maxLon := aabbAround(lat, lon, rangeM)
		next.slots[i] = secVisSlot{
			valid:  true,
			lat:    lat,
			lon:    lon,
			minLat: minLat,
			minLon: minLon,
			maxLat: maxLat,
			maxLon: maxLon,
		}
	}
	if !any {
		s.secVis.Store(nil)
		return
	}
	s.secVis.Store(&next)
}

// aabbAround returns equirectangular AABB edges for center/range (degrees / meters).
func aabbAround(lat, lon, rangeM float64) (minLat, minLon, maxLat, maxLon float64) {
	const metersPerDegreeLat = (math.Pi * 6371000.0) / 180
	latRad := lat * (math.Pi / 180)
	deltaLat := rangeM / metersPerDegreeLat
	metersPerDegreeLon := metersPerDegreeLat * math.Cos(latRad)
	deltaLon := rangeM / metersPerDegreeLon
	return lat - deltaLat, lon - deltaLon, lat + deltaLat, lon + deltaLon
}

func aabbOverlap(minA, maxA, minB, maxB [2]float64) bool {
	return minA[0] <= maxB[0] && maxA[0] >= minB[0] &&
		minA[1] <= maxB[1] && maxA[1] >= minB[1]
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

// RemoteIP returns the remote host IP for this session.
// Nil-safe: returns "" when neither Conn nor a cached remoteIP is available.
// Host is taken from net.SplitHostPort when possible; otherwise the raw addr string.
func (s *Session) RemoteIP() string {
	if s == nil {
		return ""
	}
	if s.remoteIP != "" {
		return s.remoteIP
	}
	if s.Conn == nil {
		return ""
	}
	addr := s.Conn.RemoteAddr()
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
