package gui

import (
	"context"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject"
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
	comingIDs := map[string]bool{}
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
			comingIDs[s.ID] = true
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
	// PR-11 scaffold: additional clients stay Coming soon (no Apply adapters).
	for _, id := range []string{"euroscope", "vatsys", "trackaudio"} {
		if !comingIDs[id] {
			t.Fatalf("expected %q in Coming soon catalog, slots=%v", id, slots)
		}
	}
}

func TestFutureClientCatalog_IDsStable(t *testing.T) {
	// Catalog lists non-default-enabled product names; xPilot stays in the
	// catalog for display order but becomes Enabled when registered.
	want := []string{"xpilot", "euroscope", "vatsys", "trackaudio"}
	if len(FutureClientCatalog) != len(want) {
		t.Fatalf("catalog len=%d want %d", len(FutureClientCatalog), len(want))
	}
	for i, id := range want {
		s := FutureClientCatalog[i]
		if s.ID != id {
			t.Fatalf("catalog[%d]=%q want %q", i, s.ID, id)
		}
		if s.Enabled {
			t.Fatalf("%q must not be Enabled in FutureClientCatalog", id)
		}
	}
}

func TestBuildClientSlots_RegisteredAdapterBecomesEnabled(t *testing.T) {
	// When an adapter is registered for a catalog ID, that slot becomes Enabled
	// and loses the Coming soon label (product rule for future enable path).
	m := map[string]clientinject.Adapter{
		"euroscope": &enableProbe{id: "euroscope", name: "Euroscope"},
	}
	slots := BuildClientSlots(m)
	var found bool
	for _, s := range slots {
		if s.ID != "euroscope" {
			continue
		}
		found = true
		if !s.Enabled {
			t.Fatal("registered euroscope adapter must be Enabled")
		}
		if strings.Contains(SlotLabel(s), "Coming soon") {
			t.Fatalf("label: %q", SlotLabel(s))
		}
	}
	if !found {
		t.Fatal("euroscope slot missing")
	}
}

// enableProbe implements clientinject.Adapter with no-op methods for catalog tests.
type enableProbe struct {
	id, name string
}

func (e *enableProbe) ClientID() string    { return e.id }
func (e *enableProbe) DisplayName() string { return e.name }
func (e *enableProbe) SupportedProfiles() []string {
	return nil
}
func (e *enableProbe) Discover(ctx context.Context) ([]clientinject.InstallCandidate, error) {
	return nil, nil
}
func (e *enableProbe) Verify(install clientinject.Install, profile *clientinject.Profile) error {
	return nil
}
func (e *enableProbe) EndpointConstraints(profile *clientinject.Profile) []clientinject.Constraint {
	return nil
}
func (e *enableProbe) Plan(install clientinject.Install, profile *clientinject.Profile, ep clientinject.Endpoints) (*clientinject.Plan, error) {
	return nil, nil
}
func (e *enableProbe) Apply(ctx context.Context, plan *clientinject.Plan, w clientinject.FileWriter) error {
	return nil
}
func (e *enableProbe) HealthCheck(install clientinject.Install, ep clientinject.Endpoints) error {
	return nil
}
func (e *enableProbe) LaunchArgs(install clientinject.Install, ep clientinject.Endpoints) []string {
	return nil
}
