package postoffice

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/session"
)

// setLinearSearchThreshold overrides the large-N threshold for the duration of t.
func setLinearSearchThreshold(t *testing.T, n int) {
	t.Helper()
	old := linearSearchThreshold
	linearSearchThreshold = n
	t.Cleanup(func() { linearSearchThreshold = old })
}

func callsignsOf(ss []*session.Session) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Callsign
	}
	sort.Strings(out)
	return out
}

func searchCallsigns(p *PostOffice, self *session.Session) []string {
	var found []*session.Session
	p.Search(self, func(r *session.Session) bool {
		found = append(found, r)
		return true
	})
	return callsignsOf(found)
}

func searchATCCallsigns(p *PostOffice, self *session.Session) []string {
	var found []*session.Session
	p.SearchATC(self, func(r *session.Session) bool {
		found = append(found, r)
		return true
	})
	return callsignsOf(found)
}

// TestSearchATC_LinearPath yields only ATC peers on the lock-free snapshot path.
func TestSearchATC_LinearPath(t *testing.T) {
	p := New()
	pilot := newTestClient("PIL1", 34.0, -118.0, 200*1852)
	pilot.IsAtc = false
	atc1 := newTestClient("TWR1", 34.0, -118.0, 200*1852)
	atc1.IsAtc = true
	atc2 := newTestClient("GND1", 34.01, -118.01, 200*1852)
	atc2.IsAtc = true
	far := newTestClient("FAR_ATC", 50.0, 10.0, 50*1852)
	far.IsAtc = true
	otherPilot := newTestClient("PIL2", 34.0, -118.0, 200*1852)

	for _, c := range []*session.Session{pilot, atc1, atc2, far, otherPilot} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if p.treeReady.Load() {
		t.Fatal("expected linear mode (treeReady=false) for small N")
	}

	got := searchATCCallsigns(p, pilot)
	want := []string{"GND1", "TWR1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("SearchATC = %v, want %v", got, want)
	}

	// ATC searching should not see itself.
	got = searchATCCallsigns(p, atc1)
	want = []string{"GND1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("SearchATC(self ATC) = %v, want %v", got, want)
	}
}

// TestSearchATC_EarlyStop honors false from the callback.
func TestSearchATC_EarlyStop(t *testing.T) {
	p := New()
	self := newTestClient("self", 0, 0, 500000)
	a := newTestClient("A_ATC", 0, 0, 500000)
	a.IsAtc = true
	b := newTestClient("B_ATC", 0, 0, 500000)
	b.IsAtc = true
	for _, c := range []*session.Session{self, a, b} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	p.SearchATC(self, func(r *session.Session) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("early-stop SearchATC invoked callback %d times, want 1", count)
	}
}

// TestThresholdCrossing_TreeMode enables the R-tree once N exceeds the threshold
// and Search results stay consistent with the linear path.
func TestThresholdCrossing_TreeMode(t *testing.T) {
	const thr = 4
	setLinearSearchThreshold(t, thr)

	p := New()
	// Cluster of thr+2 co-located clients so range searches hit everyone.
	clients := make([]*session.Session, thr+2)
	for i := 0; i < thr+2; i++ {
		c := newTestClient(fmt.Sprintf("C%d", i), 34.0, -118.0, 100*1852)
		if i%3 == 0 {
			c.IsAtc = true
		}
		clients[i] = c
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}

	if !p.treeReady.Load() {
		t.Fatal("expected treeReady after crossing threshold")
	}
	if p.liveLen() != thr+2 {
		t.Fatalf("liveLen=%d want %d", p.liveLen(), thr+2)
	}

	// Every client should see all others via Search (tree path).
	for _, self := range clients {
		got := searchCallsigns(p, self)
		if len(got) != thr+1 {
			t.Fatalf("%s Search len=%d want %d (%v)", self.Callsign, len(got), thr+1, got)
		}
	}

	// SearchATC must only return ATC and never self.
	for _, self := range clients {
		got := searchATCCallsigns(p, self)
		for _, cs := range got {
			found, err := p.Find(cs)
			if err != nil {
				t.Fatal(err)
			}
			if !found.IsAtc {
				t.Fatalf("SearchATC returned non-ATC %s", cs)
			}
			if found == self {
				t.Fatalf("SearchATC included self %s", cs)
			}
		}
	}
}

// TestThresholdDrop_ReturnsToLinear clears treeReady when N falls back.
func TestThresholdDrop_ReturnsToLinear(t *testing.T) {
	const thr = 3
	setLinearSearchThreshold(t, thr)

	p := New()
	clients := make([]*session.Session, thr+1)
	for i := 0; i < thr+1; i++ {
		c := newTestClient(fmt.Sprintf("D%d", i), 0, 0, 500000)
		clients[i] = c
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if !p.treeReady.Load() {
		t.Fatal("expected tree mode")
	}

	// Drop one → at threshold → linear again.
	p.Release(clients[0])
	if p.treeReady.Load() {
		t.Fatal("expected treeReady=false after dropping to threshold")
	}
	if p.liveLen() != thr {
		t.Fatalf("liveLen=%d want %d", p.liveLen(), thr)
	}

	// Remaining clients still find each other on the linear path.
	got := searchCallsigns(p, clients[1])
	if len(got) != thr-1 {
		t.Fatalf("Search after drop len=%d want %d (%v)", len(got), thr-1, got)
	}

	// Re-cross threshold: tree comes back and Search remains correct.
	extra := newTestClient("EXTRA", 0, 0, 500000)
	if err := p.Register(extra); err != nil {
		t.Fatal(err)
	}
	if !p.treeReady.Load() {
		t.Fatal("expected treeReady after re-crossing")
	}
	got = searchCallsigns(p, extra)
	if len(got) != thr {
		t.Fatalf("Search after re-cross len=%d want %d (%v)", len(got), thr, got)
	}
}

// TestUpdatePosition_TreeMode rewrites the index so distant peers fall out of range.
func TestUpdatePosition_TreeMode(t *testing.T) {
	const thr = 2
	setLinearSearchThreshold(t, thr)

	p := New()
	a := newTestClient("A", 34.0, -118.0, 50*1852)
	b := newTestClient("B", 34.0, -118.0, 50*1852)
	c := newTestClient("C", 34.0, -118.0, 50*1852)
	for _, s := range []*session.Session{a, b, c} {
		if err := p.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	if !p.treeReady.Load() {
		t.Fatal("expected tree mode")
	}

	got := searchCallsigns(p, a)
	if len(got) != 2 {
		t.Fatalf("before move Search=%v", got)
	}

	// Move B far away; quantized box must change so the tree entry is rewritten.
	p.UpdatePosition(b, [2]float64{50.0, 10.0}, 50*1852)
	got = searchCallsigns(p, a)
	if fmt.Sprint(got) != fmt.Sprint([]string{"C"}) {
		t.Fatalf("after move Search=%v, want [C]", got)
	}

	// No-op quantized update keeps B searchable from its new home (with a peer there).
	p.UpdatePosition(b, [2]float64{50.0, 10.0}, 50*1852)
	peer := newTestClient("PEER", 50.0, 10.0, 50*1852)
	if err := p.Register(peer); err != nil {
		t.Fatal(err)
	}
	got = searchCallsigns(p, peer)
	if !contains(got, "B") {
		t.Fatalf("peer near B should find B after noop update, got %v", got)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestLinearVsTree_SearchEquivalence registers the same geometry under both modes
// and asserts identical recipient sets (modulo order).
func TestLinearVsTree_SearchEquivalence(t *testing.T) {
	// Fixed geometry: mixed ranges so some pairs overlap and some do not.
	type place struct {
		cs       string
		lat, lon float64
		vr       float64
		atc      bool
	}
	places := []place{
		{"P0", 34.00, -118.00, 80 * 1852, false},
		{"P1", 34.10, -118.00, 80 * 1852, false},
		{"P2", 34.50, -118.00, 40 * 1852, true}, // farther; may not see P0 depending on ranges
		{"P3", 40.00, -100.00, 30 * 1852, true}, // far away
		{"P4", 34.05, -118.05, 100 * 1852, false},
	}

	build := func(thr int) *PostOffice {
		setLinearSearchThreshold(t, thr)
		p := New()
		for _, pl := range places {
			c := newTestClient(pl.cs, pl.lat, pl.lon, pl.vr)
			c.IsAtc = pl.atc
			if err := p.Register(c); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}

	// Linear: threshold above N.
	linear := build(100)
	if linear.treeReady.Load() {
		t.Fatal("linear fixture unexpectedly in tree mode")
	}

	// Tree: threshold below N.
	tree := build(2)
	if !tree.treeReady.Load() {
		t.Fatal("tree fixture not in tree mode")
	}

	for _, pl := range places {
		selfL, err := linear.Find(pl.cs)
		if err != nil {
			t.Fatal(err)
		}
		selfT, err := tree.Find(pl.cs)
		if err != nil {
			t.Fatal(err)
		}
		gotL := searchCallsigns(linear, selfL)
		gotT := searchCallsigns(tree, selfT)
		if fmt.Sprint(gotL) != fmt.Sprint(gotT) {
			t.Fatalf("Search(%s) linear=%v tree=%v", pl.cs, gotL, gotT)
		}
		gotAL := searchATCCallsigns(linear, selfL)
		gotAT := searchATCCallsigns(tree, selfT)
		if fmt.Sprint(gotAL) != fmt.Sprint(gotAT) {
			t.Fatalf("SearchATC(%s) linear=%v tree=%v", pl.cs, gotAL, gotAT)
		}
	}
}

// TestConcurrentRegisterAcrossThreshold hammers Register around the threshold
// and asserts the registry ends consistent (no lost sessions, Search works).
func TestConcurrentRegisterAcrossThreshold(t *testing.T) {
	const thr = 8
	const n = 32
	setLinearSearchThreshold(t, thr)

	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		// Slight spatial spread so the tree has non-trivial boxes.
		clients[i] = newTestClient(
			fmt.Sprintf("R%d", i),
			34.0+float64(i%8)*0.01,
			-118.0+float64(i/8)*0.01,
			100*1852,
		)
		if i%5 == 0 {
			clients[i].IsAtc = true
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(c *session.Session) {
			defer wg.Done()
			if err := p.Register(c); err != nil {
				t.Errorf("register %s: %v", c.Callsign, err)
			}
		}(clients[i])
	}
	wg.Wait()

	if p.liveLen() != n {
		t.Fatalf("liveLen=%d want %d", p.liveLen(), n)
	}
	if !p.treeReady.Load() {
		t.Fatal("expected treeReady after concurrent register past threshold")
	}
	snap := p.Snapshot()
	if len(snap) != n {
		t.Fatalf("Snapshot len=%d want %d", len(snap), n)
	}

	// Every registered callsign must be Find-able and appear in some Search.
	for _, c := range clients {
		got, err := p.Find(c.Callsign)
		if err != nil || got != c {
			t.Fatalf("Find(%s)=(%v,%v)", c.Callsign, got, err)
		}
	}

	// Concurrent Search + UpdatePosition must not race or panic.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(c *session.Session) {
			defer wg.Done()
			p.Search(c, func(r *session.Session) bool { return true })
			p.SearchATC(c, func(r *session.Session) bool { return true })
			ll := c.LatLon()
			p.UpdatePosition(c, [2]float64{ll[0] + 0.02, ll[1] - 0.02}, 80*1852)
			p.Search(c, func(r *session.Session) bool { return true })
		}(clients[i])
	}
	wg.Wait()

	// Concurrent release of half, then the rest.
	for i := 0; i < n/2; i++ {
		wg.Add(1)
		go func(c *session.Session) {
			defer wg.Done()
			p.Release(c)
		}(clients[i])
	}
	wg.Wait()
	if p.liveLen() != n/2 {
		t.Fatalf("after half release liveLen=%d want %d", p.liveLen(), n/2)
	}

	for i := n / 2; i < n; i++ {
		wg.Add(1)
		go func(c *session.Session) {
			defer wg.Done()
			p.Release(c)
		}(clients[i])
	}
	wg.Wait()

	if p.liveLen() != 0 {
		t.Fatalf("after full release liveLen=%d", p.liveLen())
	}
	if p.treeReady.Load() {
		t.Fatal("expected treeReady=false on empty registry")
	}
	if len(p.Snapshot()) != 0 {
		t.Fatalf("Snapshot not empty: %d", len(p.Snapshot()))
	}
}

// TestSearch_DoesNotHoldLockAcrossCallback verifies callbacks may block on
// Session.Send without deadlocking concurrent Register/Release/Search.
func TestSearch_DoesNotHoldLockAcrossCallback(t *testing.T) {
	p := New()
	self := newTestClient("self", 0, 0, 500000)
	peer := newTestClient("peer", 0, 0, 500000)
	// Fill peer outbound so Send would block if the buffer is full.
	for i := 0; i < 32; i++ {
		if err := peer.Send("pad"); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []*session.Session{self, peer} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}

	// Run Search that tries blocking Send in a goroutine; ensure another
	// Register can still complete (proves no lock held across callback).
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		p.Search(self, func(r *session.Session) bool {
			// This will block until we cancel the peer context.
			_ = r.Send("blocked\r\n")
			return true
		})
		close(done)
	}()
	<-started
	time.Sleep(20 * time.Millisecond)

	extra := newTestClient("extra", 0, 0, 500000)
	regDone := make(chan error, 1)
	go func() { regDone <- p.Register(extra) }()

	select {
	case err := <-regDone:
		if err != nil {
			t.Fatalf("Register during blocked Search callback: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Register blocked — Search likely held a postoffice lock across Send")
	}

	peer.Cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Search did not finish after peer Cancel")
	}
}

// TestBroadcastFanout_SendPositionDoesNotStall ensures a slow peer with a full
// buffer cannot stall position fan-out to other recipients (UX-critical path).
func TestBroadcastFanout_SendPositionDoesNotStall(t *testing.T) {
	p := New()
	src := newTestClient("SRC", 34.0, -118.0, 100*1852)
	slow := newTestClient("SLOW", 34.0, -118.0, 100*1852)
	fast := newTestClient("FAST", 34.0, -118.0, 100*1852)
	for _, c := range []*session.Session{src, slow, fast} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	// Saturate slow peer's outbound channel.
	for i := 0; i < 32; i++ {
		if err := slow.Send("old"); err != nil {
			t.Fatal(err)
		}
	}

	pkt := "@S:SRC:1200:1:34.0:-118.0:5000:250:0:0\r\n"
	start := time.Now()
	p.Search(src, func(r *session.Session) bool {
		_ = r.SendPosition(pkt)
		return true
	})
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("fan-out took %v — SendPosition appears to block on slow peer", elapsed)
	}

	// Fast peer must have received the fresh position.
	var saw bool
	for {
		out, ok := fast.DequeueOutbound()
		if !ok {
			break
		}
		if out == pkt {
			saw = true
		}
	}
	if !saw {
		t.Fatal("FAST peer did not receive position packet")
	}

	// Slow peer should also have the latest position (latest-wins drop).
	saw = false
	for {
		out, ok := slow.DequeueOutbound()
		if !ok {
			break
		}
		if out == pkt {
			saw = true
		}
	}
	if !saw {
		t.Fatal("SLOW peer should still receive latest position via SendPosition drop policy")
	}
}

// TestSnapshotCOW_Isolation ensures live snapshot readers are not mutated by
// later Register/Release (copy-on-write).
func TestSnapshotCOW_Isolation(t *testing.T) {
	p := New()
	a := newTestClient("A", 0, 0, 1000)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	live1 := p.live.Load()
	if live1 == nil || len(*live1) != 1 {
		t.Fatalf("live1 = %v", live1)
	}

	b := newTestClient("B", 0, 0, 1000)
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}
	// Prior snapshot slice must be unchanged (COW).
	if len(*live1) != 1 || (*live1)[0] != a {
		t.Fatalf("live1 mutated after Register: %v", *live1)
	}
	live2 := p.live.Load()
	if live2 == nil || len(*live2) != 2 {
		t.Fatalf("live2 len=%v", live2)
	}

	p.Release(a)
	if len(*live2) != 2 {
		t.Fatalf("live2 mutated after Release: len=%d", len(*live2))
	}
	live3 := p.live.Load()
	if live3 == nil || len(*live3) != 1 || (*live3)[0] != b {
		t.Fatalf("live3 = %v", live3)
	}
}

// TestATCSnapshot_TracksATCOnly keeps atcLive consistent across Register/Release.
func TestATCSnapshot_TracksATCOnly(t *testing.T) {
	p := New()
	pilot := newTestClient("P", 0, 0, 1000)
	atc := newTestClient("TWR", 0, 0, 1000)
	atc.IsAtc = true
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(atc); err != nil {
		t.Fatal(err)
	}
	atcLive := p.atcLive.Load()
	if atcLive == nil || len(*atcLive) != 1 || (*atcLive)[0] != atc {
		t.Fatalf("atcLive after register = %v", atcLive)
	}

	p.Release(pilot)
	atcLive = p.atcLive.Load()
	if atcLive == nil || len(*atcLive) != 1 || (*atcLive)[0] != atc {
		t.Fatalf("atcLive after pilot release = %v", atcLive)
	}

	p.Release(atc)
	atcLive = p.atcLive.Load()
	if atcLive == nil || len(*atcLive) != 0 {
		t.Fatalf("atcLive after atc release = %v", atcLive)
	}
}

// TestReleaseUnregistered is a no-crash soft path (disconnect races).
func TestReleaseUnregistered(t *testing.T) {
	p := New()
	ghost := newTestClient("GHOST", 0, 0, 1000)
	p.Release(ghost) // must not panic
	if p.liveLen() != 0 {
		t.Fatalf("liveLen=%d", p.liveLen())
	}
}

// TestFoundPool_OversizedDropped ensures huge recipient slices are not pooled.
func TestFoundPool_OversizedDropped(t *testing.T) {
	// Build a slice larger than the pool cap limit and release it; a subsequent
	// acquire must not return the oversized buffer.
	big := make([]*session.Session, 0, 5000)
	ptr := &big
	releaseFound(ptr)

	got := acquireFound()
	if cap(*got) > 4096 {
		t.Fatalf("acquireFound returned oversized buffer cap=%d", cap(*got))
	}
	releaseFound(got)
}

// TestSearch_ConcurrentWithPositionStorm mimics dense event load: many clients
// updating positions while others Search. Must stay race-clean and not drop
// registry entries.
func TestSearch_ConcurrentWithPositionStorm(t *testing.T) {
	const thr = 16
	const n = 48
	setLinearSearchThreshold(t, thr)

	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(
			fmt.Sprintf("S%d", i),
			34.0+float64(i%10)*0.02,
			-118.0+float64(i/10)*0.02,
			80*1852,
		)
		if err := p.Register(clients[i]); err != nil {
			t.Fatal(err)
		}
	}

	var ops atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Position updaters.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(c *session.Session, idx int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				lat := 34.0 + float64((idx+int(ops.Load()))%20)*0.01
				lon := -118.0 + float64((idx*3)%20)*0.01
				p.UpdatePosition(c, [2]float64{lat, lon}, 80*1852)
				ops.Add(1)
			}
		}(clients[i], i)
	}

	// Searchers / fan-out.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				src := clients[idx%n]
				p.Search(src, func(r *session.Session) bool {
					_ = r.SendPosition("pos\r\n")
					_, _ = r.DequeueOutbound()
					return true
				})
				ops.Add(1)
			}
		}(i)
	}

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	if p.liveLen() != n {
		t.Fatalf("liveLen drifted to %d want %d", p.liveLen(), n)
	}
	if ops.Load() == 0 {
		t.Fatal("no ops executed")
	}
}

// TestAll_NilExcept iterates the entire registry.
func TestAll_NilExcept(t *testing.T) {
	p := New()
	a := newTestClient("A", 0, 0, 1000)
	b := newTestClient("B", 0, 0, 1000)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}
	var seen []string
	p.All(nil, func(r *session.Session) bool {
		seen = append(seen, r.Callsign)
		return true
	})
	sort.Strings(seen)
	if fmt.Sprint(seen) != fmt.Sprint([]string{"A", "B"}) {
		t.Fatalf("All(nil) = %v", seen)
	}
}

// TestAllSnapshot_MapFallback covers the defensive map-locked paths used when
// the atomic live pointer is nil (should not happen after New, but must be safe).
func TestAllSnapshot_MapFallback(t *testing.T) {
	p := New()
	a := newTestClient("A", 0, 0, 1000)
	b := newTestClient("B", 0, 0, 1000)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}
	// Force nil live/atc to exercise fallback branches.
	p.live.Store(nil)
	p.atcLive.Store(nil)

	snap := p.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot fallback len=%d want 2", len(snap))
	}

	var seen []string
	p.All(a, func(r *session.Session) bool {
		seen = append(seen, r.Callsign)
		return true
	})
	if len(seen) != 1 || seen[0] != "B" {
		t.Fatalf("All fallback = %v, want [B]", seen)
	}

	// Early stop on map fallback.
	count := 0
	p.All(nil, func(r *session.Session) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("early-stop All fallback count=%d", count)
	}

	// liveLen nil branch + Search with nil live (linear) must not panic.
	if p.liveLen() != 0 {
		t.Fatalf("liveLen with nil live = %d", p.liveLen())
	}
	// Restore empty snapshot so Search linear nil path is hit cleanly.
	p.Search(a, func(r *session.Session) bool {
		t.Fatal("Search should find nothing with nil live")
		return true
	})
	p.SearchATC(a, func(r *session.Session) bool {
		t.Fatal("SearchATC should find nothing with nil atcLive")
		return true
	})
}
