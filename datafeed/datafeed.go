package datafeed

import (
	"sync"
	"time"
)

type DataFeed struct {
	lock        sync.RWMutex
	currentFeed string
	lastUpdated time.Time
}

func (d *DataFeed) Feed() (feed string, lastUpdated time.Time) {
	d.lock.RLock()
	feed = d.currentFeed
	lastUpdated = d.lastUpdated
	d.lock.RUnlock()

	return
}

func (d *DataFeed) SetFeed(feed string, lastUpdated time.Time) {
	d.lock.Lock()
	d.currentFeed = feed
	d.lastUpdated = lastUpdated
	d.lock.Unlock()
}
