// Package postoffice is the callsign registry and geospatial index for
// connected FSD sessions.
//
// # Lock order
//
// PostOffice uses clientMapLock for the callsign map and slab free-lists.
// Search / SearchATC / All scan an atomic live slab lock-free (no map lock).
//
//   - Register: map lock → unlock
//   - Release:  map lock → unlock
//   - UpdatePosition: session atomics only (no postoffice lock)
//   - Find / Send: map RLock → unlock before Session.Send
//   - Search / All: lock-free slab load; callbacks outside any postoffice lock
//
// # Session.Send rule
//
// NEVER hold clientMapLock across Session.Send if Send can block.
// Search and All invoke user callbacks only after collecting recipients so
// handlers may safely Send from callbacks.
//
// # Geospatial model
//
// Range uses axis-aligned visibility-box overlap (historical FSD semantics)
// via session VisBox / geo.AABBOverlap. At VATSIM-scale N (≤~15k) a lock-free
// O(N) slab scan with cached AABB edges is faster under concurrent position
// storms than a global R-tree or multi-cell hash (no write convoy).
//
// Live membership is an atomic pointer slab with free-list tombstones so
// Register/Release are O(1) amortized (not O(N) COW copies).
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
)

// Sentinel errors for callsign registry operations.
var (
	ErrCallsignInUse        = errors.New("callsign in use")
	ErrCallsignDoesNotExist = errors.New("callsign does not exist")
)

// foundPool recycles recipient slices used by Search.
// Cap raised for dense events (thousands of recipients).
var foundPool = sync.Pool{
	New: func() any {
		s := make([]*session.Session, 0, 64)
		return &s
	},
}

const foundPoolMaxCap = 32768

func acquireFound() *[]*session.Session {
	p := foundPool.Get().(*[]*session.Session)
	*p = (*p)[:0]
	return p
}

func releaseFound(p *[]*session.Session) {
	if cap(*p) > foundPoolMaxCap {
		return
	}
	foundPool.Put(p)
}

// liveSlab is an immutable-header slab of atomic session pointers.
// After a slab is published via atomic.Pointer, only slot values mutate
// (Register Store(s), Release Store(nil)). Grow publishes a new slab.
type liveSlab struct {
	slots []atomic.Pointer[session.Session]
}

// regMeta is the callsign map value: session + O(1) slab indices for remove.
type regMeta struct {
	s       *session.Session
	liveIdx int32
	atcIdx  int32 // -1 if not ATC
}

// PostOffice is the callsign map + lock-free geospatial scan registry.
// Safe for concurrent use.
type PostOffice struct {
	clientMap     map[string]*regMeta
	clientMapLock sync.RWMutex

	// Lock-free scan targets for Search / SearchATC / All.
	live    atomic.Pointer[liveSlab]
	atcLive atomic.Pointer[liveSlab]

	// Free-list and high-water; only under clientMapLock.
	freeLive []int32
	freeAtc  []int32
	liveUsed int
	atcUsed  int

	liveCount atomic.Int32
	atcCount  atomic.Int32
}

// New returns an empty PostOffice.
func New() *PostOffice {
	p := &PostOffice{
		clientMap: make(map[string]*regMeta, 128),
	}
	p.live.Store(newLiveSlab(128))
	p.atcLive.Store(newLiveSlab(32))
	return p
}

func newLiveSlab(capHint int) *liveSlab {
	if capHint < 1 {
		capHint = 1
	}
	return &liveSlab{slots: make([]atomic.Pointer[session.Session], capHint)}
}

func (p *PostOffice) claimLiveSlotLocked() int32 {
	if n := len(p.freeLive); n > 0 {
		idx := p.freeLive[n-1]
		p.freeLive = p.freeLive[:n-1]
		return idx
	}
	slab := p.live.Load()
	if p.liveUsed < len(slab.slots) {
		idx := int32(p.liveUsed)
		p.liveUsed++
		return idx
	}
	newCap := len(slab.slots) * 2
	if newCap < 128 {
		newCap = 128
	}
	fresh := newLiveSlab(newCap)
	for i := 0; i < len(slab.slots); i++ {
		fresh.slots[i].Store(slab.slots[i].Load())
	}
	p.live.Store(fresh)
	idx := int32(p.liveUsed)
	p.liveUsed++
	return idx
}

func (p *PostOffice) claimAtcSlotLocked() int32 {
	if n := len(p.freeAtc); n > 0 {
		idx := p.freeAtc[n-1]
		p.freeAtc = p.freeAtc[:n-1]
		return idx
	}
	slab := p.atcLive.Load()
	if p.atcUsed < len(slab.slots) {
		idx := int32(p.atcUsed)
		p.atcUsed++
		return idx
	}
	newCap := len(slab.slots) * 2
	if newCap < 32 {
		newCap = 32
	}
	fresh := newLiveSlab(newCap)
	for i := 0; i < len(slab.slots); i++ {
		fresh.slots[i].Store(slab.slots[i].Load())
	}
	p.atcLive.Store(fresh)
	idx := int32(p.atcUsed)
	p.atcUsed++
	return idx
}

// snapshotAppendLocked claims live (and ATC) slab slots for s. Caller holds clientMapLock.
func (p *PostOffice) snapshotAppendLocked(s *session.Session) (liveIdx, atcIdx int32) {
	liveIdx = p.claimLiveSlotLocked()
	p.live.Load().slots[liveIdx].Store(s)
	p.liveCount.Add(1)

	atcIdx = -1
	if s.IsAtc {
		atcIdx = p.claimAtcSlotLocked()
		p.atcLive.Load().slots[atcIdx].Store(s)
		p.atcCount.Add(1)
	}
	return liveIdx, atcIdx
}

// snapshotRemoveLocked nils slab slots for meta. Caller holds clientMapLock.
func (p *PostOffice) snapshotRemoveLocked(meta *regMeta) {
	if slab := p.live.Load(); slab != nil && meta.liveIdx >= 0 && int(meta.liveIdx) < len(slab.slots) {
		slab.slots[meta.liveIdx].Store(nil)
		p.freeLive = append(p.freeLive, meta.liveIdx)
		p.liveCount.Add(-1)
	}
	if meta.atcIdx >= 0 {
		if slab := p.atcLive.Load(); slab != nil && int(meta.atcIdx) < len(slab.slots) {
			slab.slots[meta.atcIdx].Store(nil)
			p.freeAtc = append(p.freeAtc, meta.atcIdx)
			p.atcCount.Add(-1)
		}
	}
	p.maybeCompactLocked()
}

func (p *PostOffice) maybeCompactLocked() {
	slab := p.live.Load()
	if slab == nil {
		return
	}
	n := int(p.liveCount.Load())
	capN := len(slab.slots)
	freeN := len(p.freeLive)
	if capN >= 256 && freeN >= n && freeN > 0 {
		p.compactLiveLocked()
	}
	atcSlab := p.atcLive.Load()
	if atcSlab == nil {
		return
	}
	na := int(p.atcCount.Load())
	capA := len(atcSlab.slots)
	freeA := len(p.freeAtc)
	if capA >= 64 && freeA >= na && freeA > 0 {
		p.compactAtcLocked()
	}
}

func (p *PostOffice) compactLiveLocked() {
	n := int(p.liveCount.Load())
	newCap := n * 2
	if newCap < 128 {
		newCap = 128
	}
	fresh := newLiveSlab(newCap)
	i := 0
	for _, meta := range p.clientMap {
		fresh.slots[i].Store(meta.s)
		meta.liveIdx = int32(i)
		i++
	}
	p.live.Store(fresh)
	p.liveUsed = i
	p.freeLive = p.freeLive[:0]
}

func (p *PostOffice) compactAtcLocked() {
	n := int(p.atcCount.Load())
	newCap := n * 2
	if newCap < 32 {
		newCap = 32
	}
	fresh := newLiveSlab(newCap)
	i := 0
	for _, meta := range p.clientMap {
		if meta.atcIdx < 0 {
			continue
		}
		fresh.slots[i].Store(meta.s)
		meta.atcIdx = int32(i)
		i++
	}
	p.atcLive.Store(fresh)
	p.atcUsed = i
	p.freeAtc = p.freeAtc[:0]
}

func (p *PostOffice) liveLen() int {
	return int(p.liveCount.Load())
}

// Register adds a Session to the registry.
// Returns ErrCallsignInUse when the callsign is already taken.
func (p *PostOffice) Register(s *session.Session) error {
	p.clientMapLock.Lock()
	if _, exists := p.clientMap[s.Callsign]; exists {
		p.clientMapLock.Unlock()
		return ErrCallsignInUse
	}
	liveIdx, atcIdx := p.snapshotAppendLocked(s)
	p.clientMap[s.Callsign] = &regMeta{s: s, liveIdx: liveIdx, atcIdx: atcIdx}
	p.clientMapLock.Unlock()
	return nil
}

// Release removes a Session from the registry.
func (p *PostOffice) Release(s *session.Session) {
	p.clientMapLock.Lock()
	meta, ok := p.clientMap[s.Callsign]
	if !ok || meta.s != s {
		p.clientMapLock.Unlock()
		return
	}
	delete(p.clientMap, s.Callsign)
	p.snapshotRemoveLocked(meta)
	p.clientMapLock.Unlock()
}

// UpdatePosition updates the geospatial position of a Session.
// Only session atomics (lat/lon/range/VisBox) are rewritten — O(1), lock-free
// for the postoffice. Search reads live VisBox values.
func (p *PostOffice) UpdatePosition(s *session.Session, newCenter [2]float64, newVisRange float64) {
	s.SetGeo(newCenter[0], newCenter[1], newVisRange)
}

// Search calls callback for every other Session within geographical range of s.
//
// Range uses axis-aligned visibility-box overlap (historical FSD postoffice
// semantics): searcher box(es) vs each peer's box(es). SECPOS secondary ATC
// centers participate via session.VisBoxesOverlap (any-vs-any).
//
// It resets Session.ClosestVelocityClientDistance to +Inf, then updates it to the
// minimum equirectangular distance among non-self proto-101 pilot pairs discovered
// during the search (used for send-fast hysteresis).
//
// Callbacks run without holding postoffice locks so they may call Session.Send.
func (p *PostOffice) Search(s *session.Session, callback func(recipient *session.Session) bool) {
	s.ClosestVelocityClientDistance = math.MaxFloat64

	foundPtr := acquireFound()
	defer releaseFound(foundPtr)

	p.searchLinear(s, foundPtr, false)

	// Closest-velocity over found (outside any postoffice lock).
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

// SearchATC is like Search but only yields ATC recipients (ATC slab scan).
func (p *PostOffice) SearchATC(s *session.Session, callback func(recipient *session.Session) bool) {
	s.ClosestVelocityClientDistance = math.MaxFloat64

	foundPtr := acquireFound()
	defer releaseFound(foundPtr)

	p.searchLinear(s, foundPtr, true)

	for _, recipient := range *foundPtr {
		if !callback(recipient) {
			break
		}
	}
}

// searchLinear lock-free scans live or atcLive slabs using visibility AABBs.
// Overlap includes primary VisBox and any SECPOS secondary centers (union search).
func (p *PostOffice) searchLinear(s *session.Session, foundPtr *[]*session.Session, atcOnly bool) {
	var slab *liveSlab
	if atcOnly {
		slab = p.atcLive.Load()
	} else {
		slab = p.live.Load()
	}
	if slab == nil {
		return
	}

	for i := range slab.slots {
		other := slab.slots[i].Load()
		if other == nil || other == s {
			continue
		}
		// Primary + secondary multi-center mutual box overlap (SECPOS).
		if !session.VisBoxesOverlap(s, other) {
			continue
		}
		*foundPtr = append(*foundPtr, other)
	}
}

// Send delivers a packet to the session with the given callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign is not registered.
// The map lock is released before Session.Send (Send may block on a full channel).
func (p *PostOffice) Send(callsign string, packet string) error {
	p.clientMapLock.RLock()
	meta, exists := p.clientMap[callsign]
	var s *session.Session
	if exists {
		s = meta.s
	}
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
	meta, exists := p.clientMap[callsign]
	var s *session.Session
	if exists {
		s = meta.s
	}
	p.clientMapLock.RUnlock()

	if !exists {
		return nil, ErrCallsignDoesNotExist
	}
	return s, nil
}

// All calls callback for every registered Session except except (which may be nil).
//
// Callbacks run without holding the map lock so they may call Session.Send.
func (p *PostOffice) All(except *session.Session, callback func(recipient *session.Session) bool) {
	if slab := p.live.Load(); slab != nil {
		for i := range slab.slots {
			recipient := slab.slots[i].Load()
			if recipient == nil || recipient == except {
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
	for _, meta := range p.clientMap {
		if meta.s == except {
			continue
		}
		recipients = append(recipients, meta.s)
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
	p.clientMapLock.RLock()
	out := make([]*session.Session, 0, len(p.clientMap))
	for _, meta := range p.clientMap {
		out = append(out, meta.s)
	}
	p.clientMapLock.RUnlock()
	return out
}
