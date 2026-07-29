package gui

import (
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject/adapters"
)

func TestBuildClientSlots_WithDefaultAdapters(t *testing.T) {
	eng, err := adapters.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	slots := BuildClientSlots(eng.Adapters)
	var enabled, coming int
	var hasVPilot bool
	for _, s := range slots {
		if s.Enabled {
			enabled++
			if s.ID == "vpilot" {
				hasVPilot = true
			}
			if strings.Contains(SlotLabel(s), "Coming soon") {
				t.Fatalf("enabled slot labeled coming soon: %+v", s)
			}
		} else {
			coming++
			if !strings.Contains(SlotLabel(s), "Coming soon") {
				t.Fatalf("disabled label: %q", SlotLabel(s))
			}
		}
	}
	if !hasVPilot {
		t.Fatal("expected vpilot enabled")
	}
	if enabled < 1 || coming < 1 {
		t.Fatalf("enabled=%d coming=%d slots=%d", enabled, coming, len(slots))
	}
}
