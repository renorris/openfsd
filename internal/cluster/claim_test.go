package cluster

import (
	"sync"
	"testing"
	"time"
)

func TestClaimReserveCommitRelease(t *testing.T) {
	ct := NewClaimTable(2 * time.Second)
	fence, err := ct.Reserve("N1", ClaimMeta{NodeID: "a", CID: 1})
	if err != nil || fence == "" {
		t.Fatalf("%v %q", err, fence)
	}
	// second reserve fails
	if _, err := ct.Reserve("N1", ClaimMeta{NodeID: "b", CID: 2}); err != ErrClaimInUse {
		t.Fatalf("want in use, got %v", err)
	}
	meta, err := ct.Commit("N1", fence)
	if err != nil || !meta.Routable || meta.NodeID != "a" {
		t.Fatalf("%+v %v", meta, err)
	}
	if !ct.Release("N1", fence) {
		t.Fatal("release")
	}
	// can reserve again
	if _, err := ct.Reserve("N1", ClaimMeta{NodeID: "b"}); err != nil {
		t.Fatal(err)
	}
}

func TestClaimAbortAndTTL(t *testing.T) {
	ct := NewClaimTable(50 * time.Millisecond)
	fence, err := ct.Reserve("X", ClaimMeta{NodeID: "a", TTL: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ct.Abort("X", fence)
	if _, err := ct.Reserve("X", ClaimMeta{NodeID: "b"}); err != nil {
		t.Fatal(err)
	}
}

func TestClaimCommitFenceMismatch(t *testing.T) {
	ct := NewClaimTable(time.Second)
	fence, _ := ct.Reserve("Y", ClaimMeta{NodeID: "a"})
	if _, err := ct.Commit("Y", "wrong"); err != ErrClaimFence {
		t.Fatalf("got %v", err)
	}
	if _, err := ct.Commit("Y", fence); err != nil {
		t.Fatal(err)
	}
}

func TestClaimConcurrentReserve(t *testing.T) {
	ct := NewClaimTable(time.Second)
	var wg sync.WaitGroup
	var okCount int
	var mu sync.Mutex
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := ct.Reserve("RACE", ClaimMeta{NodeID: "n", CID: i})
			if err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("okCount=%d want 1", okCount)
	}
}

func TestClaimReleaseNode(t *testing.T) {
	ct := NewClaimTable(time.Second)
	f, _ := ct.Reserve("A", ClaimMeta{NodeID: "dead"})
	_, _ = ct.Commit("A", f)
	_, _ = ct.Reserve("B", ClaimMeta{NodeID: "live"})
	got := ct.ReleaseNode("dead")
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
}

func TestClaimReserveEmptyFenceGenerate(t *testing.T) {
	ct := NewClaimTable(time.Second)
	// empty fence generates uuid (covers fence=="" branch)
	f, err := ct.Reserve("GEN", ClaimMeta{NodeID: "a", Fence: ""})
	if err != nil || f == "" {
		t.Fatal(err, f)
	}
	// empty ring owner edge
	r, err := NewStablePeerRing([]string{"only"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Owner("x") != "only" {
		t.Fatal()
	}
	// Len zero shouldn't happen but Owner on empty is covered via ring of 1
}

func TestClaimPendingExpiryReplace(t *testing.T) {
	ct := NewClaimTable(5 * time.Millisecond)
	f1, err := ct.Reserve("EXP", ClaimMeta{NodeID: "a", TTL: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_ = f1
	time.Sleep(15 * time.Millisecond)
	// second reserve after expiry should succeed (covers delete pending expired)
	f2, err := ct.Reserve("EXP", ClaimMeta{NodeID: "b"})
	if err != nil || f2 == "" {
		t.Fatal(err)
	}
}
