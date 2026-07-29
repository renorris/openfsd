package gui

import "github.com/renorris/openfsd/internal/clientinject"

// ClientSlot is one entry in the client picker (enabled or coming-soon).
// Layout must not branch on ClientID for form regions — only Enabled gates
// whether the user can select the slot.
type ClientSlot struct {
	ID          string
	DisplayName string
	Enabled     bool
	// ComingSoonLabel is non-empty when !Enabled (e.g. "Coming soon").
	ComingSoonLabel string
}

// FutureClientCatalog is empty for now — only Windows vPilot is shipped.
// Planned multi-client slots can return here when adapters are ready.
var FutureClientCatalog = []ClientSlot{}

// BuildClientSlots merges registered adapters (enabled) with the future catalog
// (disabled). Adapters take precedence by ID. Order: adapters first (registry
// order), then remaining future slots.
func BuildClientSlots(adapters map[string]clientinject.Adapter) []ClientSlot {
	seen := make(map[string]struct{})
	var out []ClientSlot

	// Stable product order for known IDs, then any extras.
	preferred := []string{"vpilot"}
	for _, id := range preferred {
		if a, ok := adapters[id]; ok && a != nil {
			out = append(out, ClientSlot{
				ID:          a.ClientID(),
				DisplayName: a.DisplayName(),
				Enabled:     true,
			})
			seen[id] = struct{}{}
		}
	}
	for id, a := range adapters {
		if a == nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, ClientSlot{
			ID:          a.ClientID(),
			DisplayName: a.DisplayName(),
			Enabled:     true,
		})
		seen[id] = struct{}{}
	}
	for _, fut := range FutureClientCatalog {
		if _, ok := seen[fut.ID]; ok {
			continue
		}
		slot := fut
		if slot.ComingSoonLabel == "" {
			slot.ComingSoonLabel = "Coming soon"
		}
		slot.Enabled = false
		out = append(out, slot)
		seen[fut.ID] = struct{}{}
	}
	return out
}

// SlotLabel returns the dropdown label for a slot.
func SlotLabel(s ClientSlot) string {
	if s.Enabled {
		return s.DisplayName
	}
	label := s.ComingSoonLabel
	if label == "" {
		label = "Coming soon"
	}
	return s.DisplayName + " (" + label + ")"
}
