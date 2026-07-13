// Package postoffice is the callsign registry and geospatial index for
// connected FSD sessions.
//
// # Lock order
//
// PostOffice uses two independent RWMutexes (map + R-tree). Critical sections
// are always separate — never hold both locks at once:
//
//   - Register: map lock → unlock, then tree lock → unlock
//   - Release:  tree lock → unlock, then map lock → unlock
//   - UpdatePosition: rewrites session coords outside locks; tree rewrite under tree lock only
//   - Find / Send / Snapshot / All: map RLock only
//   - Search: tree RLock only
//
// # Session.Send rule
//
// NEVER hold clientMapLock or treeLock across Session.Send if Send can block
// (the outbound channel send blocks when full). Send looks up under RLock then
// unlocks before calling Session.Send. Search and All collect recipients under
// RLock and invoke user callbacks only after releasing the lock so handlers may
// safely Send from callbacks.
//
// This package must not import internal/server, web, or other orchestration
// packages (session depends upward only).
package postoffice

import (
	"errors"
	"math"
	"sync"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/internal/session"
	"github.com/tidwall/rtree"
)

// Sentinel errors for callsign registry operations.
var (
	ErrCallsignInUse        = errors.New("callsign in use")
	ErrCallsignDoesNotExist = errors.New("callsign does not exist")
)

// PostOffice is the callsign map + geospatial R-tree registry.
// Safe for concurrent use.
type PostOffice struct {
	clientMap     map[string]*session.Session // Callsign -> *session.Session
	clientMapLock sync.RWMutex

	tree     rtree.RTreeG[*session.Session] // Geospatial rtree
	treeLock sync.RWMutex
}

// New returns an empty PostOffice.
func New() *PostOffice {
	return &PostOffice{
		clientMap: make(map[string]*session.Session, 128),
	}
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
	p.clientMapLock.Unlock()

	// Insert into R-tree (separate critical section — never hold map+tree together).
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())
	p.treeLock.Lock()
	p.tree.Insert(clientMin, clientMax, s)
	p.treeLock.Unlock()

	return nil
}

// Release removes a Session from the registry.
//
// Lock order: tree then map (separate critical sections).
func (p *PostOffice) Release(s *session.Session) {
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())

	p.treeLock.Lock()
	p.tree.Delete(clientMin, clientMax, s)
	p.treeLock.Unlock()

	p.clientMapLock.Lock()
	delete(p.clientMap, s.Callsign)
	p.clientMapLock.Unlock()
}

// UpdatePosition updates the geospatial position of a Session.
// The session's lat/lon and visRange are rewritten (atomics / SetLatLon).
func (p *PostOffice) UpdatePosition(s *session.Session, newCenter [2]float64, newVisRange float64) {
	oldMin, oldMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())
	newMin, newMax := geo.BoundingBox(newCenter, newVisRange)

	s.SetLatLon(newCenter[0], newCenter[1])
	s.VisRange.Store(newVisRange)

	// Avoid redundant tree rewrites
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
// It resets Session.ClosestVelocityClientDistance to +Inf, then updates it to the
// minimum geo.Distance among non-self proto-101 pilot pairs discovered during the search.
//
// Callbacks run after the tree RLock is released so they may call Session.Send
// without holding postoffice locks.
func (p *PostOffice) Search(s *session.Session, callback func(recipient *session.Session) bool) {
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())

	s.ClosestVelocityClientDistance = math.MaxFloat64

	var found []*session.Session

	p.treeLock.RLock()
	p.tree.Search(clientMin, clientMax, func(_ [2]float64, _ [2]float64, other *session.Session) bool {
		if other == s {
			return true // Ignore self
		}

		if !s.IsAtc && s.ProtoRevision == 101 && other.ProtoRevision == 101 {
			clientLatLon := s.LatLon()
			foundLatLon := other.LatLon()
			dist := geo.Distance(clientLatLon[0], clientLatLon[1], foundLatLon[0], foundLatLon[1])
			if dist < s.ClosestVelocityClientDistance {
				s.ClosestVelocityClientDistance = dist
			}
		}

		found = append(found, other)
		return true
	})
	p.treeLock.RUnlock()

	for _, recipient := range found {
		if !callback(recipient) {
			break
		}
	}
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
	p.clientMapLock.RLock()
	out := make([]*session.Session, 0, len(p.clientMap))
	for _, s := range p.clientMap {
		out = append(out, s)
	}
	p.clientMapLock.RUnlock()
	return out
}
