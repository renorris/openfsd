package cluster

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// claimEntry is the owner-local state for one callsign.
type claimEntry struct {
	nodeID   string
	fence    string
	expiry   time.Time // pending only
	routable bool
	cid      int
	isATC    bool
}

// ClaimTable is the owner-side callsign claim arbiter (per-node).
// Only the owner node for a callsign mutates claims for that key.
type ClaimTable struct {
	mu      sync.Mutex
	pending map[string]*claimEntry // callsign upper → entry
	active  map[string]*claimEntry
	ttl     time.Duration
	now     func() time.Time
}

// NewClaimTable creates a claim table. defaultTTL for pending reserves (2s design).
func NewClaimTable(defaultTTL time.Duration) *ClaimTable {
	if defaultTTL <= 0 {
		defaultTTL = 2 * time.Second
	}
	return &ClaimTable{
		pending: make(map[string]*claimEntry),
		active:  make(map[string]*claimEntry),
		ttl:     defaultTTL,
		now:     time.Now,
	}
}

func (t *ClaimTable) key(cs string) string { return stringsToUpper(cs) }

// Reserve tries to place a pending reserve. Returns fence on success.
func (t *ClaimTable) Reserve(callsign string, meta ClaimMeta) (fence string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(callsign)
	now := t.now()
	t.expireLocked(now)

	if _, ok := t.active[k]; ok {
		return "", ErrClaimInUse
	}
	if e, ok := t.pending[k]; ok {
		if now.Before(e.expiry) {
			return "", ErrClaimInUse
		}
		delete(t.pending, k)
	}

	fence = meta.Fence
	if fence == "" {
		fence = uuid.NewString()
	}
	ttl := meta.TTL
	if ttl <= 0 {
		ttl = t.ttl
	}
	t.pending[k] = &claimEntry{
		nodeID:   meta.NodeID,
		fence:    fence,
		expiry:   now.Add(ttl),
		routable: false,
		cid:      meta.CID,
		isATC:    meta.IsATC,
	}
	return fence, nil
}

// Commit flips pending → active routable if fence matches.
func (t *ClaimTable) Commit(callsign, fence string) (DirMeta, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(callsign)
	now := t.now()
	t.expireLocked(now)

	e, ok := t.pending[k]
	if !ok {
		return DirMeta{}, ErrClaimNotFound
	}
	if e.fence != fence {
		return DirMeta{}, ErrClaimFence
	}
	if !now.Before(e.expiry) {
		delete(t.pending, k)
		return DirMeta{}, ErrClaimFence
	}
	delete(t.pending, k)
	e.routable = true
	e.expiry = time.Time{}
	t.active[k] = e
	return DirMeta{
		Callsign: callsign,
		NodeID:   e.nodeID,
		CID:      e.cid,
		IsATC:    e.isATC,
		Fence:    e.fence,
		Routable: true,
	}, nil
}

// Abort cancels a pending reserve if fence matches.
// Prefer Release(fence) for teardown after Reserve (clears pending OR active).
func (t *ClaimTable) Abort(callsign, fence string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(callsign)
	if e, ok := t.pending[k]; ok && fence != "" && e.fence == fence {
		delete(t.pending, k)
	}
}

// Release removes pending or active claim only when fence is non-empty and matches.
// Empty fence never force-frees (wire path safety — R2-1). Peer death uses ReleaseNode.
func (t *ClaimTable) Release(callsign, fence string) (released bool) {
	if fence == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.key(callsign)
	if e, ok := t.pending[k]; ok && e.fence == fence {
		delete(t.pending, k)
		released = true
	}
	if e, ok := t.active[k]; ok && e.fence == fence {
		delete(t.active, k)
		released = true
	}
	return released
}

// ReleaseNode removes all claims held by nodeID (peer death cleanup).
func (t *ClaimTable) ReleaseNode(nodeID string) (callsigns []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, e := range t.pending {
		if e.nodeID == nodeID {
			callsigns = append(callsigns, k)
			delete(t.pending, k)
		}
	}
	for k, e := range t.active {
		if e.nodeID == nodeID {
			callsigns = append(callsigns, k)
			delete(t.active, k)
		}
	}
	return callsigns
}

// LookupActive returns active claim info.
func (t *ClaimTable) LookupActive(callsign string) (nodeID, fence string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.active[t.key(callsign)]
	if !ok {
		return "", "", false
	}
	return e.nodeID, e.fence, true
}

func (t *ClaimTable) expireLocked(now time.Time) {
	for k, e := range t.pending {
		if !now.Before(e.expiry) {
			delete(t.pending, k)
		}
	}
}

func stringsToUpper(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
