package afv

import (
	"strings"
	"sync"
)

// remoteDir is this node's view of peers' radios, tagged by origin node.
// Not merged into local freqIndex (M-7). Used for peer-death purge bookkeeping
// and debug; range class comes from AudioRelay.IsATC, not this store.
type remoteDir struct {
	mu sync.RWMutex
	// origin → callsignKey → session
	byOrigin map[string]map[string]RemoteSession
}

// RemoteSession is one remote voice session's radio state.
type RemoteSession struct {
	Callsign string
	IsATC    bool
	Trxs     []Transceiver
}

func newRemoteDir() *remoteDir {
	return &remoteDir{byOrigin: make(map[string]map[string]RemoteSession)}
}

// ApplySnapshot replaces all sessions for origin with the given list.
func (d *remoteDir) ApplySnapshot(origin string, sessions []RemoteSession) {
	if d == nil {
		return
	}
	origin = strings.TrimSpace(origin)
	d.mu.Lock()
	defer d.mu.Unlock()
	m := make(map[string]RemoteSession, len(sessions))
	for _, s := range sessions {
		key := meshCallsignKey(s.Callsign)
		if key == "" {
			continue
		}
		cp := RemoteSession{
			Callsign: key,
			IsATC:    s.IsATC,
			Trxs:     append([]Transceiver(nil), s.Trxs...),
		}
		m[key] = cp
	}
	if d.byOrigin == nil {
		d.byOrigin = make(map[string]map[string]RemoteSession)
	}
	d.byOrigin[origin] = m
}

// ApplyDelta upserts one callsign under origin (replace-all trxs).
func (d *remoteDir) ApplyDelta(origin, callsign string, isATC bool, trxs []Transceiver) {
	if d == nil {
		return
	}
	origin = strings.TrimSpace(origin)
	key := meshCallsignKey(callsign)
	if key == "" || origin == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byOrigin == nil {
		d.byOrigin = make(map[string]map[string]RemoteSession)
	}
	m := d.byOrigin[origin]
	if m == nil {
		m = make(map[string]RemoteSession)
		d.byOrigin[origin] = m
	}
	m[key] = RemoteSession{
		Callsign: key,
		IsATC:    isATC,
		Trxs:     append([]Transceiver(nil), trxs...),
	}
}

// ApplyLeave removes one callsign under origin.
func (d *remoteDir) ApplyLeave(origin, callsign string) {
	if d == nil {
		return
	}
	origin = strings.TrimSpace(origin)
	key := meshCallsignKey(callsign)
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.byOrigin[origin]
	if m == nil {
		return
	}
	delete(m, key)
	if len(m) == 0 {
		delete(d.byOrigin, origin)
	}
}

// RemoveNode drops all entries for origin (peer death).
func (d *remoteDir) RemoveNode(origin string) {
	if d == nil {
		return
	}
	origin = strings.TrimSpace(origin)
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.byOrigin, origin)
}

// Session returns a copy of one remote session if present.
func (d *remoteDir) Session(origin, callsign string) (RemoteSession, bool) {
	if d == nil {
		return RemoteSession{}, false
	}
	origin = strings.TrimSpace(origin)
	key := meshCallsignKey(callsign)
	d.mu.RLock()
	defer d.mu.RUnlock()
	m := d.byOrigin[origin]
	if m == nil {
		return RemoteSession{}, false
	}
	s, ok := m[key]
	if !ok {
		return RemoteSession{}, false
	}
	return RemoteSession{
		Callsign: s.Callsign,
		IsATC:    s.IsATC,
		Trxs:     append([]Transceiver(nil), s.Trxs...),
	}, true
}

// Count returns total remote sessions across all origins.
func (d *remoteDir) Count() int {
	if d == nil {
		return 0
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := 0
	for _, m := range d.byOrigin {
		n += len(m)
	}
	return n
}

// CountOrigin returns sessions for one origin.
func (d *remoteDir) CountOrigin(origin string) int {
	if d == nil {
		return 0
	}
	origin = strings.TrimSpace(origin)
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.byOrigin[origin])
}

// Origins lists known origin node IDs.
func (d *remoteDir) Origins() []string {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]string, 0, len(d.byOrigin))
	for o := range d.byOrigin {
		out = append(out, o)
	}
	return out
}
