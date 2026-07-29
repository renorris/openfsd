package adapters

import (
	"testing"
)

func TestDefaultAdapters(t *testing.T) {
	as := DefaultAdapters()
	if len(as) != 1 {
		t.Fatalf("want 1 adapter (vPilot only), got %d", len(as))
	}
	if as[0].ClientID() != "vpilot" {
		t.Fatalf("got %q", as[0].ClientID())
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
	if eng.Adapters["xpilot"] != nil {
		t.Fatal("xpilot must not be registered")
	}
	if _, ok := eng.Profiles.Get("vpilot-3.12.1"); !ok {
		t.Fatal("missing vpilot profile")
	}
	if _, ok := eng.Profiles.Get("xpilot-3.0.1"); ok {
		t.Fatal("xpilot profile must not be embedded")
	}
	if len(eng.Adapters) != 1 {
		t.Fatalf("adapters=%d", len(eng.Adapters))
	}
	// No embed profiles for research-only clients.
	for _, id := range []string{"euroscope-3.2.9", "vatsys-1.4.19", "xpilot-3.0.1"} {
		if _, ok := eng.Profiles.Get(id); ok {
			t.Fatalf("unexpected embed profile %q", id)
		}
	}
}
