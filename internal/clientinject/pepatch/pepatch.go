// Package pepatch provides stdlib-only PE / binary overwrite helpers for
// on-disk client injection (padded strings, offset overwrites, UTF-16 scans).
package pepatch

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// ErrBeyondEOF is returned when a write would grow the file beyond its size
// (OverwriteAt never extends).
var ErrBeyondEOF = errors.New("pepatch: write would extend past end of file")

// ErrInvalidSlot is returned when a padded overwrite cannot fit the string.
var ErrInvalidSlot = errors.New("pepatch: string does not fit slot")

// OverwriteAt seeks to offset and writes data. The write must not grow the file
// beyond its current size (caller must know the file length). f must support
// Seek and Write.
func OverwriteAt(f io.WriteSeeker, offset int64, data []byte) error {
	if offset < 0 {
		return fmt.Errorf("pepatch: negative offset %d", offset)
	}
	if len(data) == 0 {
		return nil
	}
	// Prefer Size when available (os.File) so we refuse growth.
	if sizer, ok := f.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, err := sizer.Stat()
		if err != nil {
			return fmt.Errorf("pepatch: stat: %w", err)
		}
		if offset+int64(len(data)) > info.Size() {
			return fmt.Errorf("%w: offset=%d len=%d size=%d", ErrBeyondEOF, offset, len(data), info.Size())
		}
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("pepatch: seek %d: %w", offset, err)
	}
	n, err := f.Write(data)
	if err != nil {
		return fmt.Errorf("pepatch: write at %d: %w", offset, err)
	}
	if n != len(data) {
		return fmt.Errorf("pepatch: short write at %d: %d/%d", offset, n, len(data))
	}
	return nil
}

// OverwriteFileBytes opens path read-write, overwrites at offset, and closes.
func OverwriteFileBytes(path string, offset int64, data []byte) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("pepatch: open %s: %w", path, err)
	}
	defer f.Close()
	if err := OverwriteAt(f, offset, data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("pepatch: sync %s: %w", path, err)
	}
	return nil
}

// PaddedStringOverwrite writes s encoded per encoding ("utf16le" or "ascii")
// into a fixed slot of slotLen bytes at offset, zero-padding the remainder.
// The encoded form (including a trailing NUL for both encodings) must fit in
// slotLen; remaining bytes are filled with 0x00.
func PaddedStringOverwrite(f io.WriteSeeker, offset int64, s string, slotLen int, encoding string) error {
	if slotLen < 0 {
		return fmt.Errorf("%w: negative slotLen", ErrInvalidSlot)
	}
	payload, err := encodePadded(s, slotLen, encoding)
	if err != nil {
		return err
	}
	return OverwriteAt(f, offset, payload)
}

func encodePadded(s string, slotLen int, encoding string) ([]byte, error) {
	enc := strings.ToLower(strings.TrimSpace(encoding))
	var raw []byte
	switch enc {
	case "utf16le", "utf-16le", "utf16":
		// UTF-16LE code units + trailing U+0000 (2 bytes).
		u := utf16.Encode([]rune(s))
		raw = make([]byte, (len(u)+1)*2)
		for i, c := range u {
			binary.LittleEndian.PutUint16(raw[i*2:], c)
		}
		// trailing null already zero
	case "ascii", "utf8", "latin1", "":
		// Bytes + trailing 0x00.
		raw = make([]byte, len(s)+1)
		copy(raw, []byte(s))
	default:
		return nil, fmt.Errorf("pepatch: unknown encoding %q", encoding)
	}
	if len(raw) > slotLen {
		return nil, fmt.Errorf("%w: encoded %d bytes > slot %d", ErrInvalidSlot, len(raw), slotLen)
	}
	out := make([]byte, slotLen)
	copy(out, raw)
	return out, nil
}

// ScanUTF16String finds all file offsets of s encoded as UTF-16LE without a
// trailing null (raw body match). Overlapping matches are reported separately
// only when they start at distinct offsets.
func ScanUTF16String(data []byte, s string) []int64 {
	if s == "" || len(data) < 2 {
		return nil
	}
	u := utf16.Encode([]rune(s))
	need := len(u) * 2
	if need == 0 || need > len(data) {
		return nil
	}
	pat := make([]byte, need)
	for i, c := range u {
		binary.LittleEndian.PutUint16(pat[i*2:], c)
	}
	var hits []int64
	// Byte-aligned scan; PE strings are 2-byte aligned but residual scan is exhaustive.
	for i := 0; i+need <= len(data); i++ {
		if bytesEqual(data[i:i+need], pat) {
			hits = append(hits, int64(i))
		}
	}
	return hits
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
