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

// linearOracleSearch is a brute-force primary-only AABB scan used only in
// tests as the reference semantics for Search on primary-box fixtures.
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

// visBoxesOracleSearch is the exact Search predicate including SECPOS.
func visBoxesOracleSearch(all []*session.Session, self *session.Session, atcOnly bool) []string {
	var found []string
	for _, other := range all {
		if other == self {
			continue
		}
		if atcOnly && !other.IsAtc {
			continue
		}
		if !session.VisBoxesOverlap(self, other) {
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

// TestSearch_EquivalenceToVisBoxesOracle includes a secondary-only pair.
func TestSearch_EquivalenceToVisBoxesOracle(t *testing.T) {
	p := New()
	atc := newTestClient("LAX_CTR", 34.0, -118.0, 40*1852)
	atc.IsAtc = true
	pilot := newTestClient("N100", 36.0, -118.0, 50*1852)
	near := newTestClient("NEAR", 34.01, -118.01, 50*1852)
	for _, c := range []*session.Session{atc, pilot, near} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if !atc.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("SetSecondaryVisCenter")
	}
	all := []*session.Session{atc, pilot, near}
	for _, self := range all {
		got := searchCallsigns(p, self)
		want := visBoxesOracleSearch(all, self, false)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("Search(%s)=%v oracle=%v", self.Callsign, got, want)
		}
		gotA := searchATCCallsigns(p, self)
		wantA := visBoxesOracleSearch(all, self, true)
		if fmt.Sprint(gotA) != fmt.Sprint(wantA) {
			t.Fatalf("SearchATC(%s)=%v oracle=%v", self.Callsign, gotA, wantA)
		}
	}
	// Primary-only oracle must miss the secondary-only pair (do not add SECPOS to it).
	if contains(linearOracleSearch(all, pilot, false), "LAX_CTR") {
		t.Fatal("linearOracleSearch is primary-only; must miss CTR↔pilot")
	}
	if !contains(visBoxesOracleSearch(all, pilot, false), "LAX_CTR") {
		t.Fatal("visBoxesOracleSearch must include SECPOS pair")
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
	peer := newTestClient("PEER", 1, 1, 100000)
	if err := p.Register(peer); err != nil {
		t.Fatal(err)
	}
	gotCS := searchCallsigns(p, peer)
	if !contains(gotCS, "SAME") {
		t.Fatalf("identity-guard Search after ghost release = %v, want SAME", gotCS)
	}
}

// TestSearch_SetLatLonWithoutUpdatePosition mirrors sweatbox syncSessionWire.
func TestSearch_SetLatLonWithoutUpdatePosition(t *testing.T) {
	p := New()
	a := newTestClient("A", 0, 0, 50*1852)
	b := newTestClient("B", 0, 0, 50*1852)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}
	b.SetLatLon(50, 10)
	if contains(searchCallsigns(p, a), "B") {
		t.Fatal("SetLatLon without UpdatePosition must refresh sidecar (move out)")
	}
	b.SetLatLon(0, 0)
	if !contains(searchCallsigns(p, a), "B") {
		t.Fatal("SetLatLon without UpdatePosition must refresh sidecar (move back)")
	}
}

// TestSearch_SetSecondaryWithoutUpdatePosition is the hook path for ' packets.
func TestSearch_SetSecondaryWithoutUpdatePosition(t *testing.T) {
	p := New()
	atc := newTestClient("CTR", 34.0, -118.0, 40*1852)
	atc.IsAtc = true
	pilot := newTestClient("N1", 36.0, -118.0, 50*1852)
	if err := p.Register(atc); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}
	if contains(searchCallsigns(p, pilot), "CTR") {
		t.Fatal("primary-only should miss")
	}
	if !atc.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("SetSecondaryVisCenter")
	}
	if !contains(searchCallsigns(p, pilot), "CTR") {
		t.Fatal("hook after SetSecondaryVisCenter must Search-hit")
	}
}

// TestSearch_CompactConcurrentSECPOS forces ATC compact while a surviving ATC
// mutates SECPOS. After wait, SearchATC must hit *without* re-setting SECPOS
// (would pass a missing post-Store re-fill if we hooked first).
func TestSearch_CompactConcurrentSECPOS(t *testing.T) {
	const n = 80
	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		c := newTestClient(fmt.Sprintf("ATC%d", i), 34.0, -118.0, 40*1852)
		c.IsAtc = true
		clients[i] = c
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	survivor := clients[n-1]
	if !survivor.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("pre-compact SECPOS")
	}
	pilot := newTestClient("N100", 36.0, -118.0, 50*1852)
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				survivor.SetSecondaryVisCenter(0, 36.0, -118.0)
			}
		}
	}()

	for i := 0; i < n*3/4; i++ {
		p.Release(clients[i])
	}
	close(stop)
	wg.Wait()

	if len(p.freeAtc) != 0 {
		t.Fatalf("ATC compact did not run: freeAtc=%d", len(p.freeAtc))
	}
	// Do not re-set SECPOS first — sidecar must already be packed+refilled.
	if !contains(searchATCCallsigns(p, pilot), survivor.Callsign) {
		t.Fatalf("SearchATC after ATC compact (no extra mutator) = %v, want %s", searchATCCallsigns(p, pilot), survivor.Callsign)
	}

	survivor.ClearSecondaryVisCenters()
	if contains(searchATCCallsigns(p, pilot), survivor.Callsign) {
		t.Fatal("clear after compact should drop the pair")
	}
	if !survivor.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("re-set")
	}
	if !contains(searchATCCallsigns(p, pilot), survivor.Callsign) {
		t.Fatal("post-compact hook without UpdatePosition must still SearchATC-hit packed row")
	}
}

// TestSearch_CompactLiveConcurrentSECPOS grows the live slab to cap>=256 then
// compactLiveLocked; Search must hit without an extra mutator.
func TestSearch_CompactLiveConcurrentSECPOS(t *testing.T) {
	const n = 256
	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		c := newTestClient(fmt.Sprintf("L%d", i), 34.0, -118.0, 40*1852)
		if i == n-1 {
			c.IsAtc = true
		}
		clients[i] = c
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	if capN := len(p.live.Load().slots); capN < 256 {
		t.Fatalf("live slab cap=%d, want >=256", capN)
	}
	survivor := clients[n-1]
	if !survivor.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("pre-compact SECPOS")
	}
	// Keep a pre-registered searcher (do not Register after n=256 or cap grows to 512
	// and free>=remaining may not hold). Place them under the secondary.
	pilot := clients[n-2]
	pilot.SetGeo(36.0, -118.0, 50*1852)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				survivor.SetSecondaryVisCenter(0, 36.0, -118.0)
			}
		}
	}()

	// Release half so free >= remaining (128/128) and cap >= 256.
	for i := 0; i < n/2; i++ {
		p.Release(clients[i])
	}
	close(stop)
	wg.Wait()

	if len(p.freeLive) != 0 {
		t.Fatalf("live compact did not run: freeLive=%d", len(p.freeLive))
	}
	if !contains(searchCallsigns(p, pilot), survivor.Callsign) {
		t.Fatalf("Search after live compact (no extra mutator) = %v", searchCallsigns(p, pilot))
	}
	survivor.ClearSecondaryVisCenters()
	if contains(searchCallsigns(p, pilot), survivor.Callsign) {
		t.Fatal("clear after live compact should miss")
	}
	if !survivor.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("re-set")
	}
	if !contains(searchCallsigns(p, pilot), survivor.Callsign) {
		t.Fatal("post-live-compact hook without UpdatePosition must Search-hit")
	}
}

// TestLiveSlab_GrowConcurrentHook registers past the 128-slot grow while an
// early occupant mutates geo; after grow, Search must hit without an extra mutator.
func TestLiveSlab_GrowConcurrentHook(t *testing.T) {
	const n = 200
	p := New()
	early := newTestClient("EARLY", 34.0, -118.0, 40*1852)
	early.IsAtc = true
	if err := p.Register(early); err != nil {
		t.Fatal(err)
	}
	if !early.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("SECPOS")
	}
	pilot := newTestClient("N100", 36.0, -118.0, 50*1852)
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				early.SetSecondaryVisCenter(0, 36.0, -118.0)
				early.SetLatLon(34.0, -118.0)
			}
		}
	}()

	for i := 0; i < n; i++ {
		c := newTestClient(fmt.Sprintf("G%d", i), 0, 0, 1000)
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	if len(p.live.Load().slots) < 256 {
		t.Fatalf("expected grow past 128, cap=%d", len(p.live.Load().slots))
	}
	if !contains(searchCallsigns(p, pilot), "EARLY") {
		t.Fatal("after grow, Search without extra mutator must hit EARLY via SECPOS")
	}
	early.ClearSecondaryVisCenters()
	if contains(searchCallsigns(p, pilot), "EARLY") {
		t.Fatal("clear after grow should miss")
	}
	early.SetLatLon(36.0, -118.0) // no UpdatePosition
	if !contains(searchCallsigns(p, pilot), "EARLY") {
		t.Fatal("SetLatLon after grow without UpdatePosition must refresh sidecar")
	}
}

// TestLiveSlab_GrowCopiesSidecar registers past initial cap so grow copies SoA+live.
func TestLiveSlab_GrowCopiesSidecar(t *testing.T) {
	const n = 200
	p := New()
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(fmt.Sprintf("G%d", i), 34.0, -118.0, 50*1852)
		if err := p.Register(clients[i]); err != nil {
			t.Fatal(err)
		}
	}
	got := searchCallsigns(p, clients[0])
	if len(got) != n-1 {
		t.Fatalf("after grow Search found %d, want %d", len(got), n-1)
	}
}

// TestLiveSlab_GrowRefillAfterSECPOS injects SetSecondary between SoA copy and
// Store. Without post-publish re-fill the new slab keeps a primary-only row.
func TestLiveSlab_GrowRefillAfterSECPOS(t *testing.T) {
	p := New()
	atc := newTestClient("CTR", 34.0, -118.0, 40*1852)
	atc.IsAtc = true
	if err := p.Register(atc); err != nil {
		t.Fatal(err)
	}
	pilot := newTestClient("N1", 36.0, -118.0, 50*1852)
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}
	if contains(searchCallsigns(p, pilot), "CTR") {
		t.Fatal("primary-only must miss")
	}

	cap0 := len(p.live.Load().slots)
	for i := p.liveLen(); i < cap0; i++ {
		c := newTestClient(fmt.Sprintf("F%d", i), 0, 0, 1000)
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}

	var injected atomic.Bool
	growCopyHook = func() {
		if injected.Load() {
			return
		}
		if !atc.SetSecondaryVisCenter(0, 36.0, -118.0) {
			t.Error("SetSecondaryVisCenter during grow")
			return
		}
		injected.Store(true)
	}
	defer func() { growCopyHook = nil }()

	if err := p.Register(newTestClient("GROW", 0, 0, 1000)); err != nil {
		t.Fatal(err)
	}
	growCopyHook = nil

	if !injected.Load() {
		t.Fatal("grow copy hook did not run")
	}
	if !session.VisBoxesOverlap(atc, pilot) {
		t.Fatal("exact must hit after injected SECPOS")
	}
	if !contains(searchCallsigns(p, pilot), "CTR") {
		t.Fatal("grow re-fill must publish SECPOS written between copy and Store")
	}
}

// TestATCSlab_GrowRefillAfterSECPOS is the ATC-slab counterpart.
func TestATCSlab_GrowRefillAfterSECPOS(t *testing.T) {
	p := New()
	atc := newTestClient("CTR", 34.0, -118.0, 40*1852)
	atc.IsAtc = true
	if err := p.Register(atc); err != nil {
		t.Fatal(err)
	}
	pilot := newTestClient("N1", 36.0, -118.0, 50*1852)
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}

	cap0 := len(p.atcLive.Load().slots)
	for i := int(p.atcCount.Load()); i < cap0; i++ {
		c := newTestClient(fmt.Sprintf("A%d", i), 0, 0, 1000)
		c.IsAtc = true
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}

	var injected atomic.Bool
	growCopyHook = func() {
		if injected.Load() {
			return
		}
		if !atc.SetSecondaryVisCenter(0, 36.0, -118.0) {
			t.Error("SetSecondaryVisCenter during ATC grow")
			return
		}
		injected.Store(true)
	}
	defer func() { growCopyHook = nil }()

	extra := newTestClient("AGROW", 0, 0, 1000)
	extra.IsAtc = true
	if err := p.Register(extra); err != nil {
		t.Fatal(err)
	}
	growCopyHook = nil

	if !injected.Load() {
		t.Fatal("ATC grow copy hook did not run")
	}
	if !contains(searchATCCallsigns(p, pilot), "CTR") {
		t.Fatal("ATC grow re-fill must publish SECPOS written between copy and Store")
	}
}

func TestIdxPool_OversizedDropped(t *testing.T) {
	big := make([]int32, 0, idxPoolMaxCap+1)
	ptr := &big
	releaseIdx(ptr)
	got := acquireIdx()
	if cap(*got) > idxPoolMaxCap {
		t.Fatalf("acquireIdx returned oversized buffer cap=%d", cap(*got))
	}
	releaseIdx(got)
	releaseIdx(nil)
}

func TestStoreUnionSidecar_IdentityNoWrite(t *testing.T) {
	p := New()
	s := newTestClient("S", 0, 0, 1000)
	// Unregistered: indices −1, slot not published.
	p.storeUnionSidecar(s)
	p.writeOne(&p.live, 0, s, 0, 0, 1, 1)
	if slab := p.live.Load(); slab.live[0] != 0 {
		t.Fatal("must not write sidecar when slot != s")
	}
}

// TestStoreUnionSidecar_IdentityAfterReregister: old session must not smash
// the reused slot’s union. A write that ignores slots[idx]!=s would FN Search.
func TestStoreUnionSidecar_IdentityAfterReregister(t *testing.T) {
	p := New()
	old := newTestClient("SAME", 0, 0, 1000)
	if err := p.Register(old); err != nil {
		t.Fatal(err)
	}
	oldIdx := old.SlabLive()
	p.Release(old)

	neu := newTestClient("SAME", 50.0, 10.0, 80*1852)
	if err := p.Register(neu); err != nil {
		t.Fatal(err)
	}
	if neu.SlabLive() != oldIdx {
		t.Fatalf("expected free-list reuse: neu=%d old=%d", neu.SlabLive(), oldIdx)
	}
	peer := newTestClient("PEER", 50.0, 10.0, 80*1852)
	if err := p.Register(peer); err != nil {
		t.Fatal(err)
	}
	if !contains(searchCallsigns(p, peer), "SAME") {
		t.Fatal("peer should find neu before smash attempt")
	}

	// Stale writes as if old still owned the packed row (tiny equator box).
	p.writeOne(&p.live, oldIdx, old, -1, -1, -0.9, -0.9)
	old.SetSlabLive(oldIdx)
	p.storeUnionSidecar(old)
	old.SetGeo(0, 0, 1) // hook cleared on Release

	if !contains(searchCallsigns(p, peer), "SAME") {
		t.Fatal("identity check must ignore old writeOne/storeUnionSidecar/SetGeo on reused slot")
	}
}

// TestSearch_SetVisRangeWithoutUpdatePosition: expand ATC range so exact
// overlap becomes true. Search from the still-small pilot: a stale small
// ATC sidecar would coarse-miss. Do not SetGeo the pilot.
func TestSearch_SetVisRangeWithoutUpdatePosition(t *testing.T) {
	p := New()
	// 0.3° apart: 100 m boxes cannot meet; 40 NM ATC box reaches the pilot.
	atc := newTestClient("CTR", 34.0, -118.0, 100)
	atc.IsAtc = true
	pilot := newTestClient("N1", 34.3, -118.0, 100)
	if err := p.Register(atc); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(pilot); err != nil {
		t.Fatal(err)
	}
	if contains(searchCallsigns(p, pilot), "CTR") {
		t.Fatal("small ranges must not overlap")
	}
	if session.VisBoxesOverlap(atc, pilot) {
		t.Fatal("exact predicate must miss at 100 m")
	}

	atc.SetVisRange(40 * 1852)
	if !session.VisBoxesOverlap(atc, pilot) {
		t.Fatal("exact predicate must hit after ATC range expand")
	}
	if !contains(searchCallsigns(p, pilot), "CTR") {
		t.Fatal("SetVisRange without UpdatePosition must refresh sidecar (Search from small-box pilot)")
	}
}
