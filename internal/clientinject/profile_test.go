package clientinject

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmbedded(t *testing.T) {
	store, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := store.Get("vpilot-3.12.1")
	if !ok {
		t.Fatal("missing vpilot-3.12.1")
	}
	if p.SchemaVersion != 2 {
		t.Fatalf("schema=%d", p.SchemaVersion)
	}
	if p.ClientID != "vpilot" {
		t.Fatalf("client=%q", p.ClientID)
	}
	if p.PrimaryBinary.SHA1 != "7d95a7110392c15728143cc30e1f00899c686eb5" {
		t.Fatalf("sha1=%q", p.PrimaryBinary.SHA1)
	}
	// Hex offsets
	if p.CLR == nil || p.CLR.USHeap == nil {
		t.Fatal("missing clr.us_heap")
	}
	if p.CLR.USHeap.FileOffset.Int64() != 0xA22E8 {
		t.Fatalf("us_heap offset=%#x", p.CLR.USHeap.FileOffset.Int64())
	}
	js, ok := p.Strings["fsd_jwt"]
	if !ok {
		t.Fatal("missing fsd_jwt string")
	}
	if js.PayloadBudgetBytes != 71 {
		t.Fatalf("budget=%d", js.PayloadBudgetBytes)
	}
	if len(js.BodyFileOffsets) != 1 || js.BodyFileOffsets[0].Int64() != 0xB7988 {
		t.Fatalf("body offs=%v", js.BodyFileOffsets)
	}
	if len(p.USFreeSlots) < 1 {
		t.Fatal("us_free_slots should list free-slot remap targets")
	}
	if p.USFreeSlots[0].BudgetBytes < 71 {
		t.Fatalf("primary free slot budget %d too small for JWT remap", p.USFreeSlots[0].BudgetBytes)
	}
	if p.USFreeSlots[0].HeapOffset.Int64() != 0x12EEE {
		t.Fatalf("primary free slot heap=%#x want 0x12EEE", p.USFreeSlots[0].HeapOffset.Int64())
	}
	if _, ok := store.LookupByHash("vpilot", "7D95A7110392C15728143CC30E1F00899C686EB5"); !ok {
		t.Fatal("lookup by hash case-insensitive failed")
	}
	list := store.ForClient("vpilot")
	if len(list) < 1 {
		t.Fatal("ForClient empty")
	}
	if _, ok := store.Get("xpilot-3.0.1"); ok {
		t.Fatal("xpilot profile must not be embedded")
	}
}

func TestParseProfile_RejectsV1(t *testing.T) {
	yaml := []byte("schema_version: 1\nclient_id: x\nprimary_binary:\n  relative_path: a.exe\n")
	_, err := ParseProfile(yaml, "x")
	if err == nil {
		t.Fatal("expected schema reject")
	}
}

func TestParseProfile_RequiresFields(t *testing.T) {
	_, err := ParseProfile([]byte("schema_version: 2\n"), "x")
	if err == nil {
		t.Fatal("expected client_id required")
	}
	_, err = ParseProfile([]byte("schema_version: 2\nclient_id: c\n"), "x")
	if err == nil {
		t.Fatal("expected primary_binary required")
	}
}

func TestLoadFromDir(t *testing.T) {
	dir := t.TempDir()
	// Copy a minimal v2 profile.
	body := []byte(`schema_version: 2
client_id: fake
display_name: Fake
primary_binary:
  relative_path: fake.exe
  sha1: deadbeefdeadbeefdeadbeefdeadbeefdeadbeef
mutations: []
us_free_slots: []
`)
	if err := os.WriteFile(filepath.Join(dir, "fake-1.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := store.Get("fake-1")
	if !ok || p.ClientID != "fake" {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestFlexibleInt64_HexAndDec(t *testing.T) {
	yaml := []byte(`schema_version: 2
client_id: t
primary_binary:
  relative_path: t.exe
clr:
  us_heap:
    file_offset: 42
    size: 0x10
strings:
  s:
    body_file_offsets: [0xFF, 256]
`)
	p, err := ParseProfile(yaml, "t")
	if err != nil {
		t.Fatal(err)
	}
	if p.CLR.USHeap.FileOffset.Int64() != 42 {
		t.Fatal(p.CLR.USHeap.FileOffset)
	}
	if p.CLR.USHeap.Size.Int64() != 0x10 {
		t.Fatal(p.CLR.USHeap.Size)
	}
	offs := p.Strings["s"].BodyFileOffsets
	if len(offs) != 2 || offs[0].Int64() != 0xFF || offs[1].Int64() != 256 {
		t.Fatalf("%v", offs)
	}
}

func TestProfileStore_Duplicate(t *testing.T) {
	s := NewProfileStore()
	p := &Profile{ProfileID: "a", ClientID: "c", SchemaVersion: 2}
	if err := s.Add(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(p); err == nil {
		t.Fatal("expected duplicate error")
	}
}
