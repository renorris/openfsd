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
	var hasVPilot, hasXPilot bool
	for _, s := range slots {
		if s.Enabled {
			enabled++
			if s.ID == "vpilot" {
				hasVPilot = true
			}
			if s.ID == "xpilot" {
				hasXPilot = true
			}
			if strings.Contains(SlotLabel(s), "Coming soon") {
				t.Fatalf("enabled slot labeled coming soon: %+v", s)
			}
		} else {
			coming++
			if !strings.Contains(SlotLabel(s), "Coming soon") {
				t.Fatalf("disabled label: %q", SlotLabel(s))
			}
			if s.ID == "xpilot" {
				t.Fatal("xpilot registered as adapter must not stay Coming soon")
			}
		}
	}
	if !hasVPilot {
		t.Fatal("expected vpilot enabled")
	}
	if !hasXPilot {
		t.Fatal("expected xpilot enabled (adapter registered)")
	}
	if enabled < 2 || coming < 1 {
		t.Fatalf("enabled=%d coming=%d slots=%d", enabled, coming, len(slots))
	}
}
