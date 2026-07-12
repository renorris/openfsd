package fsd

import (
	"math"
	"testing"
)

// TestCountFields verifies the countFields function.
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
	}
	for _, tt := range tests {
		got := countFields(tt.packet)
		if got != tt.want {
			t.Errorf("countFields(%q) = %d, want %d", tt.packet, got, tt.want)
		}
	}
}

// TestGetField verifies the getField function.
func TestGetField(t *testing.T) {
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
	}
	for _, tt := range tests {
		got := string(getField(tt.packet, tt.index))
		if got != tt.want {
			t.Errorf("getField(%q, %d) = %q, want %q", tt.packet, tt.index, got, tt.want)
		}
	}
}

func TestPitchBankHeading(t *testing.T) {
	const packed uint32 = 4261294148 // 0xfdfe3044
	const conversionRatio float64 = 359.0 / 1023.0
	const mask uint32 = 1023
	const eps = 1e-9

	wantPitch := float64(packed>>22&mask) * conversionRatio
	wantBank := float64(packed>>12&mask) * conversionRatio
	wantHeading := float64(packed>>2&mask) * conversionRatio

	pitch, bank, heading := pitchBankHeading(packed)
	if math.Abs(pitch-wantPitch) > eps {
		t.Errorf("pitch = %v, want %v", pitch, wantPitch)
	}
	if math.Abs(bank-wantBank) > eps {
		t.Errorf("bank = %v, want %v", bank, wantBank)
	}
	if math.Abs(heading-wantHeading) > eps {
		t.Errorf("heading = %v, want %v", heading, wantHeading)
	}
}
