package fsd

import (
	"errors"
	"math"
	"sync"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/tidwall/rtree"
)

type postOffice struct {
	clientMap     map[string]*Client // Callsign -> *Client
	clientMapLock *sync.RWMutex

	tree     *rtree.RTreeG[*Client] // Geospatial rtree
	treeLock *sync.RWMutex
}

func newPostOffice() *postOffice {
	return &postOffice{
		clientMap:     make(map[string]*Client, 128),
		clientMapLock: &sync.RWMutex{},
		tree:          &rtree.RTreeG[*Client]{},
		treeLock:      &sync.RWMutex{},
	}
}

var ErrCallsignInUse = errors.New("callsign in use")
var ErrCallsignDoesNotExist = errors.New("callsign does not exist")

// register adds a new Client to the post office. Returns ErrCallsignInUse when the callsign is taken.
func (p *postOffice) register(client *Client) (err error) {
	p.clientMapLock.Lock()
	if _, exists := p.clientMap[client.callsign]; exists {
		p.clientMapLock.Unlock()
		err = ErrCallsignInUse
		return
	}
	p.clientMap[client.callsign] = client
	p.clientMapLock.Unlock()

	// Insert into R-tree
	clientMin, clientMax := geo.BoundingBox(client.latLon(), client.visRange.Load())
	p.treeLock.Lock()
	p.tree.Insert(clientMin, clientMax, client)
	p.treeLock.Unlock()

	return
}

// release removes a Client from the post office.
func (p *postOffice) release(client *Client) {
	clientMin, clientMax := geo.BoundingBox(client.latLon(), client.visRange.Load())

	p.treeLock.Lock()
	p.tree.Delete(clientMin, clientMax, client)
	p.treeLock.Unlock()

	p.clientMapLock.Lock()
	delete(p.clientMap, client.callsign)
	p.clientMapLock.Unlock()

	return
}

// updatePosition updates the geospatial position of a Client.
// The referenced client's latLon and visRange are rewritten.
func (p *postOffice) updatePosition(client *Client, newCenter [2]float64, newVisRange float64) {
	oldMin, oldMax := geo.BoundingBox(client.latLon(), client.visRange.Load())
	newMin, newMax := geo.BoundingBox(newCenter, newVisRange)

	client.setLatLon(newCenter[0], newCenter[1])
	client.visRange.Store(newVisRange)

	// Avoid redundant updates
	if oldMin == newMin && oldMax == newMax {
		return
	}

	p.treeLock.Lock()
	p.tree.Delete(oldMin, oldMax, client)
	p.tree.Insert(newMin, newMax, client)
	p.treeLock.Unlock()

	return
}

// search calls `callback` for every other Client within geographical range of the provided Client.
//
// It resets Client.closestVelocityClientDistance to +Inf, then updates it to the
// minimum geo.Distance among non-self proto-101 pilot pairs discovered during the search.
func (p *postOffice) search(client *Client, callback func(recipient *Client) bool) {
	clientMin, clientMax := geo.BoundingBox(client.latLon(), client.visRange.Load())

	client.closestVelocityClientDistance = math.MaxFloat64

	p.treeLock.RLock()
	p.tree.Search(clientMin, clientMax, func(foundMin [2]float64, foundMax [2]float64, foundClient *Client) bool {
		if foundClient == client {
			return true // Ignore self
		}

		if !client.isAtc && client.protoRevision == 101 && foundClient.protoRevision == 101 {
			clientLatLon := client.latLon()
			foundClientLatLon := foundClient.latLon()
			dist := geo.Distance(clientLatLon[0], clientLatLon[1], foundClientLatLon[0], foundClientLatLon[1])
			if dist < client.closestVelocityClientDistance {
				client.closestVelocityClientDistance = dist
			}
		}

		return callback(foundClient)
	})
	p.treeLock.RUnlock()
}

// send sends a packet to a client with a given callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign does not exist.
func (p *postOffice) send(callsign string, packet string) (err error) {
	p.clientMapLock.RLock()
	client, exists := p.clientMap[callsign]
	p.clientMapLock.RUnlock()

	if !exists {
		err = ErrCallsignDoesNotExist
		return
	}

	return client.send(packet)
}

// find finds a Client with a given callsign.
//
// Returns ErrCallsignDoesNotExist if the callsign does not exist.
func (p *postOffice) find(callsign string) (client *Client, err error) {
	p.clientMapLock.RLock()
	client, exists := p.clientMap[callsign]
	p.clientMapLock.RUnlock()

	if !exists {
		err = ErrCallsignDoesNotExist
	}

	return
}

// all calls `callback` for every single client registered to the post office.
func (p *postOffice) all(client *Client, callback func(recipient *Client) bool) {
	p.clientMapLock.RLock()
	for _, recipient := range p.clientMap {
		if recipient == client {
			continue
		}
		if !callback(recipient) {
			break
		}
	}
	p.clientMapLock.RUnlock()
}
