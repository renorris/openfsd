package protocol

import (
	"bytes"
	"testing"
)

func TestCountFields(t *testing.T) {
	tests := []struct {
		packet []byte
		want   int
	}{
		{[]byte(""), 1},
		{[]byte("abc"), 1},
		{[]byte("a:b"), 2},
		{[]byte("a:b:c"), 3},
		{[]byte("a:b:"), 3},
		{[]byte(":a:b"), 3},
		{[]byte(":"), 2},
		{[]byte("a:b:c\r\n"), 3},
	}
	for _, tt := range tests {
		got := CountFields(tt.packet)
		if got != tt.want {
			t.Errorf("CountFields(%q) = %d, want %d", tt.packet, got, tt.want)
		}
	}
}

func TestField(t *testing.T) {
	tests := []struct {
		packet []byte
		index  int
		want   string
	}{
		{[]byte("a:b:c"), 0, "a"},
		{[]byte("a:b:c"), 1, "b"},
		{[]byte("a:b:c"), 2, "c"},
		{[]byte("a:"), 0, "a"},
		{[]byte("a:"), 1, ""},
		{[]byte("a:"), 2, ""},
		{[]byte(":a"), 0, ""},
		{[]byte(":a"), 1, "a"},
		{[]byte(""), 0, ""},
		{[]byte(""), 1, ""},
		{[]byte("a"), 0, "a"},
		{[]byte("a:b\r\n"), 1, "b"},
		{[]byte("a:b\r\n"), 0, "a"},
		{[]byte("x:y:z\r\n"), -1, ""}, // negative: no panic, nil/empty
	}
	for _, tt := range tests {
		got := Field(tt.packet, tt.index)
		if string(got) != tt.want {
			t.Errorf("Field(%q, %d) = %q, want %q", tt.packet, tt.index, got, tt.want)
		}
	}
	// Negative index: intentional safety delta vs historical getField (which returned field 0).
	// Documented contract: nil, no panic. fsd never passes negative indices.
	if got := Field([]byte("a:b:c"), -1); got != nil {
		t.Errorf("Field(-1) = %q, want nil", got)
	}
	if got := Field([]byte("a"), -5); got != nil {
		t.Errorf("Field(-5) = %q, want nil", got)
	}
	if got := Field(nil, -1); got != nil {
		t.Errorf("Field(nil, -1) = %q, want nil", got)
	}
}

func TestFieldMatchesHistoricalRebase(t *testing.T) {
	// Differential: Field must match historical getField for out-of-range indices
	// (last remaining slice is returned repeatedly after the final delimiter).
	packet := []byte("a:b")
	if got := string(Field(packet, 5)); got != "b" {
		t.Errorf("Field out-of-range = %q, want %q", got, "b")
	}
}

func TestRebaseToNextField(t *testing.T) {
	got := rebaseToNextField([]byte("a:b:c"))
	if !bytes.Equal(got, []byte("b:c")) {
		t.Errorf("rebase = %q, want %q", got, "b:c")
	}
	// no colon: returns packet[0:]
	got = rebaseToNextField([]byte("abc"))
	if !bytes.Equal(got, []byte("abc")) {
		t.Errorf("rebase no-colon = %q, want %q", got, "abc")
	}
}
