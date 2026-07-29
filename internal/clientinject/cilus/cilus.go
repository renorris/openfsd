// Package cilus provides pure CLR #US (user string heap) encode/decode per
// ECMA-335 II.24.2.4. It depends only on the Go standard library.
//
// A #US heap entry is:
//
//	compressed unsigned length || UTF-16LE characters || terminal byte
//
// The compressed length encodes the size of the following blob bytes (UTF-16
// payload + terminal). Payload budgets for in-place PE overwrite count body
// bytes only (UTF-16 + terminal), not the length prefix.
//
// # In-place PE overwrite contract
//
// When replacing a stock string whose body slot is budgetBytes long:
//
//  1. newBody := EncodeBody(s)  (must fit: FitsBudget(s, budgetBytes))
//  2. Rewrite the compressed length prefix to len(newBody) (not budgetBytes).
//  3. Write EncodeBodyPadded(s, budgetBytes) at the body file offset so any
//     residual stock bytes past the real terminal are zeroed and cannot leak.
//
// EncodeBodyPadded is NOT a valid #US body of length budgetBytes when the
// string is shorter than the budget: the terminal sits at len(EncodeBody(s))-1,
// with zeros after it. Callers must always rewrite the length prefix to the
// unpadded body length. DecodeBody on the full padded buffer will fail or
// produce garbage — only DecodeBody(padded[:len(EncodeBody(s))]) is valid.
package cilus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// Sentinel errors for #US encode/decode failures.
var (
	ErrEmptyBlob      = errors.New("cilus: empty blob")
	ErrTruncated      = errors.New("cilus: truncated blob")
	ErrInvalidLength  = errors.New("cilus: invalid compressed length")
	ErrInvalidBody    = errors.New("cilus: invalid #US body")
	ErrBudgetExceeded = errors.New("cilus: body exceeds budget")
	ErrInvalidBudget  = errors.New("cilus: invalid budget")
	ErrLengthTooLarge = errors.New("cilus: length exceeds compressed-int max")
)

// maxCompressedUInt is the largest value encodable as a 4-byte ECMA-335
// compressed unsigned integer (29 payload bits). Overridable in tests.
var maxCompressedUInt uint32 = 0x1FFFFFFF

// EncodeUserString encodes s as a full #US heap entry: compressed length
// prefix, then UTF-16LE characters and a terminal byte.
func EncodeUserString(s string) ([]byte, error) {
	body := EncodeBody(s)
	prefix, err := encodeCompressedUInt(uint32(len(body)))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(prefix)+len(body))
	out = append(out, prefix...)
	out = append(out, body...)
	return out, nil
}

// DecodeUserString decodes a full #US entry (compressed length + body).
// Extra trailing bytes after the declared body are ignored.
func DecodeUserString(blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", ErrEmptyBlob
	}
	n, nPrefix, err := decodeCompressedUInt(blob)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("%w: zero-length body", ErrInvalidBody)
	}
	rest := blob[nPrefix:]
	if uint32(len(rest)) < n {
		return "", fmt.Errorf("%w: need %d body bytes, have %d", ErrTruncated, n, len(rest))
	}
	return DecodeBody(rest[:n])
}

// EncodeBody encodes only the body (UTF-16LE + terminal), with no length
// prefix. Always succeeds for any Go string.
//
// For in-place PE overwrite of a fixed stock slot, use EncodeBodyPadded and
// rewrite the compressed length prefix to len(EncodeBody(s)); see package doc.
func EncodeBody(s string) []byte {
	// utf16.Encode handles surrogates for runes > 0xFFFF.
	u16 := utf16.Encode([]rune(s))
	body := make([]byte, 2*len(u16)+1)
	for i, c := range u16 {
		binary.LittleEndian.PutUint16(body[2*i:], c)
	}
	body[len(body)-1] = terminalByte(u16)
	return body
}

// DecodeBody decodes a body-only blob (UTF-16LE + terminal). The terminal
// byte is validated for consistency with the character data but is not
// returned.
func DecodeBody(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("%w: empty body", ErrInvalidBody)
	}
	if len(body)%2 == 0 {
		// Body must be 2*n + 1 (UTF-16 pairs + terminal).
		return "", fmt.Errorf("%w: even length %d (expected odd)", ErrInvalidBody, len(body))
	}
	charBytes := body[:len(body)-1]
	term := body[len(body)-1]
	if term != 0x00 && term != 0x01 {
		return "", fmt.Errorf("%w: terminal byte 0x%02x", ErrInvalidBody, term)
	}
	nChars := len(charBytes) / 2
	u16 := make([]uint16, nChars)
	for i := 0; i < nChars; i++ {
		u16[i] = binary.LittleEndian.Uint16(charBytes[2*i:])
	}
	want := terminalByte(u16)
	if term != want {
		return "", fmt.Errorf("%w: terminal 0x%02x does not match characters (want 0x%02x)", ErrInvalidBody, term, want)
	}
	return string(utf16.Decode(u16)), nil
}

// BodyBudgetBytes returns the number of body bytes EncodeBody would produce
// for s (UTF-16LE + terminal).
func BodyBudgetBytes(s string) int {
	// utf16.Encode length: most BMP chars → 1 unit; astral → 2. We avoid
	// allocating by counting runes and surrogates.
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return 2*n + 1
}

// FitsBudget reports whether EncodeBody(s) fits in budgetBytes (exact ≤).
func FitsBudget(s string, budgetBytes int) bool {
	if budgetBytes < 0 {
		return false
	}
	return BodyBudgetBytes(s) <= budgetBytes
}

// EncodeBodyPadded encodes s as a #US body and zero-fills the remainder of a
// stock slot of size budgetBytes so leftover stock characters cannot leak.
//
// Contract (normative for PE patchers):
//
//   - The returned buffer is budgetBytes long: EncodeBody(s) followed by zeros.
//   - When len(EncodeBody(s)) < budgetBytes, the buffer is NOT a valid #US body
//     of length budgetBytes (terminal is early; zeros are residual orphan bytes).
//   - The caller MUST rewrite the compressed length prefix to len(EncodeBody(s)),
//     then write this padded buffer at the body file offset.
//   - When the new body length equals the stock budget, the length prefix may be
//     left unchanged (same body size).
//
// Returns ErrBudgetExceeded if EncodeBody(s) is longer than budgetBytes, and
// ErrInvalidBudget if budgetBytes is negative.
func EncodeBodyPadded(s string, budgetBytes int) ([]byte, error) {
	if budgetBytes < 0 {
		return nil, ErrInvalidBudget
	}
	body := EncodeBody(s)
	if len(body) > budgetBytes {
		return nil, fmt.Errorf("%w: need %d, budget %d", ErrBudgetExceeded, len(body), budgetBytes)
	}
	if len(body) == budgetBytes {
		return body, nil
	}
	out := make([]byte, budgetBytes)
	copy(out, body)
	// Remaining bytes stay zero (already zeroed by make).
	return out, nil
}

// terminalByte returns the ECMA-335 II.24.2.4 #US terminal byte for u16.
//
// The final byte is 1 if and only if any UTF-16 code unit has any bit set in
// its top byte, or its low byte is any of: 0x01–0x08, 0x0E–0x1F, 0x27, 0x2D,
// 0x7F. Otherwise it is 0.
//
// Note: ASCII hyphen ('-', 0x2D) forces terminal 0x01. Stock VATSIM JWT URLs
// containing "fsd-jwt" therefore end with 0x01, not 0x00. Conversely, many
// BMP characters with a non-zero low byte outside the special set (e.g. 'é'
// U+00E9) yield terminal 0x00 when the high byte is clear.
func terminalByte(u16 []uint16) byte {
	for _, c := range u16 {
		lo := byte(c)
		if c > 0x00FF ||
			(lo >= 0x01 && lo <= 0x08) ||
			(lo >= 0x0E && lo <= 0x1F) ||
			lo == 0x27 || lo == 0x2D || lo == 0x7F {
			return 0x01
		}
	}
	return 0x00
}

// encodeCompressedUInt encodes n as an ECMA-335 compressed unsigned integer
// (II.24.2.4 / blob heap length encoding).
//
//	n ≤ 0x7F        → 1 byte:  n
//	n ≤ 0x3FFF      → 2 bytes: 0x80|hi, lo   (big-endian, high bit set)
//	n ≤ maxCompressedUInt → 4 bytes: 0xC0|b3, b2, b1, b0
func encodeCompressedUInt(n uint32) ([]byte, error) {
	if n > maxCompressedUInt {
		return nil, fmt.Errorf("%w: %d", ErrLengthTooLarge, n)
	}
	switch {
	case n <= 0x7F:
		return []byte{byte(n)}, nil
	case n <= 0x3FFF:
		return []byte{
			byte(0x80 | (n >> 8)),
			byte(n),
		}, nil
	default:
		// n ≤ maxCompressedUInt (and > 0x3FFF) → 4-byte form.
		return []byte{
			byte(0xC0 | (n >> 24)),
			byte(n >> 16),
			byte(n >> 8),
			byte(n),
		}, nil
	}
}

// decodeCompressedUInt reads a compressed unsigned integer from b.
// Returns (value, bytesConsumed, error).
func decodeCompressedUInt(b []byte) (uint32, int, error) {
	if len(b) == 0 {
		return 0, 0, ErrEmptyBlob
	}
	// Top bits of first byte select form:
	//  0xxxxxxx → 1 byte
	//  10xxxxxx → 2 bytes
	//  110xxxxx → 4 bytes
	//  111xxxxx → invalid for unsigned compressed int
	first := b[0]
	switch {
	case first&0x80 == 0:
		return uint32(first), 1, nil
	case first&0xC0 == 0x80:
		if len(b) < 2 {
			return 0, 0, fmt.Errorf("%w: 2-byte length needs 2 bytes", ErrTruncated)
		}
		n := (uint32(first&0x3F) << 8) | uint32(b[1])
		return n, 2, nil
	case first&0xE0 == 0xC0:
		if len(b) < 4 {
			return 0, 0, fmt.Errorf("%w: 4-byte length needs 4 bytes", ErrTruncated)
		}
		n := (uint32(first&0x1F) << 24) |
			(uint32(b[1]) << 16) |
			(uint32(b[2]) << 8) |
			uint32(b[3])
		return n, 4, nil
	default:
		return 0, 0, fmt.Errorf("%w: leading byte 0x%02x", ErrInvalidLength, first)
	}
}
