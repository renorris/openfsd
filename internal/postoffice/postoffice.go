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
// via a lock-free float32 union-AABB sidecar (FilterOverlap) plus exact
// session.VisBoxesOverlap on coarse hits. At VATSIM-scale N (≤~15k) this
// beats a global R-tree or multi-cell hash under concurrent position storms
// (no write convoy).
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
	"github.com/renorris/openfsd/internal/postoffice/aabbfilter"
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

// idxPool recycles FilterOverlap destination slices.
var idxPool = sync.Pool{
	New: func() any {
		s := make([]int32, 0, 64)
		return &s
	},
}

const idxPoolMaxCap = 32768 // same spirit as foundPoolMaxCap

func acquireIdx() *[]int32 {
	p := idxPool.Get().(*[]int32)
	*p = (*p)[:0]
	return p
}

func releaseIdx(p *[]int32) {
	if p == nil || cap(*p) > idxPoolMaxCap {
		return
	}
	idxPool.Put(p)
}

// liveSlab is an immutable-header slab of atomic session pointers.
// After a slab is published via atomic.Pointer, only slot values mutate
// (Register Store(s), Release Store(nil)). Grow publishes a new slab.
type liveSlab struct {
	slots                          []atomic.Pointer[session.Session]
	minLat, minLon, maxLat, maxLon []float32 // expanded degrees; ordinary stores
	live                           []byte    // 0 = hole, ≠0 = occupied
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
	s := &liveSlab{
		slots:  make([]atomic.Pointer[session.Session], capHint),
		minLat: make([]float32, capHint),
		minLon: make([]float32, capHint),
		maxLat: make([]float32, capHint),
		maxLon: make([]float32, capHint),
		live:   make([]byte, capHint),
	}
	nan := float32(math.NaN())
	for i := 0; i < capHint; i++ {
		s.minLat[i] = nan
		s.minLon[i] = nan
		s.maxLat[i] = nan
		s.maxLon[i] = nan
	}
	return s
}

func copySlabOccupants(dst, src *liveSlab) {
	n := len(src.slots)
	for i := 0; i < n; i++ {
		dst.slots[i].Store(src.slots[i].Load())
	}
	copySidecarSoA(dst, src)
}

// copySidecarSoA copies union-AABB words.
//
// Design called for //go:norace only on sidecar loads. Production also marks
// these store helpers: concurrent compact re-fill / grow copy / hook writes
// are the same accepted torn-float32 contract as VisBox. This is not a
// license to share dst unsafely, and Search must not RaceDisable.

//go:norace
func copySidecarSoA(dst, src *liveSlab) {
	copy(dst.minLat, src.minLat)
	copy(dst.minLon, src.minLon)
	copy(dst.maxLat, src.maxLat)
	copy(dst.maxLon, src.maxLon)
	copy(dst.live, src.live)
}

// storeSidecarRow writes one expanded AABB + live flag (torn stores accepted).
// norace: see copySidecarSoA (extends load-only norace to compact/hook stores).

//go:norace
func storeSidecarRow(slab *liveSlab, i int, qMinLat, qMinLon, qMaxLat, qMaxLon float32, live byte) {
	slab.minLat[i] = qMinLat
	slab.minLon[i] = qMinLon
	slab.maxLat[i] = qMaxLat
	slab.maxLon[i] = qMaxLon
	slab.live[i] = live
}

//go:norace
func clearSidecarLive(slab *liveSlab, i int) {
	slab.live[i] = 0
}

//go:norace
func nanSidecarEdges(slab *liveSlab, i int) {
	nan := float32(math.NaN())
	slab.minLat[i] = nan
	slab.minLon[i] = nan
	slab.maxLat[i] = nan
	slab.maxLon[i] = nan
}

const maxSidecarRetries = 8

func (p *PostOffice) onVisChanged(s *session.Session) {
	p.storeUnionSidecar(s)
}

func (p *PostOffice) storeUnionSidecar(s *session.Session) {
	if s == nil {
		return
	}
	min, max := s.UnionVisBox()
	qMinLat, qMinLon, qMaxLat, qMaxLon := aabbfilter.ExpandF32(min, max)
	p.writeOne(&p.live, s.SlabLive(), s, qMinLat, qMinLon, qMaxLat, qMaxLon)
	if atc := s.SlabATC(); atc >= 0 {
		p.writeOne(&p.atcLive, atc, s, qMinLat, qMinLon, qMaxLat, qMaxLon)
	}
}

func (p *PostOffice) writeOne(slabPtr *atomic.Pointer[liveSlab], idx int32, s *session.Session, qMinLat, qMinLon, qMaxLat, qMaxLon float32) {
	for attempt := 0; attempt < maxSidecarRetries; attempt++ {
		slab := slabPtr.Load()
		if slab == nil || idx < 0 || int(idx) >= len(slab.slots) {
			return
		}
		if slab.slots[idx].Load() != s {
			return // released, compacted away, or not yet published — NEVER write
		}
		i := int(idx)
		storeSidecarRow(slab, i, qMinLat, qMinLon, qMaxLat, qMaxLon, 1)
		if slabPtr.Load() != slab {
			continue // grow/compact published a new header; retry
		}
		return
	}
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
	copySlabOccupants(fresh, slab)
	if growCopyHook != nil {
		growCopyHook()
	}
	p.live.Store(fresh)
	p.refillSidecarLocked()
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
	copySlabOccupants(fresh, slab)
	if growCopyHook != nil {
		growCopyHook()
	}
	p.atcLive.Store(fresh)
	p.refillSidecarLocked()
	idx := int32(p.atcUsed)
	p.atcUsed++
	return idx
}

// growCopyHook, if set, runs after a grow SoA copy and before Store.
// Tests use it to inject SetSecondary in the copy/Store window. Nil in production.
var growCopyHook func()

// refillSidecarLocked writes current UnionVisBox into published sidecar rows.
// Caller holds clientMapLock. Same post-publish refresh as compact.
func (p *PostOffice) refillSidecarLocked() {
	for _, meta := range p.clientMap {
		p.storeUnionSidecar(meta.s)
	}
}

// snapshotAppendLocked claims live (and ATC) slab slots for s. Caller holds clientMapLock.
func (p *PostOffice) snapshotAppendLocked(s *session.Session) (liveIdx, atcIdx int32) {
	liveIdx = p.claimLiveSlotLocked()
	atcIdx = -1
	if s.IsAtc {
		atcIdx = p.claimAtcSlotLocked()
	}
	s.SetSlabLive(liveIdx)
	s.SetSlabATC(atcIdx)
	p.live.Load().slots[liveIdx].Store(s)
	p.liveCount.Add(1)
	if atcIdx >= 0 {
		p.atcLive.Load().slots[atcIdx].Store(s)
		p.atcCount.Add(1)
	}
	return liveIdx, atcIdx
}

// snapshotRemoveLocked nils slab slots for meta. Caller holds clientMapLock.
func (p *PostOffice) snapshotRemoveLocked(meta *regMeta) {
	s := meta.s
	s.SetVisChangedHook(nil)
	s.SetSlabLive(-1)
	s.SetSlabATC(-1)
	if slab := p.live.Load(); slab != nil && meta.liveIdx >= 0 && int(meta.liveIdx) < len(slab.slots) {
		i := int(meta.liveIdx)
		clearSidecarLive(slab, i)
		slab.slots[i].Store(nil)
		nanSidecarEdges(slab, i)
		p.freeLive = append(p.freeLive, meta.liveIdx)
		p.liveCount.Add(-1)
	}
	if meta.atcIdx >= 0 {
		if slab := p.atcLive.Load(); slab != nil && int(meta.atcIdx) < len(slab.slots) {
			i := int(meta.atcIdx)
			clearSidecarLive(slab, i)
			slab.slots[i].Store(nil)
			nanSidecarEdges(slab, i)
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
		meta.s.SetSlabLive(int32(i))
		min, max := meta.s.UnionVisBox()
		q0, q1, q2, q3 := aabbfilter.ExpandF32(min, max)
		storeSidecarRow(fresh, i, q0, q1, q2, q3, 1)
		i++
	}
	p.live.Store(fresh)
	p.liveUsed = i
	p.freeLive = p.freeLive[:0]
	// Re-fill AFTER publish while still holding clientMapLock.
	// Closes: compact copied UnionVisBox, concurrent SetSecondary wrote the
	// OLD slab (p.live still old ⇒ hook retry did not run), then compact
	// published a stale primary-only row — lasting SECPOS FN.
	p.refillSidecarLocked()
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
		meta.s.SetSlabATC(int32(i))
		min, max := meta.s.UnionVisBox()
		q0, q1, q2, q3 := aabbfilter.ExpandF32(min, max)
		storeSidecarRow(fresh, i, q0, q1, q2, q3, 1)
		i++
	}
	p.atcLive.Store(fresh)
	p.atcUsed = i
	p.freeAtc = p.freeAtc[:0]
	p.refillSidecarLocked()
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
	p.storeUnionSidecar(s)
	s.SetVisChangedHook(p.onVisChanged)
	p.storeUnionSidecar(s)
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

	p.scanSidecar(s, foundPtr, false)

	// Closest-velocity: proto-101 pilots only; min DistanceSq; one sqrt → meters.
	if !s.IsAtc && s.ProtoRevision == 101 {
		sLL := s.LatLon()
		minD2 := math.MaxFloat64
		for _, other := range *foundPtr {
			if other.IsAtc || other.ProtoRevision != 101 {
				continue
			}
			oLL := other.LatLon()
			d2 := geo.DistanceSq(sLL[0], sLL[1], oLL[0], oLL[1])
			if d2 < minD2 {
				minD2 = d2
			}
		}
		if minD2 < math.MaxFloat64 {
			s.ClosestVelocityClientDistance = math.Sqrt(minD2)
		}
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

	p.scanSidecar(s, foundPtr, true)

	for _, recipient := range *foundPtr {
		if !callback(recipient) {
			break
		}
	}
}

// scanSidecar lock-free scans the union-AABB sidecar then exact VisBoxesOverlap.
func (p *PostOffice) scanSidecar(s *session.Session, foundPtr *[]*session.Session, atcOnly bool) {
	var slab *liveSlab
	if atcOnly {
		slab = p.atcLive.Load()
	} else {
		slab = p.live.Load()
	}
	if slab == nil {
		return
	}

	min, max := s.UnionVisBox()
	q0, q1, q2, q3 := aabbfilter.ExpandF32(min, max)
	query := [4]float32{q0, q1, q2, q3}

	idxBuf := acquireIdx()
	defer releaseIdx(idxBuf)
	if n := len(slab.slots); cap(*idxBuf) < n {
		grown := make([]int32, 0, n)
		*idxBuf = grown
	}
	*idxBuf = aabbfilter.FilterOverlap(query, slab.minLat, slab.minLon, slab.maxLat, slab.maxLon, slab.live, (*idxBuf)[:0])

	slotN := len(slab.slots)
	for _, i := range *idxBuf {
		if i < 0 || int(i) >= slotN {
			continue
		}
		other := slab.slots[i].Load()
		// Identity skip only. Do not skip i == SlabLive/SlabATC: compact
		// remaps those indices while this scan still holds an older slab,
		// and the old slot at that integer may be a different session.
		if other == nil || other == s {
			continue
		}
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
