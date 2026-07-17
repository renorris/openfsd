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
//     under tree lock only when the live set exceeds linearSearchThreshold
//   - Find / Send / Snapshot / All: map RLock only (or lock-free live snapshot)
//   - Search: lock-free linear scan for N ≤ linearSearchThreshold; otherwise
//     tree RLock only (callbacks run after unlock)
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

// linearSearchThreshold is the live-set size at or below which Search and
// UpdatePosition avoid the R-tree entirely. Concurrent position storms at
// dense events (hundreds–low thousands) are dominated by tree RWMutex
// convoy; an O(N) AABB scan over an atomic snapshot is lock-free and faster
// under that load. Above the threshold the R-tree path is used.
const linearSearchThreshold = 2048

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

// rebuildSnapshotsLocked rebuilds live/atc snapshots from the map.
// Prefer snapshotAppendLocked / snapshotRemoveLocked on the hot Register/Release paths.
func (p *PostOffice) rebuildSnapshotsLocked() {
	all := make([]*session.Session, 0, len(p.clientMap))
	atc := make([]*session.Session, 0, 8)
	for _, s := range p.clientMap {
		all = append(all, s)
		if s.IsAtc {
			atc = append(atc, s)
		}
	}
	p.live.Store(&all)
	p.atcLive.Store(&atc)
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
	// Copy live snapshot for possible full tree rebuild after unlock.
	var all []*session.Session
	if n == linearSearchThreshold+1 {
		live := p.live.Load()
		if live != nil {
			all = append([]*session.Session(nil), (*live)...)
		}
	}
	p.clientMapLock.Unlock()

	if n < linearSearchThreshold+1 {
		return nil
	}
	if n == linearSearchThreshold+1 {
		// Crossed into large-N mode: build tree from the full set.
		p.rebuildTree(all)
		return nil
	}

	// Already in large-N mode: insert this session only.
	clientMin, clientMax := indexBox(s.LatLon(), s.VisRange.Load())
	p.treeLock.Lock()
	p.tree.Insert(clientMin, clientMax, s)
	p.treeLock.Unlock()
	return nil
}

// rebuildTree replaces the R-tree with a full rebuild (caller not holding locks).
func (p *PostOffice) rebuildTree(all []*session.Session) {
	var fresh rtree.RTreeG[*session.Session]
	for _, s := range all {
		min, max := indexBox(s.LatLon(), s.VisRange.Load())
		fresh.Insert(min, max, s)
	}
	p.treeLock.Lock()
	p.tree = fresh
	p.treeLock.Unlock()
}

// Release removes a Session from the registry.
//
// Lock order: tree then map (separate critical sections).
func (p *PostOffice) Release(s *session.Session) {
	nBefore := p.liveLen()
	if nBefore > linearSearchThreshold {
		clientMin, clientMax := indexBox(s.LatLon(), s.VisRange.Load())
		p.treeLock.Lock()
		p.tree.Delete(clientMin, clientMax, s)
		p.treeLock.Unlock()
	}

	p.clientMapLock.Lock()
	delete(p.clientMap, s.Callsign)
	p.snapshotRemoveLocked(s)
	p.clientMapLock.Unlock()
}

// UpdatePosition updates the geospatial position of a Session.
// The session's lat/lon and visRange are rewritten (atomics / SetLatLon).
// Tree rewrites are skipped entirely while the live set is small enough for
// lock-free linear Search.
func (p *PostOffice) UpdatePosition(s *session.Session, newCenter [2]float64, newVisRange float64) {
	oldCenter := s.LatLon()
	oldRange := s.VisRange.Load()
	oldMin, oldMax := indexBox(oldCenter, oldRange)
	newMin, newMax := indexBox(newCenter, newVisRange)

	s.SetLatLon(newCenter[0], newCenter[1])
	s.VisRange.Store(newVisRange)

	// Large-N only: avoid redundant tree rewrites when quantized box is unchanged.
	if p.liveLen() <= linearSearchThreshold {
		return
	}
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

	if p.liveLen() <= linearSearchThreshold {
		p.searchLinear(s, foundPtr)
	} else {
		p.searchTree(s, foundPtr)
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

	if p.liveLen() <= linearSearchThreshold {
		p.searchLinearATC(s, foundPtr)
	} else {
		p.searchTree(s, foundPtr)
		// Filter to ATC only (tree path does not separate ATC).
		dst := (*foundPtr)[:0]
		for _, other := range *foundPtr {
			if other.IsAtc {
				dst = append(dst, other)
			}
		}
		*foundPtr = dst
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
