package cluster

import (
	"hash/fnv"
	"sort"
	"strings"
)

// StablePeerRing holds sorted peer node IDs used for consistent-hash ownership.
// Membership is the configured CLUSTER_PEERS set (not live mesh set).
type StablePeerRing struct {
	ids []string // sorted
}

// NewStablePeerRing builds a ring from node IDs (self included). Max 8 peers (PD-4).
func NewStablePeerRing(nodeIDs []string) (*StablePeerRing, error) {
	seen := make(map[string]struct{}, len(nodeIDs))
	var ids []string
	for _, id := range nodeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, ErrInvalidConfig
	}
	if len(ids) > 8 {
		return nil, ErrInvalidConfig
	}
	sort.Strings(ids)
	return &StablePeerRing{ids: ids}, nil
}

// IDs returns a copy of the sorted ring.
func (r *StablePeerRing) IDs() []string {
	out := make([]string, len(r.ids))
	copy(out, r.ids)
	return out
}

// Owner returns the owner node for callsign (hash mod len).
func (r *StablePeerRing) Owner(callsign string) string {
	if len(r.ids) == 0 {
		return ""
	}
	if len(r.ids) == 1 {
		return r.ids[0]
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToUpper(callsign)))
	return r.ids[int(h.Sum32())%len(r.ids)]
}

// Len returns ring size.
func (r *StablePeerRing) Len() int { return len(r.ids) }
