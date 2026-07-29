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

// FutureClientCatalog lists planned multi-client product slots that are not
// yet registered as adapters. When an adapter appears in DefaultAdapters, it
// is merged as Enabled and removed from the "coming soon" presentation.
//
// This is a product catalog, not a layout special-case on a single client_id.
var FutureClientCatalog = []ClientSlot{
	{ID: "xpilot", DisplayName: "xPilot", Enabled: false, ComingSoonLabel: "Coming soon"},
	{ID: "euroscope", DisplayName: "Euroscope", Enabled: false, ComingSoonLabel: "Coming soon"},
	{ID: "vatsys", DisplayName: "vatSys", Enabled: false, ComingSoonLabel: "Coming soon"},
	{ID: "trackaudio", DisplayName: "TrackAudio", Enabled: false, ComingSoonLabel: "Coming soon"},
}

// BuildClientSlots merges registered adapters (enabled) with the future catalog
// (disabled). Adapters take precedence by ID. Order: adapters first (registry
// order), then remaining future slots.
func BuildClientSlots(adapters map[string]clientinject.Adapter) []ClientSlot {
	seen := make(map[string]struct{})
	var out []ClientSlot

	// Stable order: prefer known adapter IDs in a fixed product order, then any extras.
	preferred := []string{"vpilot", "xpilot", "euroscope", "vatsys", "trackaudio"}
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
	// Any other registered adapters not in preferred list.
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
	// Future catalog entries without a live adapter.
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
