package afv

import (
	"sync"
	"testing"
)

func TestRemoteDir_SnapshotDeltaLeaveRemove(t *testing.T) {
	d := newRemoteDir()
	d.ApplySnapshot("n1", []RemoteSession{
		{Callsign: "AAL1", IsATC: true, Trxs: []Transceiver{{ID: 0, Frequency: 1}}},
		{Callsign: "aal2", IsATC: false, Trxs: nil},
	})
	if d.Count() != 2 || d.CountOrigin("n1") != 2 {
		t.Fatalf("count=%d origin=%d", d.Count(), d.CountOrigin("n1"))
	}
	s, ok := d.Session("n1", "AAL1")
	if !ok || !s.IsATC || len(s.Trxs) != 1 {
		t.Fatalf("%+v ok=%v", s, ok)
	}
	// delta upsert
	d.ApplyDelta("n1", "AAL1", false, []Transceiver{{ID: 1, Frequency: 2}})
	s, _ = d.Session("n1", "aal1")
	if s.IsATC || len(s.Trxs) != 1 || s.Trxs[0].ID != 1 {
		t.Fatalf("%+v", s)
	}
	// leave
	d.ApplyLeave("n1", "AAL2")
	if d.CountOrigin("n1") != 1 {
		t.Fatal()
	}
	d.ApplyLeave("n1", "AAL1")
	if d.Count() != 0 {
		t.Fatal()
	}
	// re-add and remove node
	d.ApplyDelta("n2", "X", true, nil)
	d.ApplyDelta("n1", "Y", false, nil)
	d.RemoveNode("n1")
	if d.CountOrigin("n1") != 0 || d.CountOrigin("n2") != 1 {
		t.Fatal()
	}
	origins := d.Origins()
	if len(origins) != 1 || origins[0] != "n2" {
		t.Fatalf("%v", origins)
	}
}

func TestRemoteDir_Concurrent(t *testing.T) {
	d := newRemoteDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				d.ApplyDelta("n1", "CS", i%2 == 0, []Transceiver{{ID: uint16(j)}})
				d.ApplySnapshot("n2", []RemoteSession{{Callsign: "Z", IsATC: true}})
				_, _ = d.Session("n1", "CS")
				_ = d.Count()
				if j%10 == 0 {
					d.ApplyLeave("n2", "Z")
				}
			}
		}(i)
	}
	wg.Wait()
	d.RemoveNode("n1")
	d.RemoveNode("n2")
	if d.Count() != 0 {
		t.Fatalf("count=%d", d.Count())
	}
}

func TestRemoteDir_NilSafe(t *testing.T) {
	var d *remoteDir
	d.ApplySnapshot("a", nil)
	d.ApplyDelta("a", "b", false, nil)
	d.ApplyLeave("a", "b")
	d.RemoveNode("a")
	if d.Count() != 0 {
		t.Fatal()
	}
	if _, ok := d.Session("a", "b"); ok {
		t.Fatal()
	}
	if d.CountOrigin("a") != 0 {
		t.Fatal()
	}
	if d.Origins() != nil {
		t.Fatal()
	}
}

func TestRemoteDir_EmptyCallsignAndMiss(t *testing.T) {
	d := newRemoteDir()
	d.ApplySnapshot("", []RemoteSession{{Callsign: "X", IsATC: true}})
	if d.Count() != 0 {
		t.Fatal("empty origin should reject")
	}
	d.ApplySnapshot("n", []RemoteSession{{Callsign: "  ", IsATC: true}})
	if d.Count() != 0 {
		t.Fatal("empty callsign should skip")
	}
	d.ApplyDelta("", "X", false, nil)
	d.ApplyDelta("n", "", false, nil)
	if d.Count() != 0 {
		t.Fatal()
	}
	if _, ok := d.Session("missing", "X"); ok {
		t.Fatal()
	}
	d.ApplyDelta("n", "Y", true, []Transceiver{{ID: 1}})
	if _, ok := d.Session("n", "nope"); ok {
		t.Fatal()
	}
	// leave on empty map
	d.ApplyLeave("other", "Y")
	// Remove leaves empty origin map entry cleanup already tested
	d.ApplyLeave("n", "Y")
	if len(d.Origins()) != 0 {
		t.Fatal()
	}
}
