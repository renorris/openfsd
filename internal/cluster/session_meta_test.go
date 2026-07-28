package cluster

import "testing"

func TestSessionMetaRoundTrip(t *testing.T) {
	raw := EncodeSessionMeta(SessionMeta{FPLInfo: "VFR:IFR", Beacon: "7700", IsATC: false, Rating: 1})
	m, err := DecodeSessionMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.FPLInfo != "VFR:IFR" || m.Beacon != "7700" || m.IsATC || m.Rating != 1 {
		t.Fatalf("%+v", m)
	}
	// CR/LF stripped from FPL
	raw2 := EncodeSessionMeta(SessionMeta{FPLInfo: "a\r\nb", Beacon: "1\n2", IsATC: true, Rating: 11})
	m2, err := DecodeSessionMeta(raw2)
	if err != nil {
		t.Fatal(err)
	}
	if m2.FPLInfo != "ab" || m2.Beacon != "12" || !m2.IsATC {
		t.Fatalf("%+v", m2)
	}
}

func TestApplySnapshotFromOwnership(t *testing.T) {
	d := NewDirectory()
	d.ApplySnapshotFrom("peer-a", []DirMeta{
		{Callsign: "A", NodeID: "peer-a", Routable: true},
		{Callsign: "B", NodeID: "peer-b", Routable: true}, // rejected
	})
	if _, _, ok := d.Lookup("A"); !ok {
		t.Fatal("A missing")
	}
	if _, _, ok := d.Lookup("B"); ok {
		t.Fatal("B foreign ownership should be rejected")
	}
}

func TestDecodeSessionMetaErrors(t *testing.T) {
	if _, err := DecodeSessionMeta(nil); err == nil {
		t.Fatal()
	}
	if _, err := DecodeSessionMeta([]byte{0, 1}); err == nil {
		t.Fatal()
	}
}

func TestReleaseEmptyFenceClaimTable(t *testing.T) {
	ct := NewClaimTable(0)
	f, _ := ct.Reserve("Z", ClaimMeta{NodeID: "n", Fence: "f"})
	if ct.Release("Z", "") {
		t.Fatal()
	}
	if !ct.Release("Z", f) {
		t.Fatal()
	}
	// Abort empty fence no-op
	ct.Abort("Z", "")
}

func TestApplySnapshotFromNoSteal(t *testing.T) {
	d := NewDirectory()
	d.ApplyJoin(DirMeta{Callsign: "LIVE", NodeID: "owner", Routable: true})
	// Peer tries to re-home via snapshot
	d.ApplySnapshotFrom("attacker", []DirMeta{
		{Callsign: "LIVE", NodeID: "attacker", Routable: true},
	})
	_, meta, ok := d.Lookup("LIVE")
	if !ok || meta.NodeID != "owner" {
		t.Fatalf("ownership stolen: %+v ok=%v", meta, ok)
	}
}

func TestDecodeTextRangedPayload(t *testing.T) {
	var p []byte
	p = encodeU32(p, 1)
	p = encodeF64(p, 1)
	p = encodeF64(p, 2)
	p = encodeF64(p, 3)
	p = encodeF64(p, 4)
	p = append(p, []byte("#TM\r\n")...)
	boxes, wire, err := DecodeTextRangedPayload(p)
	if err != nil || len(boxes) != 1 || string(wire) != "#TM\r\n" {
		t.Fatalf("%v %v %q", boxes, err, wire)
	}
	if boxes[0].MinLat != 1 || boxes[0].MaxLon != 4 {
		t.Fatal(boxes[0])
	}
	// error path
	if _, _, err := DecodeTextRangedPayload([]byte{0}); err == nil {
		t.Fatal()
	}
}
