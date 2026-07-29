package pepatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestOverwriteAt_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	orig := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := OverwriteAt(f, 2, []byte{0xAA, 0xBB}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 1, 0xAA, 0xBB, 4, 5, 6, 7}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestOverwriteAt_BeyondEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = OverwriteAt(f, 2, []byte{9, 9, 9})
	if !errors.Is(err, ErrBeyondEOF) {
		t.Fatalf("err=%v want ErrBeyondEOF", err)
	}
}

func TestOverwriteAt_NegativeOffset(t *testing.T) {
	var buf seekBuffer
	buf.data = []byte{1, 2, 3}
	if err := OverwriteAt(&buf, -1, []byte{1}); err == nil {
		t.Fatal("expected error")
	}
}

func TestOverwriteAt_EmptyData(t *testing.T) {
	var buf seekBuffer
	buf.data = []byte{1, 2, 3}
	if err := OverwriteAt(&buf, 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOverwriteAt_NoStat(t *testing.T) {
	// WriteSeeker without Stat still works (no growth check).
	var buf seekBuffer
	buf.data = make([]byte, 4)
	if err := OverwriteAt(&buf, 1, []byte{9, 9}); err != nil {
		t.Fatal(err)
	}
	if buf.data[1] != 9 || buf.data[2] != 9 {
		t.Fatalf("data=%v", buf.data)
	}
}

func TestOverwriteFileBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("HELLO-XXXXXX"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := OverwriteFileBytes(path, 6, []byte("PLANET")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "HELLO-PLANET" {
		t.Fatalf("got %q", got)
	}
}

func TestOverwriteFileBytes_Missing(t *testing.T) {
	err := OverwriteFileBytes(filepath.Join(t.TempDir(), "nope"), 0, []byte{1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPaddedStringOverwrite_UTF16LE(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pe")
	// 20-byte slot starting at 0.
	buf := make([]byte, 20)
	for i := range buf {
		buf[i] = 0xFF
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := PaddedStringOverwrite(f, 0, "ab", 20, "utf16le"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	// 'a' 'b' + NUL = 6 bytes, rest zeros
	want := make([]byte, 20)
	binary.LittleEndian.PutUint16(want[0:], 'a')
	binary.LittleEndian.PutUint16(want[2:], 'b')
	// null at 4..5, rest 0
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPaddedStringOverwrite_ASCII(t *testing.T) {
	var buf seekBuffer
	buf.data = bytes.Repeat([]byte{0xFF}, 8)
	if err := PaddedStringOverwrite(&buf, 0, "hi", 8, "ascii"); err != nil {
		t.Fatal(err)
	}
	want := []byte{'h', 'i', 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(buf.data, want) {
		t.Fatalf("got %v want %v", buf.data, want)
	}
}

func TestPaddedStringOverwrite_TooLong(t *testing.T) {
	var buf seekBuffer
	buf.data = make([]byte, 4)
	err := PaddedStringOverwrite(&buf, 0, "toolong", 4, "ascii")
	if !errors.Is(err, ErrInvalidSlot) {
		t.Fatalf("err=%v", err)
	}
}

func TestPaddedStringOverwrite_UnknownEncoding(t *testing.T) {
	var buf seekBuffer
	buf.data = make([]byte, 8)
	err := PaddedStringOverwrite(&buf, 0, "x", 8, "ebcdic")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPaddedStringOverwrite_NegativeSlot(t *testing.T) {
	var buf seekBuffer
	buf.data = make([]byte, 4)
	err := PaddedStringOverwrite(&buf, 0, "x", -1, "ascii")
	if !errors.Is(err, ErrInvalidSlot) {
		t.Fatalf("err=%v", err)
	}
}

func TestScanUTF16String(t *testing.T) {
	s := "https://auth.vatsim.net/api/fsd-jwt"
	u := utf16.Encode([]rune(s))
	body := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(body[i*2:], c)
	}
	// Prefix + string + junk + string again
	data := append([]byte{0xDE, 0xAD}, body...)
	data = append(data, 0x00, 0x00)
	data = append(data, body...)
	hits := ScanUTF16String(data, s)
	if len(hits) != 2 {
		t.Fatalf("hits=%v", hits)
	}
	if hits[0] != 2 || hits[1] != int64(2+len(body)+2) {
		t.Fatalf("hits=%v", hits)
	}
}

func TestScanUTF16String_Empty(t *testing.T) {
	if hits := ScanUTF16String([]byte{1, 2}, ""); hits != nil {
		t.Fatalf("hits=%v", hits)
	}
	if hits := ScanUTF16String(nil, "a"); hits != nil {
		t.Fatalf("hits=%v", hits)
	}
}

func TestOverwriteAt_SeekError(t *testing.T) {
	f := &faultySeeker{seekErr: errors.New("seek fail")}
	if err := OverwriteAt(f, 0, []byte{1}); err == nil {
		t.Fatal("expected seek error")
	}
}

func TestOverwriteAt_WriteError(t *testing.T) {
	f := &faultySeeker{data: make([]byte, 4), writeErr: errors.New("write fail")}
	if err := OverwriteAt(f, 0, []byte{1, 2}); err == nil {
		t.Fatal("expected write error")
	}
}

func TestOverwriteAt_ShortWrite(t *testing.T) {
	f := &faultySeeker{data: make([]byte, 4), shortWrite: 1}
	if err := OverwriteAt(f, 0, []byte{1, 2, 3}); err == nil {
		t.Fatal("expected short write")
	}
}

func TestOverwriteAt_StatError(t *testing.T) {
	f := &statFailFile{data: make([]byte, 4), statErr: errors.New("stat fail")}
	if err := OverwriteAt(f, 0, []byte{1}); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestOverwriteFileBytes_BeyondEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("ab"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := OverwriteFileBytes(path, 0, []byte("abcd"))
	if !errors.Is(err, ErrBeyondEOF) {
		t.Fatalf("err=%v", err)
	}
}

func TestScanUTF16String_TooShort(t *testing.T) {
	if hits := ScanUTF16String([]byte{1}, "ab"); hits != nil {
		t.Fatalf("%v", hits)
	}
}

func TestEncodePadded_UTF16Alias(t *testing.T) {
	// via PaddedStringOverwrite encoding aliases
	var buf seekBuffer
	buf.data = make([]byte, 16)
	if err := PaddedStringOverwrite(&buf, 0, "x", 16, "utf-16le"); err != nil {
		t.Fatal(err)
	}
	if err := PaddedStringOverwrite(&buf, 0, "y", 16, "utf16"); err != nil {
		t.Fatal(err)
	}
	if err := PaddedStringOverwrite(&buf, 0, "z", 16, ""); err != nil {
		t.Fatal(err)
	}
}

// seekBuffer is a WriteSeeker without Stat (growth check skipped).
type seekBuffer struct {
	data []byte
	off  int64
}

type faultySeeker struct {
	data       []byte
	off        int64
	seekErr    error
	writeErr   error
	shortWrite int
}

func (f *faultySeeker) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.shortWrite > 0 {
		n := f.shortWrite
		if n > len(p) {
			n = len(p)
		}
		return n, nil
	}
	end := int(f.off) + len(p)
	if end > len(f.data) {
		nd := make([]byte, end)
		copy(nd, f.data)
		f.data = nd
	}
	copy(f.data[f.off:], p)
	f.off += int64(len(p))
	return len(p), nil
}

func (f *faultySeeker) Seek(offset int64, whence int) (int64, error) {
	if f.seekErr != nil {
		return 0, f.seekErr
	}
	f.off = offset
	return offset, nil
}

// statFailFile implements WriteSeeker + Stat for growth-check error path.
type statFailFile struct {
	data    []byte
	off     int64
	statErr error
}

func (s *statFailFile) Write(p []byte) (int, error) {
	copy(s.data[s.off:], p)
	s.off += int64(len(p))
	return len(p), nil
}
func (s *statFailFile) Seek(offset int64, whence int) (int64, error) {
	s.off = offset
	return offset, nil
}
func (s *statFailFile) Stat() (os.FileInfo, error) {
	return nil, s.statErr
}

func (b *seekBuffer) Write(p []byte) (int, error) {
	end := int(b.off) + len(p)
	if end > len(b.data) {
		// extend for no-Stat path tests only
		nd := make([]byte, end)
		copy(nd, b.data)
		b.data = nd
	}
	copy(b.data[b.off:], p)
	b.off += int64(len(p))
	return len(p), nil
}

func (b *seekBuffer) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case 0:
		abs = offset
	case 1:
		abs = b.off + offset
	case 2:
		abs = int64(len(b.data)) + offset
	default:
		return 0, errors.New("bad whence")
	}
	if abs < 0 {
		return 0, errors.New("negative")
	}
	b.off = abs
	return abs, nil
}
