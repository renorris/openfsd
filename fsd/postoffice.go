package fsd

import (
	"errors"
	"github.com/tidwall/rtree"
	"math"
	"sync"
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
	clientMin, clientMax := calculateBoundingBox(client.latLon(), client.visRange.Load())
	p.treeLock.Lock()
	p.tree.Insert(clientMin, clientMax, client)
	p.treeLock.Unlock()

	return
}

// release removes a Client from the post office.
func (p *postOffice) release(client *Client) {
	clientMin, clientMax := calculateBoundingBox(client.latLon(), client.visRange.Load())

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
	oldMin, oldMax := calculateBoundingBox(client.latLon(), client.visRange.Load())
	newMin, newMax := calculateBoundingBox(newCenter, newVisRange)

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
// It automatically resets and populates the Client.nearbyClients and Client.closestVelocityClientDistance values
func (p *postOffice) search(client *Client, callback func(recipient *Client) bool) {
	clientMin, clientMax := calculateBoundingBox(client.latLon(), client.visRange.Load())

	client.closestVelocityClientDistance = math.MaxFloat64

	p.treeLock.RLock()
	p.tree.Search(clientMin, clientMax, func(foundMin [2]float64, foundMax [2]float64, foundClient *Client) bool {
		if foundClient == client {
			return true // Ignore self
		}

		if !client.isAtc && client.protoRevision == 101 && foundClient.protoRevision == 101 {
			clientLatLon := client.latLon()
			foundClientLatLon := foundClient.latLon()
			dist := distance(clientLatLon[0], clientLatLon[1], foundClientLatLon[0], foundClientLatLon[1])
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

const (
	earthRadius = 6371000.0 // meters, approximate mean radius of Earth
	degToRad    = math.Pi / 180
)

func calculateBoundingBox(center [2]float64, radius float64) (min [2]float64, max [2]float64) {
	latRad := center[0] * degToRad
	const metersPerDegreeLat = (math.Pi * earthRadius) / 180
	deltaLat := radius / metersPerDegreeLat
	metersPerDegreeLon := metersPerDegreeLat * math.Cos(latRad)
	deltaLon := radius / metersPerDegreeLon

	minLat := center[0] - deltaLat
	maxLat := center[0] + deltaLat
	minLon := center[1] - deltaLon
	maxLon := center[1] + deltaLon

	min = [2]float64{minLat, minLon}
	max = [2]float64{maxLat, maxLon}

	return min, max
}

// distance calculates the great-circle distance between two points using the Haversine formula.
func distance(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * degToRad
	dLon := (lon2 - lon1) * degToRad

	sinDLat2 := math.Sin(dLat * 0.5)
	sinDLon2 := math.Sin(dLon * 0.5)

	cosLat1 := math.Cos(lat1 * degToRad)
	cosLat2 := math.Cos(lat2 * degToRad)

	a := sinDLat2*sinDLat2 + cosLat1*cosLat2*sinDLon2*sinDLon2

	sqrtA := math.Sqrt(a)
	sqrt1MinusA := math.Sqrt(1 - a)

	c := 2 * math.Atan2(sqrtA, sqrt1MinusA)

	return earthRadius * c
}
