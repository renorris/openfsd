package adapters

import (
	"testing"
)

func TestDefaultAdapters(t *testing.T) {
	as := DefaultAdapters()
	if len(as) == 0 {
		t.Fatal("empty")
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
	if _, ok := eng.Profiles.Get("vpilot-3.12.1"); !ok {
		t.Fatal("missing profile")
	}
}
