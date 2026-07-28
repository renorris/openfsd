package cluster

import "testing"

func TestDirectoryLookupRoutableOnly(t *testing.T) {
	d := NewDirectory()
	d.ApplyJoin(DirMeta{Callsign: "N1", NodeID: "us", Routable: true, CID: 1})
	_, _, ok := d.Lookup("N1")
	if !ok {
		t.Fatal("expected ok")
	}
	// pending-style leave
	d.ApplyLeave("N1")
	if _, _, ok := d.Lookup("N1"); ok {
		t.Fatal("expected miss")
	}
}

func TestDirectoryMetaFPL(t *testing.T) {
	d := NewDirectory()
	d.ApplyJoin(DirMeta{Callsign: "N2", NodeID: "eu", Routable: true})
	fpl := "VFR:IFR"
	d.ApplyMeta("N2", &fpl, nil)
	_, m, ok := d.Lookup("N2")
	if !ok || m.FPLInfo != fpl {
		t.Fatalf("%+v", m)
	}
}

func TestDirectoryRemoveNode(t *testing.T) {
	d := NewDirectory()
	d.ApplyJoin(DirMeta{Callsign: "A", NodeID: "x", Routable: true})
	d.ApplyJoin(DirMeta{Callsign: "B", NodeID: "y", Routable: true})
	css := d.RemoveNode("x")
	if len(css) != 1 {
		t.Fatalf("%v", css)
	}
	if _, _, ok := d.Lookup("A"); ok {
		t.Fatal("A should be gone")
	}
	if _, _, ok := d.Lookup("B"); !ok {
		t.Fatal("B should remain")
	}
}

func TestDirectorySnapshotMerge(t *testing.T) {
	d := NewDirectory()
	d.ApplySnapshot([]DirMeta{
		{Callsign: "P1", NodeID: "a", Routable: true, Fence: "f1"},
	})
	if n := len(d.Snapshot()); n != 1 {
		t.Fatalf("%d", n)
	}
}
