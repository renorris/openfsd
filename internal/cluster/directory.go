package cluster

import (
	"sync"
)

// Directory is the server-side callsign routing map.
// Lookup only returns entries with Routable=true.
type Directory struct {
	mu   sync.RWMutex
	byCS map[string]DirMeta // upper callsign
}

// NewDirectory creates an empty directory.
func NewDirectory() *Directory {
	return &Directory{byCS: make(map[string]DirMeta)}
}

// ApplyJoin upserts a routable entry. Ownership: if an entry exists for a
// different NodeID, ignore the join (prevents peer spoofing directory ownership).
func (d *Directory) ApplyJoin(m DirMeta) {
	if m.Callsign == "" {
		return
	}
	m.Routable = true
	m.Callsign = SanitizeMeshString(m.Callsign, 32)
	m.FPLInfo = SanitizeMeshString(m.FPLInfo, 2048)
	m.AssignedBeacon = SanitizeMeshString(m.AssignedBeacon, 8)
	d.mu.Lock()
	k := stringsToUpper(m.Callsign)
	if cur, ok := d.byCS[k]; ok && cur.NodeID != "" && m.NodeID != "" && cur.NodeID != m.NodeID {
		// Ownership conflict: keep existing unless incoming is same node update.
		d.mu.Unlock()
		return
	}
	// Preserve local fence if any (never overwrite with empty remote fence).
	if cur, ok := d.byCS[k]; ok && m.Fence == "" && cur.Fence != "" {
		m.Fence = cur.Fence
	}
	d.byCS[k] = m
	d.mu.Unlock()
}

// ApplyLeave removes an entry.
func (d *Directory) ApplyLeave(callsign string) {
	d.mu.Lock()
	delete(d.byCS, stringsToUpper(callsign))
	d.mu.Unlock()
}

// ApplyMeta updates FPL/beacon on an existing entry.
func (d *Directory) ApplyMeta(callsign string, fpl *string, beacon *string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := stringsToUpper(callsign)
	m, ok := d.byCS[k]
	if !ok {
		return
	}
	if fpl != nil {
		m.FPLInfo = *fpl
	}
	if beacon != nil {
		m.AssignedBeacon = *beacon
	}
	d.byCS[k] = m
}

// Lookup returns nodeID+meta only if routable.
func (d *Directory) Lookup(callsign string) (nodeID string, meta DirMeta, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	m, ok := d.byCS[stringsToUpper(callsign)]
	if !ok || !m.Routable {
		return "", DirMeta{}, false
	}
	return m.NodeID, m, true
}

// Snapshot returns all routable entries.
func (d *Directory) Snapshot() []DirMeta {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DirMeta, 0, len(d.byCS))
	for _, m := range d.byCS {
		if m.Routable {
			out = append(out, m)
		}
	}
	return out
}

// ApplySnapshot merges a peer snapshot.
// fromNode, when non-empty, filters to only entries where NodeID == fromNode
// (R2-2: peer cannot poison routing for other nodes via snapshot).
func (d *Directory) ApplySnapshot(entries []DirMeta) {
	d.ApplySnapshotFrom("", entries)
}

// ApplySnapshotFrom is like ApplySnapshot but only accepts rows owned by fromNode
// when fromNode is non-empty.
// R3-1: never steals an existing entry owned by a different node (match ApplyJoin).
func (d *Directory) ApplySnapshotFrom(fromNode string, entries []DirMeta) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range entries {
		if m.Callsign == "" || !m.Routable {
			continue
		}
		if fromNode != "" && m.NodeID != fromNode {
			continue // reject foreign ownership claims in snapshot
		}
		k := stringsToUpper(m.Callsign)
		if cur, ok := d.byCS[k]; ok {
			// Existing owner differs → keep existing (no re-home via snapshot).
			if cur.NodeID != "" && m.NodeID != "" && cur.NodeID != m.NodeID {
				continue
			}
			// Preserve local fence secret.
			if m.Fence == "" && cur.Fence != "" {
				m.Fence = cur.Fence
			}
		}
		m.Routable = true
		d.byCS[k] = m
	}
}

// RemoveNode drops all entries for nodeID; returns removed callsigns.
func (d *Directory) RemoveNode(nodeID string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for k, m := range d.byCS {
		if m.NodeID == nodeID {
			out = append(out, m.Callsign)
			if m.Callsign == "" {
				out[len(out)-1] = k
			}
			delete(d.byCS, k)
		}
	}
	return out
}

// EncodeSnapshot serializes directory for mesh (fence cleared — never broadcast secrets).
func EncodeSnapshot(entries []DirMeta) []byte {
	var b []byte
	b = encodeU32(b, uint32(len(entries)))
	for _, m := range entries {
		m.Fence = "" // never broadcast claim fences
		b = encodeDirMeta(b, m)
	}
	return b
}

// DecodeSnapshot deserializes directory snapshot.
func DecodeSnapshot(payload []byte) ([]DirMeta, error) {
	n, rest, err := decodeU32(payload)
	if err != nil {
		return nil, err
	}
	out := make([]DirMeta, 0, n)
	for i := uint32(0); i < n; i++ {
		var m DirMeta
		m, rest, err = decodeDirMeta(rest)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Delta kinds.
const (
	DeltaJoin  byte = 1
	DeltaLeave byte = 2
	DeltaMeta  byte = 3
)

// EncodeDelta encodes a single directory delta (fence cleared).
func EncodeDelta(kind byte, m DirMeta) []byte {
	m.Fence = ""
	var b []byte
	b = append(b, kind)
	b = encodeDirMeta(b, m)
	return b
}

// DecodeDelta decodes a directory delta.
func DecodeDelta(payload []byte) (kind byte, m DirMeta, err error) {
	if len(payload) < 1 {
		return 0, DirMeta{}, ErrShortFrame
	}
	kind = payload[0]
	m, _, err = decodeDirMeta(payload[1:])
	return kind, m, err
}

func encodeDirMeta(b []byte, m DirMeta) []byte {
	b = encodeString(b, m.Callsign)
	b = encodeString(b, m.NodeID)
	b = encodeU32(b, uint32(m.CID))
	if m.IsATC {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	b = encodeString(b, m.FPLInfo)
	b = encodeString(b, m.AssignedBeacon)
	b = encodeString(b, m.Fence)
	if m.Routable {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	b = encodeF64(b, m.Lat)
	b = encodeF64(b, m.Lon)
	return b
}

func decodeDirMeta(b []byte) (DirMeta, []byte, error) {
	var m DirMeta
	var err error
	m.Callsign, b, err = decodeString(b)
	if err != nil {
		return m, nil, err
	}
	m.NodeID, b, err = decodeString(b)
	if err != nil {
		return m, nil, err
	}
	var cid uint32
	cid, b, err = decodeU32(b)
	if err != nil {
		return m, nil, err
	}
	m.CID = int(cid)
	if len(b) < 1 {
		return m, nil, ErrShortFrame
	}
	m.IsATC = b[0] != 0
	b = b[1:]
	m.FPLInfo, b, err = decodeString(b)
	if err != nil {
		return m, nil, err
	}
	m.AssignedBeacon, b, err = decodeString(b)
	if err != nil {
		return m, nil, err
	}
	m.Fence, b, err = decodeString(b)
	if err != nil {
		return m, nil, err
	}
	if len(b) < 1 {
		return m, nil, ErrShortFrame
	}
	m.Routable = b[0] != 0
	b = b[1:]
	m.Lat, b, err = decodeF64(b)
	if err != nil {
		return m, nil, err
	}
	m.Lon, b, err = decodeF64(b)
	if err != nil {
		return m, nil, err
	}
	return m, b, nil
}
