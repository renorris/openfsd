package adapters

import (
	"testing"
)

func TestDefaultAdapters(t *testing.T) {
	as := DefaultAdapters()
	if len(as) < 2 {
		t.Fatalf("want ≥2 adapters, got %d", len(as))
	}
	if as[0].ClientID() != "vpilot" {
		t.Fatalf("got %q", as[0].ClientID())
	}
	ids := map[string]bool{}
	for _, a := range as {
		ids[a.ClientID()] = true
	}
	if !ids["vpilot"] || !ids["xpilot"] {
		t.Fatalf("ids=%v", ids)
	}
}

func TestDefaultEngine(t *testing.T) {
	eng, err := DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	if eng.Adapters["vpilot"] == nil {
		t.Fatal("missing vpilot")
	}
	if eng.Adapters["xpilot"] == nil {
		t.Fatal("missing xpilot")
	}
	if _, ok := eng.Profiles.Get("vpilot-3.12.1"); !ok {
		t.Fatal("missing vpilot profile")
	}
	if _, ok := eng.Profiles.Get("xpilot-3.0.1"); !ok {
		t.Fatal("missing xpilot profile")
	}
	// Multi-client: registry is not vPilot-only.
	if len(eng.Adapters) < 2 {
		t.Fatalf("adapters=%d", len(eng.Adapters))
	}
}
