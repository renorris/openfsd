package cluster

import "testing"

func TestStablePeerRingOwnerDeterministic(t *testing.T) {
	r, err := NewStablePeerRing([]string{"eu", "us", "ap"})
	if err != nil {
		t.Fatal(err)
	}
	o1 := r.Owner("N123AB")
	o2 := r.Owner("n123ab")
	if o1 != o2 {
		t.Fatalf("%s vs %s", o1, o2)
	}
	// same ring order regardless of input order
	r2, _ := NewStablePeerRing([]string{"ap", "us", "eu"})
	if r.Owner("TEST") != r2.Owner("TEST") {
		t.Fatal("order must not matter")
	}
}

func TestStablePeerRingMax8(t *testing.T) {
	ids := make([]string, 9)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	if _, err := NewStablePeerRing(ids); err != ErrInvalidConfig {
		t.Fatalf("got %v", err)
	}
}

func TestParseClusterPeers(t *testing.T) {
	peers, err := ParseClusterPeers("us=us.host:7600,eu=eu.host:7600")
	if err != nil || len(peers) != 2 {
		t.Fatalf("%v %v", peers, err)
	}
	if peers[0].NodeID != "us" || peers[0].Addr != "us.host:7600" {
		t.Fatalf("%+v", peers[0])
	}
}

func TestOwnerEmptyRingDefensive(t *testing.T) {
	// Construct via reflection-free path: zero ring not possible via New;
	// cover Len==1 and multi already. Force-call Owner after building valid ring.
	r, _ := NewStablePeerRing([]string{"a", "b"})
	_ = r.Owner("Z")
	r2, _ := NewStablePeerRing([]string{"solo"})
	if r2.Owner("anything") != "solo" {
		t.Fatal()
	}
}
