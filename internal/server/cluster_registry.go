package server

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/session"
)

// HybridRegistry wraps a local postoffice Registry with optional mesh side effects.
// Find/Search/UpdatePosition remain local-only; cross-node uses mesh ports.
type HybridRegistry struct {
	local Registry
	mesh  cluster.Mesh

	// remoteHintMap peer → {distance, expiry} for multi-peer $SF.
	// Writers: mesh OnProximityHint only. Aggregate → RemoteClosestVelocityM on sessions
	// is done by server handlers when they read the atomic on the session.
	hintMu   sync.Mutex
	hintByCS map[string]map[string]remoteHint // callsign → peer → hint

	// claim fences for local sessions (callsign → fence) for Release on disconnect.
	fenceMu sync.Mutex
	fences  map[string]string

	// interest dirty + publisher
	interestMu    sync.Mutex
	interestDirty bool
	stopInterest  chan struct{}
	interestOnce  sync.Once
}

type remoteHint struct {
	distanceM float64
	expiry    time.Time
}

// NewHybridRegistry wraps local with mesh. mesh may be nil (then behaves as local).
func NewHybridRegistry(local Registry, mesh cluster.Mesh) *HybridRegistry {
	h := &HybridRegistry{
		local:        local,
		mesh:         mesh,
		hintByCS:     make(map[string]map[string]remoteHint),
		fences:       make(map[string]string),
		stopInterest: make(chan struct{}),
	}
	if mesh != nil {
		mesh.OnProximityHint(h.onProximityHint)
		// Init RemoteClosestVelocityM sentinel handled per-session at login.
		go h.hintTTLSweep()
		go h.interestPublisher()
	}
	return h
}

func (h *HybridRegistry) Local() Registry { return h.local }
func (h *HybridRegistry) Mesh() cluster.Mesh {
	return h.mesh
}

func (h *HybridRegistry) Register(s *session.Session) error {
	// Unset remote closest = +Inf
	s.RemoteClosestVelocityM.Store(math.Inf(1))
	err := h.local.Register(s)
	if err == nil {
		h.markInterestDirty()
	}
	return err
}

func (h *HybridRegistry) Release(s *session.Session) {
	h.local.Release(s)
	if h.mesh != nil && s != nil {
		cs := s.Callsign
		h.fenceMu.Lock()
		fence := h.fences[cs]
		delete(h.fences, cs)
		h.fenceMu.Unlock()
		if fence != "" {
			h.mesh.ClaimRelease(cs, fence)
		} else {
			h.mesh.NotifyLocalLeave(cs)
		}
		h.hintMu.Lock()
		delete(h.hintByCS, cs)
		h.hintMu.Unlock()
		h.markInterestDirty()
	}
}

func (h *HybridRegistry) UpdatePosition(s *session.Session, center [2]float64, visRangeM float64) {
	h.local.UpdatePosition(s, center, visRangeM)
	h.markInterestDirty()
}

func (h *HybridRegistry) Search(s *session.Session, fn func(*session.Session) bool) {
	h.local.Search(s, fn)
}

func (h *HybridRegistry) All(except *session.Session, fn func(*session.Session) bool) {
	h.local.All(except, fn)
}

func (h *HybridRegistry) Send(callsign, packet string) error {
	return h.local.Send(callsign, packet)
}

func (h *HybridRegistry) Find(callsign string) (*session.Session, error) {
	return h.local.Find(callsign)
}

func (h *HybridRegistry) Snapshot() []*session.Session {
	return h.local.Snapshot()
}

// StoreFence records the claim fence for a callsign after successful Commit.
func (h *HybridRegistry) StoreFence(callsign, fence string) {
	h.fenceMu.Lock()
	h.fences[callsign] = fence
	h.fenceMu.Unlock()
}

// TakeFence returns and clears the fence (for abort paths).
func (h *HybridRegistry) TakeFence(callsign string) string {
	h.fenceMu.Lock()
	f := h.fences[callsign]
	delete(h.fences, callsign)
	h.fenceMu.Unlock()
	return f
}

// RemoteClosestM returns the min non-expired remote proximity for callsign.
func (h *HybridRegistry) RemoteClosestM(callsign string, now time.Time) float64 {
	h.hintMu.Lock()
	defer h.hintMu.Unlock()
	m := h.hintByCS[callsign]
	if len(m) == 0 {
		return math.Inf(1)
	}
	min := math.Inf(1)
	for peer, hnt := range m {
		if now.After(hnt.expiry) {
			delete(m, peer)
			continue
		}
		if hnt.distanceM < min {
			min = hnt.distanceM
		}
	}
	if len(m) == 0 {
		delete(h.hintByCS, callsign)
	}
	return min
}

func (h *HybridRegistry) onProximityHint(fromNode, targetCallsign string, distanceM float64) {
	const ttl = 3 * time.Second
	h.hintMu.Lock()
	defer h.hintMu.Unlock()
	m := h.hintByCS[targetCallsign]
	if m == nil {
		m = make(map[string]remoteHint)
		h.hintByCS[targetCallsign] = m
	}
	m[fromNode] = remoteHint{distanceM: distanceM, expiry: time.Now().Add(ttl)}
	// Update session atomic if local
	if sess, err := h.local.Find(targetCallsign); err == nil && sess != nil {
		min := math.Inf(1)
		for _, hnt := range m {
			if hnt.distanceM < min {
				min = hnt.distanceM
			}
		}
		sess.RemoteClosestVelocityM.Store(min)
	}
}

// hintTTLSweep expires remote hints and resets RemoteClosestVelocityM to +Inf when empty.
func (h *HybridRegistry) hintTTLSweep() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-h.stopInterest:
			return
		case now := <-t.C:
			h.hintMu.Lock()
			for cs, m := range h.hintByCS {
				for peer, hnt := range m {
					if now.After(hnt.expiry) {
						delete(m, peer)
					}
				}
				min := math.Inf(1)
				for _, hnt := range m {
					if hnt.distanceM < min {
						min = hnt.distanceM
					}
				}
				if len(m) == 0 {
					delete(h.hintByCS, cs)
				}
				if sess, err := h.local.Find(cs); err == nil && sess != nil {
					sess.RemoteClosestVelocityM.Store(min)
				}
			}
			h.hintMu.Unlock()
		}
	}
}

func (h *HybridRegistry) markInterestDirty() {
	if h.mesh == nil {
		return
	}
	h.interestMu.Lock()
	h.interestDirty = true
	h.interestMu.Unlock()
}

// interestPublisher publishes InterestSummary ~1 Hz when dirty (PR-8a).
func (h *HybridRegistry) interestPublisher() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-h.stopInterest:
			return
		case <-t.C:
			h.interestMu.Lock()
			dirty := h.interestDirty
			h.interestDirty = false
			h.interestMu.Unlock()
			if !dirty || h.mesh == nil {
				continue
			}
			sum := h.buildInterestSummary()
			h.mesh.PublishInterest(sum)
		}
	}
}

func (h *HybridRegistry) buildInterestSummary() cluster.InterestSummary {
	snaps := h.local.Snapshot()
	var boxes []cluster.InterestBox
	for _, s := range snaps {
		min, max := s.VisBox()
		boxes = append(boxes, cluster.InterestBox{
			AABB: cluster.AABB{
				MinLat: min[0], MinLon: min[1],
				MaxLat: max[0], MaxLon: max[1],
			},
			IsATC: s.IsAtc,
		})
		s.EachSecondaryVisBox(func(bMin, bMax [2]float64) {
			boxes = append(boxes, cluster.InterestBox{
				AABB: cluster.AABB{
					MinLat: bMin[0], MinLon: bMin[1],
					MaxLat: bMax[0], MaxLon: bMax[1],
				},
				IsATC: true, // SECPOS always ATC
			})
		})
	}
	merged, _ := cluster.MergeInterestBoxes(boxes, cluster.MaxInterestBoxes)
	return cluster.InterestSummary{
		NodeID: h.mesh.NodeID(),
		Boxes:  merged,
	}
}

// StopInterest stops background interest/hint goroutines.
func (h *HybridRegistry) StopInterest() {
	h.interestOnce.Do(func() {
		close(h.stopInterest)
	})
}

// MeshForwardPosition enqueues ranged position to mesh (non-blocking).
func (h *HybridRegistry) MeshForwardPosition(wire []byte, senderBoxes []cluster.AABB, velocity bool, senderCallsign string) {
	if h.mesh == nil {
		return
	}
	cls := cluster.RangeClassPosition
	if velocity {
		cls = cluster.RangeClassVelocity
	}
	h.mesh.ForwardRanged(wire, senderBoxes, cls, senderCallsign)
}

// MeshSendDirect forwards a direct packet if target is remote.
func (h *HybridRegistry) MeshSendDirect(callsign string, wire []byte) error {
	if h.mesh == nil {
		return ErrCallsignDoesNotExist
	}
	return h.mesh.SendDirect(callsign, wire)
}

// MeshHomeRPC invokes home-node RPC for remote targets.
func (h *HybridRegistry) MeshHomeRPC(ctx context.Context, callsign string, op cluster.HomeOp, payload []byte) ([]byte, error) {
	if h.mesh == nil {
		return nil, cluster.ErrHomeRPCNotFound
	}
	return h.mesh.HomeRPC(ctx, callsign, op, payload)
}

// LookupRemote returns directory entry for remote callsign.
func (h *HybridRegistry) LookupRemote(callsign string) (nodeID string, meta cluster.DirMeta, ok bool) {
	if h.mesh == nil {
		return "", cluster.DirMeta{}, false
	}
	return h.mesh.Lookup(callsign)
}

// Ensure HybridRegistry implements Registry.
var _ Registry = (*HybridRegistry)(nil)
