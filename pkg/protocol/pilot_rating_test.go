package protocol

import "testing"

func TestPilotRatingScaleMatchesVATSIM(t *testing.T) {
	// https://vatsim.dev/resources/ratings/ — Pilot table
	want := []struct {
		id    PilotRating
		short string
		long  string
	}{
		{PilotRatingNone, "P0", "No Pilot Rating"},
		{PilotRatingPPL, "PPL", "Private Pilot License"},
		{PilotRatingIR, "IR", "Instrument Rating"},
		{PilotRatingCMEL, "CMEL", "Commercial Multi-Engine License"},
		{PilotRatingATPL, "ATPL", "Air Transport Pilot License"},
		{PilotRatingFI, "FI", "Flight Instructor"},
		{PilotRatingFE, "FE", "Flight Examiner"},
	}
	if len(PilotRatingScale) != len(want) {
		t.Fatalf("PilotRatingScale len=%d want %d", len(PilotRatingScale), len(want))
	}
	for i, w := range want {
		if PilotRatingScale[i] != w.id {
			t.Errorf("scale[%d]=%d want %d", i, PilotRatingScale[i], w.id)
		}
		if int(w.id) != int(w.id) { // keep iota-free explicit values
		}
		if !IsValidPilotRating(int(w.id)) {
			t.Errorf("IsValidPilotRating(%d) = false", w.id)
		}
		if got := PilotRatingShort(int(w.id)); got != w.short {
			t.Errorf("Short(%d)=%q want %q", w.id, got, w.short)
		}
		if got := PilotRatingLong(int(w.id)); got != w.long {
			t.Errorf("Long(%d)=%q want %q", w.id, got, w.long)
		}
	}
	// Dense 0..5 and other non-scale values are invalid.
	for _, bad := range []int{2, 4, 5, 6, 8, 16, 32, 64, -1, 100} {
		if IsValidPilotRating(bad) {
			t.Errorf("IsValidPilotRating(%d) = true, want false", bad)
		}
	}
}
