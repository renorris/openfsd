package cilus

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestRoundTripASCII(t *testing.T) {
	cases := []string{
		"",
		"a",
		"hello",
		"https://auth.vatsim.net/api/fsd-jwt",
		"https://voice1.vatsim.net",
		"http://fsd.vatsim.net/",
	}
	for _, s := range cases {
		blob, err := EncodeUserString(s)
		if err != nil {
			t.Fatalf("EncodeUserString(%q): %v", s, err)
		}
		got, err := DecodeUserString(blob)
		if err != nil {
			t.Fatalf("DecodeUserString(%q): %v", s, err)
		}
		if got != s {
			t.Fatalf("round-trip: got %q want %q", got, s)
		}
		body := EncodeBody(s)
		gotBody, err := DecodeBody(body)
		if err != nil {
			t.Fatalf("DecodeBody(%q): %v", s, err)
		}
		if gotBody != s {
			t.Fatalf("body round-trip: got %q want %q", gotBody, s)
		}
	}
}

func TestRoundTripNonASCII(t *testing.T) {
	cases := []string{
		"héllo",
		"日本語",
		"café ☕",
		// Surrogate pair (emoji outside BMP).
		"pilot-\U0001F6E9",
	}
	for _, s := range cases {
		blob, err := EncodeUserString(s)
		if err != nil {
			t.Fatalf("EncodeUserString(%q): %v", s, err)
		}
		got, err := DecodeUserString(blob)
		if err != nil {
			t.Fatalf("DecodeUserString: %v", err)
		}
		if got != s {
			t.Fatalf("round-trip: got %q want %q", got, s)
		}
	}
}

func TestTerminalByte(t *testing.T) {
	// All ASCII → terminal 0x00.
	body := EncodeBody("hello")
	if body[len(body)-1] != 0x00 {
		t.Fatalf("ASCII terminal: got 0x%02x want 0x00", body[len(body)-1])
	}

	// High byte present → terminal 0x01.
	body = EncodeBody("héllo")
	if body[len(body)-1] != 0x01 {
		t.Fatalf("non-ASCII terminal: got 0x%02x want 0x01", body[len(body)-1])
	}

	// Surrogate pair → terminal 0x01.
	body = EncodeBody("\U0001F600")
	if body[len(body)-1] != 0x01 {
		t.Fatalf("surrogate terminal: got 0x%02x want 0x01", body[len(body)-1])
	}
}

func TestStockBudgets(t *testing.T) {
	// Budgets from vPilot 3.12.1 research: UTF-16 + terminal only.
	cases := []struct {
		s     string
		chars int
		body  int
	}{
		{"https://auth.vatsim.net/api/fsd-jwt", 35, 71},
		{"https://voice1.vatsim.net", 25, 51},
		{"http://fsd.vatsim.net/", 22, 45},
	}
	for _, tc := range cases {
		if len(tc.s) != tc.chars {
			t.Fatalf("%q: chars %d want %d", tc.s, len(tc.s), tc.chars)
		}
		got := BodyBudgetBytes(tc.s)
		if got != tc.body {
			t.Fatalf("%q: BodyBudgetBytes=%d want %d", tc.s, got, tc.body)
		}
		body := EncodeBody(tc.s)
		if len(body) != tc.body {
			t.Fatalf("%q: EncodeBody len=%d want %d", tc.s, len(body), tc.body)
		}
		if !FitsBudget(tc.s, tc.body) {
			t.Fatalf("%q should fit exact budget %d", tc.s, tc.body)
		}
		if FitsBudget(tc.s, tc.body-1) {
			t.Fatalf("%q should not fit budget %d", tc.s, tc.body-1)
		}
	}
}

func TestFitsBudgetEdges(t *testing.T) {
	if !FitsBudget("", 1) {
		t.Fatal("empty string body is 1 byte (terminal only)")
	}
	if FitsBudget("", 0) {
		t.Fatal("empty string needs 1 byte")
	}
	if FitsBudget("a", -1) {
		t.Fatal("negative budget never fits")
	}
	if !FitsBudget("ab", 5) {
		t.Fatal("exact: 2*2+1=5")
	}
	// Astral plane: one rune → 2 UTF-16 units → 5 body bytes.
	if BodyBudgetBytes("\U0001F600") != 5 {
		t.Fatalf("emoji budget: got %d want 5", BodyBudgetBytes("\U0001F600"))
	}
}

func TestEncodeBodyPadded(t *testing.T) {
	s := "hi"
	// Exact.
	body, err := EncodeBodyPadded(s, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 5 {
		t.Fatalf("exact len %d", len(body))
	}
	// Shorter: zero-pad after terminal so stock chars cannot leak.
	padded, err := EncodeBodyPadded(s, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(padded) != 10 {
		t.Fatalf("padded len %d", len(padded))
	}
	if !bytes.Equal(padded[:5], body) {
		t.Fatal("prefix should match unpadded body")
	}
	if !bytes.Equal(padded[5:], make([]byte, 5)) {
		t.Fatalf("tail should be zeros: %x", padded[5:])
	}
	// Decode only the real body (not padding) — padded slot is for overwrite.
	got, err := DecodeBody(padded[:5])
	if err != nil {
		t.Fatal(err)
	}
	if got != s {
		t.Fatalf("got %q", got)
	}
	// Too long.
	_, err = EncodeBodyPadded(s, 4)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("want ErrBudgetExceeded, got %v", err)
	}
	// Negative budget.
	_, err = EncodeBodyPadded(s, -1)
	if !errors.Is(err, ErrInvalidBudget) {
		t.Fatalf("want ErrInvalidBudget, got %v", err)
	}
}

func TestCompressedLengthForms(t *testing.T) {
	// 1-byte: value ≤ 0x7F. Empty string body = 1 → 0x01.
	blob, err := EncodeUserString("")
	if err != nil {
		t.Fatal(err)
	}
	if blob[0] != 0x01 {
		t.Fatalf("empty string length prefix: 0x%02x", blob[0])
	}

	// 1-byte max body length 0x7F = 127 → string of 63 ASCII chars.
	s63 := strings.Repeat("a", 63)
	blob, err = EncodeUserString(s63)
	if err != nil {
		t.Fatal(err)
	}
	if blob[0] != 0x7F {
		t.Fatalf("63-char length: 0x%02x want 0x7F", blob[0])
	}
	got, err := DecodeUserString(blob)
	if err != nil || got != s63 {
		t.Fatalf("63-char round-trip: %q %v", got, err)
	}

	// 2-byte: body length 0x81 (64 chars → body 129 = 0x81).
	s64 := strings.Repeat("b", 64)
	blob, err = EncodeUserString(s64)
	if err != nil {
		t.Fatal(err)
	}
	// body = 129 = 0x81 → 2-byte form: 0x80|0x00, 0x81 → 0x80, 0x81
	if len(blob) < 2 || blob[0] != 0x80 || blob[1] != 0x81 {
		t.Fatalf("64-char prefix: %x want 80 81", blob[:2])
	}
	got, err = DecodeUserString(blob)
	if err != nil || got != s64 {
		t.Fatalf("64-char round-trip: %q %v", got, err)
	}

	// Body length exactly 0x3FFF (max 2-byte); 0x3FFF is odd (valid body shape).
	bodyLen := uint32(0x3FFF)
	prefix, err := encodeCompressedUInt(bodyLen)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 2 || prefix[0] != 0xBF || prefix[1] != 0xFF {
		// 0x3FFF → 0x80|0x3F, 0xFF = 0xBF, 0xFF
		t.Fatalf("max 2-byte prefix: %x", prefix)
	}
	n, np, err := decodeCompressedUInt(prefix)
	if err != nil || n != bodyLen || np != 2 {
		t.Fatalf("decode max 2-byte: n=%d np=%d err=%v", n, np, err)
	}

	// 4-byte form: value 0x4000.
	prefix, err = encodeCompressedUInt(0x4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 4 || prefix[0] != 0xC0 || prefix[1] != 0x00 || prefix[2] != 0x40 || prefix[3] != 0x00 {
		t.Fatalf("4-byte 0x4000: %x", prefix)
	}
	n, np, err = decodeCompressedUInt(prefix)
	if err != nil || n != 0x4000 || np != 4 {
		t.Fatalf("decode 0x4000: n=%d np=%d err=%v", n, np, err)
	}

	// Max 4-byte value (production max).
	oldMax := maxCompressedUInt
	maxCompressedUInt = 0x1FFFFFFF
	t.Cleanup(func() { maxCompressedUInt = oldMax })

	prefix, err = encodeCompressedUInt(maxCompressedUInt)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 4 {
		t.Fatalf("max len prefix size %d", len(prefix))
	}
	n, np, err = decodeCompressedUInt(prefix)
	if err != nil || n != maxCompressedUInt || np != 4 {
		t.Fatalf("decode max: n=%d np=%d err=%v", n, np, err)
	}

	// Over max.
	_, err = encodeCompressedUInt(maxCompressedUInt + 1)
	if !errors.Is(err, ErrLengthTooLarge) {
		t.Fatalf("want ErrLengthTooLarge, got %v", err)
	}
}

func TestEncodeUserStringLengthTooLarge(t *testing.T) {
	// Lower max so a modest string exceeds the compressed-int budget.
	old := maxCompressedUInt
	maxCompressedUInt = 0x7F // 1-byte max only
	t.Cleanup(func() { maxCompressedUInt = old })

	// 64 ASCII chars → body 129 > 0x7F → 2-byte form required → ErrLengthTooLarge
	// when max is clamped to 0x7F (2-byte and 4-byte disabled by the switch).
	// With max=0x7F: n=129 > 0x7F and n > max → default branch.
	_, err := EncodeUserString(strings.Repeat("x", 64))
	if !errors.Is(err, ErrLengthTooLarge) {
		t.Fatalf("want ErrLengthTooLarge, got %v", err)
	}
}

func TestDecodeErrors(t *testing.T) {
	// Empty blob.
	if _, err := DecodeUserString(nil); !errors.Is(err, ErrEmptyBlob) {
		t.Fatalf("nil: %v", err)
	}
	if _, err := DecodeUserString([]byte{}); !errors.Is(err, ErrEmptyBlob) {
		t.Fatalf("empty: %v", err)
	}

	// decodeCompressedUInt empty (direct).
	if _, _, err := decodeCompressedUInt(nil); !errors.Is(err, ErrEmptyBlob) {
		t.Fatalf("decodeCompressedUInt nil: %v", err)
	}

	// Truncated 2-byte length.
	if _, err := DecodeUserString([]byte{0x80}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("trunc 2-byte len: %v", err)
	}
	// Truncated 4-byte length.
	if _, err := DecodeUserString([]byte{0xC0, 0x00}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("trunc 4-byte len: %v", err)
	}
	// Invalid leading byte (111xxxxx).
	if _, err := DecodeUserString([]byte{0xE0}); !errors.Is(err, ErrInvalidLength) {
		t.Fatalf("invalid lead: %v", err)
	}

	// Length claims more body than available.
	if _, err := DecodeUserString([]byte{0x05, 0x00}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("short body: %v", err)
	}

	// Zero-length body.
	if _, err := DecodeUserString([]byte{0x00}); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("zero body: %v", err)
	}

	// Empty body decode.
	if _, err := DecodeBody(nil); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("DecodeBody nil: %v", err)
	}
	// Even length.
	if _, err := DecodeBody([]byte{0x61, 0x00}); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("even body: %v", err)
	}
	// Bad terminal value.
	if _, err := DecodeBody([]byte{0x61, 0x00, 0x02}); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("bad terminal: %v", err)
	}
	// Terminal mismatch: non-ASCII char with terminal 0.
	// 'é' = U+00E9 → needs terminal 0x01.
	bad := []byte{0xE9, 0x00, 0x00}
	if _, err := DecodeBody(bad); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("terminal mismatch: %v", err)
	}
}

func TestDecodeIgnoresTrailing(t *testing.T) {
	blob, err := EncodeUserString("ab")
	if err != nil {
		t.Fatal(err)
	}
	// Append garbage after the declared entry.
	extra := append(append([]byte{}, blob...), 0xDE, 0xAD)
	got, err := DecodeUserString(extra)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ab" {
		t.Fatalf("got %q", got)
	}
}

func TestBodyBudgetMatchesEncode(t *testing.T) {
	// Including multi-byte runes and surrogates.
	cases := []string{"", "x", "héllo", "\U0001F6E9-plane", strings.Repeat("z", 100)}
	for _, s := range cases {
		body := EncodeBody(s)
		if BodyBudgetBytes(s) != len(body) {
			t.Fatalf("%q: budget %d encode %d", s, BodyBudgetBytes(s), len(body))
		}
		// Cross-check with utf16.Encode length.
		u16 := utf16.Encode([]rune(s))
		if len(body) != 2*len(u16)+1 {
			t.Fatalf("utf16 len mismatch for %q", s)
		}
	}
}

func TestEncodeBodyPaddedEmpty(t *testing.T) {
	out, err := EncodeBodyPadded("", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0] != 0x00 || out[1] != 0x00 || out[2] != 0x00 {
		// body is single 0x00 terminal; rest zero-padded.
		t.Fatalf("padded empty: %x", out)
	}
}
