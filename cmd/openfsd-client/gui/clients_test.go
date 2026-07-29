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
	var enabled int
	var hasVPilot bool
	for _, s := range slots {
		if s.Enabled {
			enabled++
			if s.ID == "vpilot" {
				hasVPilot = true
			}
			if s.ID == "xpilot" {
				t.Fatal("xpilot must not appear")
			}
			if strings.Contains(SlotLabel(s), "Coming soon") {
				t.Fatalf("enabled slot labeled coming soon: %+v", s)
			}
		}
	}
	if !hasVPilot {
		t.Fatal("expected vpilot enabled")
	}
	if enabled != 1 {
		t.Fatalf("enabled=%d want 1 (vPilot only)", enabled)
	}
}

func TestFutureClientCatalog_Empty(t *testing.T) {
	if len(FutureClientCatalog) != 0 {
		t.Fatalf("catalog should be empty (vPilot only), got %v", FutureClientCatalog)
	}
}

func TestBuildClientSlots_RegisteredAdapterBecomesEnabled(t *testing.T) {
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
