package client

import (
	"sync"
	"time"
)

type spatialState struct {
	lock sync.RWMutex

	latitude    float64
	longitude   float64
	altitude    int
	groundspeed int
	transponder string
	heading     int
	lastUpdated time.Time
}

func (s *spatialState) update(
	latitude, longitude float64,
	altitude, groundspeed, heading int,
	transponder string) {

	s.lock.Lock()

	s.latitude = latitude
	s.longitude = longitude
	s.altitude = altitude
	s.groundspeed = groundspeed
	s.transponder = transponder
	s.heading = heading
	s.lastUpdated = time.Now()

	s.lock.Unlock()
}
