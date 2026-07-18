package postoffice

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/internal/session"
)

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

// linearOracleSearch is a brute-force AABB scan used only in tests as the
// reference semantics for the spatial hash.
func linearOracleSearch(all []*session.Session, self *session.Session, atcOnly bool) []string {
	sMin, sMax := geo.BoundingBox(self.LatLon(), self.VisRange.Load())
	var found []string
	for _, other := range all {
		if other == self {
			continue
		}
		if atcOnly && !other.IsAtc {
			continue
		}
		oMin, oMax := geo.BoundingBox(other.LatLon(), other.VisRange.Load())
		if !geo.AABBOverlap(sMin, sMax, oMin, oMax) {
			continue
		}
		found = append(found, other.Callsign)
	}
	sort.Strings(found)
	return found
}

// TestSearchATC_ATCOnly yields only ATC peers.
func TestSearchATC_ATCOnly(t *testing.T) {
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

// TestSearch_EquivalenceToLinearOracle compares Search to brute-force AABB.
func TestSearch_EquivalenceToLinearOracle(t *testing.T) {
	type place struct {
		cs       string
		lat, lon float64
		vr       float64
		atc      bool
	}
	places := []place{
		{"P0", 34.00, -118.00, 80 * 1852, false},
		{"P1", 34.10, -118.00, 80 * 1852, false},
		{"P2", 34.50, -118.00, 40 * 1852, true},
		{"P3", 40.00, -100.00, 30 * 1852, true},
		{"P4", 34.05, -118.05, 100 * 1852, false},
	}

	p := New()
	all := make([]*session.Session, 0, len(places))
	for _, pl := range places {
		c := newTestClient(pl.cs, pl.lat, pl.lon, pl.vr)
		c.IsAtc = pl.atc
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
		all = append(all, c)
	}

	for _, pl := range places {
		self, err := p.Find(pl.cs)
		if err != nil {
			t.Fatal(err)
		}
		got := searchCallsigns(p, self)
		want := linearOracleSearch(all, self, false)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("Search(%s) grid=%v oracle=%v", pl.cs, got, want)
		}
		gotA := searchATCCallsigns(p, self)
		wantA := linearOracleSearch(all, self, true)
		if fmt.Sprint(gotA) != fmt.Sprint(wantA) {
			t.Fatalf("SearchATC(%s) grid=%v oracle=%v", pl.cs, gotA, wantA)
		}
	}
}

// TestUpdatePosition_MovesOutOfRange rewrites the index so distant peers fall out of range.
func TestUpdatePosition_MovesOutOfRange(t *testing.T) {
	p := New()
	a := newTestClient("A", 34.0, -118.0, 50*1852)
	b := newTestClient("B", 34.0, -118.0, 50*1852)
	c := newTestClient("C", 34.0, -118.0, 50*1852)
	for _, s := range []*session.Session{a, b, c} {
		if err := p.Register(s); err != nil {
			t.Fatal(err)
		}
	}

	got := searchCallsigns(p, a)
	if len(got) != 2 {
		t.Fatalf("before move Search=%v", got)
	}

	p.UpdatePosition(b, [2]float64{50.0, 10.0}, 50*1852)
	got = searchCallsigns(p, a)
	if fmt.Sprint(got) != fmt.Sprint([]string{"C"}) {
		t.Fatalf("after move Search=%v, want [C]", got)
	}

	// No-op update keeps B searchable from its new home.
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

// TestConcurrentRegisterRelease hammers Register/Release and asserts consistency.
func TestConcurrentRegisterRelease(t *testing.T) {
	const n = 32
	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
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
	snap := p.Snapshot()
	if len(snap) != n {
		t.Fatalf("Snapshot len=%d want %d", len(snap), n)
	}

	for _, c := range clients {
		got, err := p.Find(c.Callsign)
		if err != nil || got != c {
			t.Fatalf("Find(%s)=(%v,%v)", c.Callsign, got, err)
		}
	}

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

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		p.Search(self, func(r *session.Session) bool {
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
// buffer cannot stall position fan-out to other recipients.
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

// TestLiveSlab_ReleaseInvisible ensures released sessions disappear from All/Search
// without requiring COW freeze of prior slab pointers.
func TestLiveSlab_ReleaseInvisible(t *testing.T) {
	p := New()
	a := newTestClient("A", 0, 0, 1000)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	b := newTestClient("B", 0, 0, 1000)
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}
	if p.liveLen() != 2 {
		t.Fatalf("liveLen=%d", p.liveLen())
	}

	p.Release(a)
	if p.liveLen() != 1 {
		t.Fatalf("liveLen after release=%d", p.liveLen())
	}
	if _, err := p.Find("A"); err != ErrCallsignDoesNotExist {
		t.Fatalf("Find A after release: %v", err)
	}

	var seen []string
	p.All(nil, func(r *session.Session) bool {
		seen = append(seen, r.Callsign)
		return true
	})
	if len(seen) != 1 || seen[0] != "B" {
		t.Fatalf("All after release A = %v, want [B]", seen)
	}
}

// TestLiveSlab_FreeListReuse registers, releases all, re-registers without capacity explosion.
func TestLiveSlab_FreeListReuse(t *testing.T) {
	const n = 200
	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(fmt.Sprintf("F%d", i), 0, 0, 1000)
		if err := p.Register(clients[i]); err != nil {
			t.Fatal(err)
		}
	}
	slab1 := p.live.Load()
	cap1 := len(slab1.slots)

	for _, c := range clients {
		p.Release(c)
	}
	if p.liveLen() != 0 {
		t.Fatalf("liveLen=%d", p.liveLen())
	}

	// Re-register same population (new sessions, same N).
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(fmt.Sprintf("G%d", i), 0, 0, 1000)
		if err := p.Register(clients[i]); err != nil {
			t.Fatal(err)
		}
	}
	slab2 := p.live.Load()
	cap2 := len(slab2.slots)
	if cap2 > cap1*2 {
		t.Fatalf("slab cap exploded: before=%d after=%d", cap1, cap2)
	}
	if p.liveLen() != n {
		t.Fatalf("liveLen=%d want %d", p.liveLen(), n)
	}
}

// TestATCSnapshot_TracksATCOnly keeps ATC slab consistent across Register/Release.
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
	if p.atcCount.Load() != 1 {
		t.Fatalf("atcCount=%d want 1", p.atcCount.Load())
	}

	p.Release(pilot)
	if p.atcCount.Load() != 1 {
		t.Fatalf("atcCount after pilot release=%d", p.atcCount.Load())
	}

	p.Release(atc)
	if p.atcCount.Load() != 0 {
		t.Fatalf("atcCount after atc release=%d", p.atcCount.Load())
	}
}

// TestReleaseUnregistered is a no-crash soft path (disconnect races).
func TestReleaseUnregistered(t *testing.T) {
	p := New()
	ghost := newTestClient("GHOST", 0, 0, 1000)
	p.Release(ghost)
	if p.liveLen() != 0 {
		t.Fatalf("liveLen=%d", p.liveLen())
	}
}

// TestFoundPool_OversizedDropped ensures huge recipient slices are not pooled.
func TestFoundPool_OversizedDropped(t *testing.T) {
	big := make([]*session.Session, 0, foundPoolMaxCap+1)
	ptr := &big
	releaseFound(ptr)

	got := acquireFound()
	if cap(*got) > foundPoolMaxCap {
		t.Fatalf("acquireFound returned oversized buffer cap=%d", cap(*got))
	}
	releaseFound(got)
}

// TestSearch_ConcurrentWithPositionStorm mimics dense event load.
func TestSearch_ConcurrentWithPositionStorm(t *testing.T) {
	const n = 48
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

// TestAll_MapFallback covers All when the live slab pointer is nil.
func TestAll_MapFallback(t *testing.T) {
	p := New()
	a := newTestClient("A", 0, 0, 1000)
	b := newTestClient("B", 0, 0, 1000)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}
	p.live.Store(nil)

	// Snapshot still works via map.
	snap := p.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len=%d want 2", len(snap))
	}

	var seen []string
	p.All(a, func(r *session.Session) bool {
		seen = append(seen, r.Callsign)
		return true
	})
	if len(seen) != 1 || seen[0] != "B" {
		t.Fatalf("All fallback = %v, want [B]", seen)
	}

	count := 0
	p.All(nil, func(r *session.Session) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("early-stop All fallback count=%d", count)
	}

	// Search with nil live slab finds nothing (defensive; New always sets live).
	got := searchCallsigns(p, a)
	if len(got) != 0 {
		t.Fatalf("Search with nil live slab = %v, want []", got)
	}
}

// TestATCSlab_GrowAndCompact exercises ATC free-list reuse, slab grow, and compact.
func TestATCSlab_GrowAndCompact(t *testing.T) {
	const n = 80
	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		c := newTestClient(fmt.Sprintf("ATC%d", i), 34+float64(i)*0.01, -118, 50*1852)
		c.IsAtc = true
		clients[i] = c
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if p.atcCount.Load() != int32(n) {
		t.Fatalf("atcCount=%d want %d", p.atcCount.Load(), n)
	}
	// Release more than half so free >= remaining and cap is large enough to compact.
	for i := 0; i < n*3/4; i++ {
		p.Release(clients[i])
	}
	if p.atcCount.Load() != int32(n-n*3/4) {
		t.Fatalf("atcCount after release=%d", p.atcCount.Load())
	}
	// Remaining ATC still searchable.
	src := clients[n-1]
	got := searchATCCallsigns(p, src)
	if len(got) != int(p.atcCount.Load())-1 {
		t.Fatalf("SearchATC len=%d atcCount=%d got=%v", len(got), p.atcCount.Load(), got)
	}
	// Re-register released callsigns (free-list path).
	for i := 0; i < n/4; i++ {
		c := newTestClient(fmt.Sprintf("NEW%d", i), 0, 0, 1000)
		c.IsAtc = true
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if p.liveLen() < n/4 {
		t.Fatalf("liveLen=%d", p.liveLen())
	}
}

// TestRegisterRelease_IdentityGuard: Release of an old session after callsign
// re-register must not drop the new occupant.
func TestRegisterRelease_IdentityGuard(t *testing.T) {
	p := New()
	old := newTestClient("SAME", 0, 0, 1000)
	if err := p.Register(old); err != nil {
		t.Fatal(err)
	}
	p.Release(old)

	neu := newTestClient("SAME", 1, 1, 1000)
	if err := p.Register(neu); err != nil {
		t.Fatal(err)
	}
	// Ghost release of old must not remove neu.
	p.Release(old)
	got, err := p.Find("SAME")
	if err != nil || got != neu {
		t.Fatalf("Find after ghost release: %v %v", got, err)
	}
	if p.liveLen() != 1 {
		t.Fatalf("liveLen=%d", p.liveLen())
	}
}
