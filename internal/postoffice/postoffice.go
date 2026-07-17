// Package postoffice is the callsign registry and geospatial index for
// connected FSD sessions.
//
// # Lock order
//
// PostOffice uses clientMapLock and (when N is large) treeLock. Critical
// sections are always separate — never hold both locks at once:
//
//   - Register: map lock → unlock, then optional tree lock → unlock
//   - Release:  optional tree lock → unlock, then map lock → unlock
//   - UpdatePosition: rewrites session coords outside locks; tree rewrite
//     under tree lock only when treeReady
//   - Find / Send / Snapshot / All: map RLock only (or lock-free live snapshot)
//   - Search: lock-free linear scan unless treeReady; otherwise tree RLock
//     only (callbacks run after unlock)
//
// # Session.Send rule
//
// NEVER hold clientMapLock or treeLock across Session.Send if Send can block
// (the outbound channel send blocks when full). Send looks up under RLock then
// unlocks before calling Session.Send. Search and All collect recipients under
// RLock (or lock-free) and invoke user callbacks only after releasing the lock
// so handlers may safely Send from callbacks.
//
// This package must not import internal/server, web, or other orchestration
// packages (session depends upward only).
package postoffice

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/internal/session"
	"github.com/tidwall/rtree"
)

// Sentinel errors for callsign registry operations.
var (
	ErrCallsignInUse        = errors.New("callsign in use")
	ErrCallsignDoesNotExist = errors.New("callsign does not exist")
)

// defaultLinearSearchThreshold is the production live-set size at or below
// which Search and UpdatePosition avoid the R-tree entirely. Concurrent
// position storms at dense events (hundreds–low thousands) are dominated by
// tree RWMutex convoy; an O(N) AABB scan over an atomic snapshot is lock-free
// and faster under that load. Above the threshold the R-tree path is used.
const defaultLinearSearchThreshold = 2048

// linearSearchThreshold is the live-set size at or below which Search stays on
// the lock-free linear path. Overridable in tests via setLinearSearchThreshold.
var linearSearchThreshold = defaultLinearSearchThreshold

// foundPool recycles recipient slices used by Search.
var foundPool = sync.Pool{
	New: func() any {
		s := make([]*session.Session, 0, 64)
		return &s
	},
}

func acquireFound() *[]*session.Session {
	p := foundPool.Get().(*[]*session.Session)
	*p = (*p)[:0]
	return p
}

func releaseFound(p *[]*session.Session) {
	// Drop oversized buffers so the pool does not retain huge full-mesh slices.
	if cap(*p) > 4096 {
		return
	}
	foundPool.Put(p)
}

// PostOffice is the callsign map + optional geospatial R-tree registry.
// Safe for concurrent use.
type PostOffice struct {
	clientMap     map[string]*session.Session // Callsign -> *session.Session
	clientMapLock sync.RWMutex

	// live is an immutable snapshot of all sessions, rebuilt on Register/Release.
	// Search uses it for the lock-free linear path; readers load the pointer only.
	live atomic.Pointer[[]*session.Session]

	// atcLive is an immutable snapshot of ATC sessions only (for ATC-only fan-out).
	atcLive atomic.Pointer[[]*session.Session]

	// treeReady is true only after a consistent R-tree has been built for the
	// large-N regime. Search uses the tree exclusively when this is set; while
	// false it falls back to the lock-free linear snapshot (always correct).
	// This closes the race where liveLen has crossed the threshold but the
	// tree is still empty/partial during rebuild, and prevents concurrent
	// Register from Inserting into a tree that rebuildTree would overwrite.
	treeReady atomic.Bool

	tree     rtree.RTreeG[*session.Session] // Geospatial rtree (large-N path)
	treeLock sync.RWMutex
}

// New returns an empty PostOffice.
func New() *PostOffice {
	p := &PostOffice{
		clientMap: make(map[string]*session.Session, 128),
	}
	empty := []*session.Session{}
	p.live.Store(&empty)
	p.atcLive.Store(&empty)
	return p
}

// snapshotAppendLocked COW-appends s to live (and atcLive if ATC). Caller holds map lock.
func (p *PostOffice) snapshotAppendLocked(s *session.Session) {
	prev := p.live.Load()
	n := 0
	if prev != nil {
		n = len(*prev)
	}
	all := make([]*session.Session, n+1)
	if prev != nil {
		copy(all, *prev)
	}
	all[n] = s
	p.live.Store(&all)

	if !s.IsAtc {
		return
	}
	prevA := p.atcLive.Load()
	na := 0
	if prevA != nil {
		na = len(*prevA)
	}
	atc := make([]*session.Session, na+1)
	if prevA != nil {
		copy(atc, *prevA)
	}
	atc[na] = s
	p.atcLive.Store(&atc)
}

// snapshotRemoveLocked COW-removes s from live/atcLive. Caller holds map lock.
func (p *PostOffice) snapshotRemoveLocked(s *session.Session) {
	prev := p.live.Load()
	if prev == nil {
		return
	}
	all := make([]*session.Session, 0, len(*prev))
	for _, c := range *prev {
		if c != s {
			all = append(all, c)
		}
	}
	p.live.Store(&all)

	if !s.IsAtc {
		return
	}
	prevA := p.atcLive.Load()
	if prevA == nil {
		return
	}
	atc := make([]*session.Session, 0, len(*prevA))
	for _, c := range *prevA {
		if c != s {
			atc = append(atc, c)
		}
	}
	p.atcLive.Store(&atc)
}

func (p *PostOffice) liveLen() int {
	sp := p.live.Load()
	if sp == nil {
		return 0
	}
	return len(*sp)
}

func indexBox(center [2]float64, rangeM float64) (min, max [2]float64) {
	q := geo.QuantizeCenter(center, geo.DefaultIndexQuantumDeg)
	return geo.BoundingBox(q, rangeM)
}

// Register adds a Session to the registry.
// Returns ErrCallsignInUse when the callsign is already taken.
//
// Lock order: map then tree (separate critical sections).
func (p *PostOffice) Register(s *session.Session) error {
	p.clientMapLock.Lock()
	if _, exists := p.clientMap[s.Callsign]; exists {
		p.clientMapLock.Unlock()
		return ErrCallsignInUse
	}
	p.clientMap[s.Callsign] = s
	p.snapshotAppendLocked(s)
	n := len(p.clientMap)
	p.clientMapLock.Unlock()

	if n <= linearSearchThreshold {
		return nil
	}

	// Large-N: serialize all tree mutations under treeLock so a threshold-
	// crossing rebuild cannot race with concurrent Insert/Delete.
	p.treeLock.Lock()
	defer p.treeLock.Unlock()

	if !p.treeReady.Load() {
		// First large-N registration (or recovery after dropping below the
		// threshold): rebuild from the current live snapshot so concurrent
		// Registers that finished the map phase before we took treeLock are
		// included exactly once.
		live := p.live.Load()
		var all []*session.Session
		if live != nil {
			all = append([]*session.Session(nil), (*live)...)
		}
		p.rebuildTreeLocked(all)
		p.treeReady.Store(true)
		return nil
	}

	// Tree is already authoritative. Upsert this session (Delete+Insert) so a
	// concurrent rebuild that already included us cannot leave a duplicate.
	clientMin, clientMax := indexBox(s.LatLon(), s.VisRange.Load())
	p.tree.Delete(clientMin, clientMax, s)
	p.tree.Insert(clientMin, clientMax, s)
	return nil
}

// rebuildTreeLocked replaces the R-tree with a full rebuild.
// Caller must hold treeLock.
func (p *PostOffice) rebuildTreeLocked(all []*session.Session) {
	var fresh rtree.RTreeG[*session.Session]
	for _, s := range all {
		min, max := indexBox(s.LatLon(), s.VisRange.Load())
		fresh.Insert(min, max, s)
	}
	p.tree = fresh
}

// Release removes a Session from the registry.
//
// Lock order: tree then map (separate critical sections).
func (p *PostOffice) Release(s *session.Session) {
	if p.treeReady.Load() {
		clientMin, clientMax := indexBox(s.LatLon(), s.VisRange.Load())
		p.treeLock.Lock()
		p.tree.Delete(clientMin, clientMax, s)
		p.treeLock.Unlock()
	}

	p.clientMapLock.Lock()
	delete(p.clientMap, s.Callsign)
	p.snapshotRemoveLocked(s)
	n := len(p.clientMap)
	p.clientMapLock.Unlock()

	// Drop back to lock-free linear Search once at or below the threshold.
	// The tree may retain stale nodes; they are ignored while treeReady is false.
	if n <= linearSearchThreshold {
		p.treeReady.Store(false)
	}
}

// UpdatePosition updates the geospatial position of a Session.
// The session's lat/lon and visRange are rewritten (atomics / SetLatLon).
// Tree rewrites run only while treeReady (large-N mode with a consistent tree).
func (p *PostOffice) UpdatePosition(s *session.Session, newCenter [2]float64, newVisRange float64) {
	oldCenter := s.LatLon()
	oldRange := s.VisRange.Load()
	oldMin, oldMax := indexBox(oldCenter, oldRange)
	newMin, newMax := indexBox(newCenter, newVisRange)

	s.SetLatLon(newCenter[0], newCenter[1])
	s.VisRange.Store(newVisRange)

	// Linear / transitional mode: coords are authoritative; Search does not use the tree.
	if !p.treeReady.Load() {
		return
	}
	// Avoid redundant tree rewrites when quantized box is unchanged.
	if oldMin == newMin && oldMax == newMax {
		return
	}

	p.treeLock.Lock()
	p.tree.Delete(oldMin, oldMax, s)
	p.tree.Insert(newMin, newMax, s)
	p.treeLock.Unlock()
}

// Search calls callback for every other Session within geographical range of s.
//
// Range uses axis-aligned visibility-box overlap (historical FSD postoffice
// semantics): searcher box vs each peer's box.
//
// It resets Session.ClosestVelocityClientDistance to +Inf, then updates it to the
// minimum equirectangular distance among non-self proto-101 pilot pairs discovered
// during the search (used for send-fast hysteresis; haversine is unnecessary).
//
// Callbacks run without holding postoffice locks so they may call Session.Send.
func (p *PostOffice) Search(s *session.Session, callback func(recipient *session.Session) bool) {
	s.ClosestVelocityClientDistance = math.MaxFloat64

	foundPtr := acquireFound()
	defer releaseFound(foundPtr)

	if p.treeReady.Load() {
		p.searchTree(s, foundPtr)
	} else {
		p.searchLinear(s, foundPtr)
	}

	// Closest-velocity and callbacks outside any postoffice lock.
	if !s.IsAtc && s.ProtoRevision == 101 {
		sLL := s.LatLon()
		minD := math.MaxFloat64
		for _, other := range *foundPtr {
			if other.IsAtc || other.ProtoRevision != 101 {
				continue
			}
			oLL := other.LatLon()
			d := geo.ApproxDistance(sLL[0], sLL[1], oLL[0], oLL[1])
			if d < minD {
				minD = d
			}
		}
		s.ClosestVelocityClientDistance = minD
	}

	for _, recipient := range *foundPtr {
		if !callback(recipient) {
			break
		}
	}
}

// SearchATC is like Search but only yields ATC recipients (uses the ATC snapshot
// on the linear path to avoid scanning all pilots).
func (p *PostOffice) SearchATC(s *session.Session, callback func(recipient *session.Session) bool) {
	s.ClosestVelocityClientDistance = math.MaxFloat64

	foundPtr := acquireFound()
	defer releaseFound(foundPtr)

	if p.treeReady.Load() {
		p.searchTree(s, foundPtr)
		// Filter to ATC only (tree path does not separate ATC).
		dst := (*foundPtr)[:0]
		for _, other := range *foundPtr {
			if other.IsAtc {
				dst = append(dst, other)
			}
		}
		*foundPtr = dst
	} else {
		p.searchLinearATC(s, foundPtr)
	}

	for _, recipient := range *foundPtr {
		if !callback(recipient) {
			break
		}
	}
}

func (p *PostOffice) searchLinear(s *session.Session, foundPtr *[]*session.Session) {
	live := p.live.Load()
	if live == nil {
		return
	}
	sLL := s.LatLon()
	sRange := s.VisRange.Load()
	sMin, sMax := geo.BoundingBox(sLL, sRange)

	for _, other := range *live {
		if other == s {
			continue
		}
		oLL := other.LatLon()
		oMin, oMax := geo.BoundingBox(oLL, other.VisRange.Load())
		if !geo.AABBOverlap(sMin, sMax, oMin, oMax) {
			continue
		}
		*foundPtr = append(*foundPtr, other)
	}
}

func (p *PostOffice) searchLinearATC(s *session.Session, foundPtr *[]*session.Session) {
	live := p.atcLive.Load()
	if live == nil {
		return
	}
	sLL := s.LatLon()
	sRange := s.VisRange.Load()
	sMin, sMax := geo.BoundingBox(sLL, sRange)

	for _, other := range *live {
		if other == s {
			continue
		}
		oLL := other.LatLon()
		oMin, oMax := geo.BoundingBox(oLL, other.VisRange.Load())
		if !geo.AABBOverlap(sMin, sMax, oMin, oMax) {
			continue
		}
		*foundPtr = append(*foundPtr, other)
	}
}

func (p *PostOffice) searchTree(s *session.Session, foundPtr *[]*session.Session) {
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())

	p.treeLock.RLock()
	p.tree.Search(clientMin, clientMax, func(_ [2]float64, _ [2]float64, other *session.Session) bool {
		if other == s {
			return true
		}
		// Recheck with live coords (tree boxes may be quantized / briefly stale).
		oLL := other.LatLon()
		oMin, oMax := geo.BoundingBox(oLL, other.VisRange.Load())
		if !geo.AABBOverlap(clientMin, clientMax, oMin, oMax) {
			return true
		}
		*foundPtr = append(*foundPtr, other)
		return true
	})
	p.treeLock.RUnlock()
}

// Send delivers a packet to the session with the given callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign is not registered.
// The map lock is released before Session.Send (Send may block on a full channel).
func (p *PostOffice) Send(callsign string, packet string) error {
	p.clientMapLock.RLock()
	s, exists := p.clientMap[callsign]
	p.clientMapLock.RUnlock()

	if !exists {
		return ErrCallsignDoesNotExist
	}

	return s.Send(packet)
}

// Find returns the Session registered under callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign is not registered.
func (p *PostOffice) Find(callsign string) (*session.Session, error) {
	p.clientMapLock.RLock()
	s, exists := p.clientMap[callsign]
	p.clientMapLock.RUnlock()

	if !exists {
		return nil, ErrCallsignDoesNotExist
	}
	return s, nil
}

// All calls callback for every registered Session except except (which may be nil).
//
// Callbacks run after the map RLock is released so they may call Session.Send
// without holding postoffice locks.
func (p *PostOffice) All(except *session.Session, callback func(recipient *session.Session) bool) {
	// Prefer the lock-free live snapshot when present.
	if live := p.live.Load(); live != nil {
		for _, recipient := range *live {
			if recipient == except {
				continue
			}
			if !callback(recipient) {
				break
			}
		}
		return
	}

	p.clientMapLock.RLock()
	recipients := make([]*session.Session, 0, len(p.clientMap))
	for _, recipient := range p.clientMap {
		if recipient == except {
			continue
		}
		recipients = append(recipients, recipient)
	}
	p.clientMapLock.RUnlock()

	for _, recipient := range recipients {
		if !callback(recipient) {
			break
		}
	}
}

// Snapshot returns a consistent copy of all registered sessions for HTTP/admin use.
// The slice is a point-in-time snapshot; individual Session fields remain live.
func (p *PostOffice) Snapshot() []*session.Session {
	if live := p.live.Load(); live != nil {
		out := make([]*session.Session, len(*live))
		copy(out, *live)
		return out
	}

	p.clientMapLock.RLock()
	out := make([]*session.Session, 0, len(p.clientMap))
	for _, s := range p.clientMap {
		out = append(out, s)
	}
	p.clientMapLock.RUnlock()
	return out
}
