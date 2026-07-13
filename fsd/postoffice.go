package fsd

import (
	"errors"
	"math"
	"sync"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/internal/session"
	"github.com/tidwall/rtree"
)

type postOffice struct {
	clientMap     map[string]*session.Session // Callsign -> *session.Session
	clientMapLock *sync.RWMutex

	tree     *rtree.RTreeG[*session.Session] // Geospatial rtree
	treeLock *sync.RWMutex
}

func newPostOffice() *postOffice {
	return &postOffice{
		clientMap:     make(map[string]*session.Session, 128),
		clientMapLock: &sync.RWMutex{},
		tree:          &rtree.RTreeG[*session.Session]{},
		treeLock:      &sync.RWMutex{},
	}
}

var ErrCallsignInUse = errors.New("callsign in use")
var ErrCallsignDoesNotExist = errors.New("callsign does not exist")

// register adds a new Session to the post office. Returns ErrCallsignInUse when the callsign is taken.
func (p *postOffice) register(s *session.Session) (err error) {
	p.clientMapLock.Lock()
	if _, exists := p.clientMap[s.Callsign]; exists {
		p.clientMapLock.Unlock()
		err = ErrCallsignInUse
		return
	}
	p.clientMap[s.Callsign] = s
	p.clientMapLock.Unlock()

	// Insert into R-tree
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())
	p.treeLock.Lock()
	p.tree.Insert(clientMin, clientMax, s)
	p.treeLock.Unlock()

	return
}

// release removes a Session from the post office.
func (p *postOffice) release(s *session.Session) {
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())

	p.treeLock.Lock()
	p.tree.Delete(clientMin, clientMax, s)
	p.treeLock.Unlock()

	p.clientMapLock.Lock()
	delete(p.clientMap, s.Callsign)
	p.clientMapLock.Unlock()

	return
}

// updatePosition updates the geospatial position of a Session.
// The referenced session's lat/lon and visRange are rewritten.
func (p *postOffice) updatePosition(s *session.Session, newCenter [2]float64, newVisRange float64) {
	oldMin, oldMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())
	newMin, newMax := geo.BoundingBox(newCenter, newVisRange)

	s.SetLatLon(newCenter[0], newCenter[1])
	s.VisRange.Store(newVisRange)

	// Avoid redundant updates
	if oldMin == newMin && oldMax == newMax {
		return
	}

	p.treeLock.Lock()
	p.tree.Delete(oldMin, oldMax, s)
	p.tree.Insert(newMin, newMax, s)
	p.treeLock.Unlock()

	return
}

// search calls `callback` for every other Session within geographical range of the provided Session.
//
// It resets Session.ClosestVelocityClientDistance to +Inf, then updates it to the
// minimum geo.Distance among non-self proto-101 pilot pairs discovered during the search.
func (p *postOffice) search(s *session.Session, callback func(recipient *session.Session) bool) {
	clientMin, clientMax := geo.BoundingBox(s.LatLon(), s.VisRange.Load())

	s.ClosestVelocityClientDistance = math.MaxFloat64

	p.treeLock.RLock()
	p.tree.Search(clientMin, clientMax, func(foundMin [2]float64, foundMax [2]float64, found *session.Session) bool {
		if found == s {
			return true // Ignore self
		}

		if !s.IsAtc && s.ProtoRevision == 101 && found.ProtoRevision == 101 {
			clientLatLon := s.LatLon()
			foundLatLon := found.LatLon()
			dist := geo.Distance(clientLatLon[0], clientLatLon[1], foundLatLon[0], foundLatLon[1])
			if dist < s.ClosestVelocityClientDistance {
				s.ClosestVelocityClientDistance = dist
			}
		}

		return callback(found)
	})
	p.treeLock.RUnlock()
}

// send sends a packet to a client with a given callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign does not exist.
func (p *postOffice) send(callsign string, packet string) (err error) {
	p.clientMapLock.RLock()
	s, exists := p.clientMap[callsign]
	p.clientMapLock.RUnlock()

	if !exists {
		err = ErrCallsignDoesNotExist
		return
	}

	return s.Send(packet)
}

// find finds a Session with a given callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign does not exist.
func (p *postOffice) find(callsign string) (s *session.Session, err error) {
	p.clientMapLock.RLock()
	s, exists := p.clientMap[callsign]
	p.clientMapLock.RUnlock()

	if !exists {
		err = ErrCallsignDoesNotExist
	}

	return
}

// all calls `callback` for every single client registered to the post office.
func (p *postOffice) all(s *session.Session, callback func(recipient *session.Session) bool) {
	p.clientMapLock.RLock()
	for _, recipient := range p.clientMap {
		if recipient == s {
			continue
		}
		if !callback(recipient) {
			break
		}
	}
	p.clientMapLock.RUnlock()
}
