package postoffice

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/internal/session"
)

func newTestClient(callsign string, lat, lon, visRange float64) *session.Session {
	s := session.New(context.Background(), nil, nil, session.LoginData{Callsign: callsign})
	s.SetLatLon(lat, lon)
	s.VisRange.Store(visRange)
	return s
}

// TestRegister tests the registration of clients with unique and duplicate callsigns.
func TestRegister(t *testing.T) {
	p := New()
	client1 := newTestClient("client1", 0, 0, 100000)
	err := p.Register(client1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	got, err := p.Find("client1")
	if err != nil || got != client1 {
		t.Errorf("expected client1 in registry")
	}
	client2 := newTestClient("client1", 0, 0, 100000)
	err = p.Register(client2)
	if err != ErrCallsignInUse {
		t.Errorf("expected ErrCallsignInUse, got %v", err)
	}
	got, err = p.Find("client1")
	if err != nil || got != client1 {
		t.Errorf("expected original client1 in registry")
	}
}

// TestRelease tests the removal of a client and its effect on search results.
func TestRelease(t *testing.T) {
	p := New()
	client1 := newTestClient("client1", 0, 0, 100000)
	if err := p.Register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("client2", 0, 0, 200000)
	if err := p.Register(client2); err != nil {
		t.Fatal(err)
	}

	var found []*session.Session
	p.Search(client2, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0] != client1 {
		t.Errorf("expected to find client1, got %v", found)
	}

	p.Release(client1)
	if _, err := p.Find("client1"); err != ErrCallsignDoesNotExist {
		t.Errorf("expected client1 to be removed from map")
	}

	found = nil
	p.Search(client2, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found after release, got %v", found)
	}
}

// TestUpdatePosition tests updating a client's position and its effect on search.
func TestUpdatePosition(t *testing.T) {
	p := New()
	client1 := newTestClient("client1", 0, 0, 100000)
	if err := p.Register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("client2", 0.5, 0.5, 100000)
	if err := p.Register(client2); err != nil {
		t.Fatal(err)
	}

	var found []*session.Session
	p.Search(client1, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0] != client2 {
		t.Errorf("expected to find client2, got %v", found)
	}

	p.UpdatePosition(client2, [2]float64{100.0, 100.0}, 100000)

	found = nil
	p.Search(client1, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found after position update, got %v", found)
	}
}

// TestUpdatePosition_NoopKeepsIndexed covers the early-return path when the
// derived bounding box does not change, and asserts the client remains
// searchable (still present in the tree after the noop update).
func TestUpdatePosition_NoopKeepsIndexed(t *testing.T) {
	p := New()
	client1 := newTestClient("client1", 10, 20, 50000)
	if err := p.Register(client1); err != nil {
		t.Fatal(err)
	}
	peer := newTestClient("peer", 10, 20, 50000)
	if err := p.Register(peer); err != nil {
		t.Fatal(err)
	}

	// Same center and range → identical bbox → early return (no tree rewrite).
	p.UpdatePosition(client1, [2]float64{10, 20}, 50000)
	latLon := client1.LatLon()
	if latLon[0] != 10 || latLon[1] != 20 {
		t.Fatalf("latLon after noop update = %v", latLon)
	}
	if client1.VisRange.Load() != 50000 {
		t.Fatalf("visRange after noop update = %v", client1.VisRange.Load())
	}

	// Peer must still find client1, proving the tree entry remains valid.
	var found []*session.Session
	p.Search(peer, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0] != client1 {
		t.Fatalf("after noop update, peer search found %v, want [client1]", found)
	}
}

// TestSearch tests the search functionality with multiple clients.
func TestSearch(t *testing.T) {
	p := New()
	client1 := newTestClient("client1", 32.0, -117.0, 100000)
	if err := p.Register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("client2", 33.0, -117.0, 50000)
	if err := p.Register(client2); err != nil {
		t.Fatal(err)
	}
	client3 := newTestClient("client3", 34.0, -117.0, 50000)
	if err := p.Register(client3); err != nil {
		t.Fatal(err)
	}

	var found []*session.Session
	p.Search(client1, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0].Callsign != "client2" {
		t.Errorf("expected to find client2, got %v", found)
	}

	found = nil
	p.Search(client2, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0].Callsign != "client1" {
		t.Errorf("expected to find client1, got %v", found)
	}

	found = nil
	p.Search(client3, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found, got %v", found)
	}

	client4 := newTestClient("client4", 31.0, -117.0, 50000)
	if err := p.Register(client4); err != nil {
		t.Fatal(err)
	}

	found = nil
	p.Search(client1, func(recipient *session.Session) bool {
		found = append(found, recipient)
		return true
	})
	foundCallsigns := make([]string, len(found))
	for i, c := range found {
		foundCallsigns[i] = c.Callsign
	}
	sort.Strings(foundCallsigns)
	expected := []string{"client2", "client4"}
	sort.Strings(expected)
	if !reflect.DeepEqual(foundCallsigns, expected) {
		t.Errorf("expected %v, got %v", expected, foundCallsigns)
	}

	for _, c := range found {
		if c == client1 {
			t.Errorf("search included self")
		}
	}
}

// TestSearch_ClosestVelocityDistance ensures proto-101 pilot pairs update
// ClosestVelocityClientDistance via equirectangular ApproxDistance (hot path).
func TestSearch_ClosestVelocityDistance(t *testing.T) {
	p := New()
	client1 := newTestClient("v1", 0, 0, 500000)
	client1.ProtoRevision = 101
	client1.IsAtc = false
	if err := p.Register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("v2", 0.1, 0, 500000)
	client2.ProtoRevision = 101
	client2.IsAtc = false
	if err := p.Register(client2); err != nil {
		t.Fatal(err)
	}
	// Non-101 neighbor should not affect closest velocity distance.
	client3 := newTestClient("old", 0.05, 0, 500000)
	client3.ProtoRevision = 100
	if err := p.Register(client3); err != nil {
		t.Fatal(err)
	}

	p.Search(client1, func(recipient *session.Session) bool { return true })

	want := geo.ApproxDistance(0, 0, 0.1, 0)
	if !approxEqual(client1.ClosestVelocityClientDistance, want) {
		t.Fatalf("ClosestVelocityClientDistance = %v, want %v", client1.ClosestVelocityClientDistance, want)
	}
}

// TestSearch_EarlyStop honors false from the callback.
func TestSearch_EarlyStop(t *testing.T) {
	p := New()
	// Co-located so all are in range of each other.
	self := newTestClient("self", 0, 0, 500000)
	a := newTestClient("a", 0, 0, 500000)
	b := newTestClient("b", 0, 0, 500000)
	for _, c := range []*session.Session{self, a, b} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}

	count := 0
	p.Search(self, func(recipient *session.Session) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("early-stop Search invoked callback %d times, want 1", count)
	}
}

// TestFind covers lookup success and ErrCallsignDoesNotExist.
func TestFind(t *testing.T) {
	p := New()
	client1 := newTestClient("N123", 1, 2, 1000)
	if err := p.Register(client1); err != nil {
		t.Fatal(err)
	}

	got, err := p.Find("N123")
	if err != nil || got != client1 {
		t.Fatalf("find existing: got (%v, %v), want client1", got, err)
	}

	_, err = p.Find("MISSING")
	if err != ErrCallsignDoesNotExist {
		t.Fatalf("find missing: err = %v, want ErrCallsignDoesNotExist", err)
	}
}

// TestAll iterates all other clients and honors early stop.
func TestAll(t *testing.T) {
	p := New()
	self := newTestClient("self", 0, 0, 1000)
	a := newTestClient("a", 0, 0, 1000)
	b := newTestClient("b", 0, 0, 1000)
	for _, c := range []*session.Session{self, a, b} {
		if err := p.Register(c); err != nil {
			t.Fatal(err)
		}
	}

	var seen []string
	p.All(self, func(recipient *session.Session) bool {
		seen = append(seen, recipient.Callsign)
		return true
	})
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"a", "b"}) {
		t.Fatalf("all recipients = %v, want [a b]", seen)
	}

	// Early stop: callback returns false.
	count := 0
	p.All(self, func(recipient *session.Session) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("early-stop all invoked callback %d times, want 1", count)
	}
}

// TestSend delivers a packet to a registered client and errors for missing callsigns.
func TestSend(t *testing.T) {
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := session.New(ctx, nil, nil, session.LoginData{Callsign: "RECV"})
	client.SetLatLon(0, 0)
	client.VisRange.Store(1000)
	if err := p.Register(client); err != nil {
		t.Fatal(err)
	}

	if err := p.Send("RECV", "hello\r\n"); err != nil {
		t.Fatalf("send existing: %v", err)
	}
	pkt, ok := client.DequeueOutbound()
	if !ok {
		t.Fatal("expected packet on sendChan")
	}
	if pkt != "hello\r\n" {
		t.Fatalf("packet = %q, want hello\\r\\n", pkt)
	}

	if err := p.Send("NOPE", "x"); err != ErrCallsignDoesNotExist {
		t.Fatalf("send missing: err = %v, want ErrCallsignDoesNotExist", err)
	}
}

// TestSnapshot returns a consistent copy of registered sessions.
func TestSnapshot(t *testing.T) {
	p := New()
	if got := p.Snapshot(); len(got) != 0 {
		t.Fatalf("empty Snapshot = %v, want empty", got)
	}

	a := newTestClient("a", 0, 0, 1000)
	b := newTestClient("b", 1, 1, 1000)
	if err := p.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := p.Register(b); err != nil {
		t.Fatal(err)
	}

	snap := p.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	// Mutating the snapshot slice must not affect the registry.
	snap[0] = nil
	got, err := p.Find("a")
	if err != nil || got != a {
		t.Fatalf("registry mutated via Snapshot slice: got %v err %v", got, err)
	}

	// After release, snapshot must not include the released session.
	p.Release(a)
	snap = p.Snapshot()
	if len(snap) != 1 || snap[0] != b {
		t.Fatalf("Snapshot after release = %v, want [b]", snap)
	}
}

func approxEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) < epsilon
}

// TestPostOfficeConcurrent exercises Register/Search/UpdatePosition/Release
// across goroutines under the race detector.
func TestPostOfficeConcurrent(t *testing.T) {
	p := New()
	const n = 64
	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(fmt.Sprintf("C%d", i), float64(i%10), float64(i%20), 200000)
	}

	var wg sync.WaitGroup
	// Register all.
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

	// Concurrent search + updatePosition.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(c *session.Session) {
			defer wg.Done()
			p.Search(c, func(recipient *session.Session) bool { return true })
			ll := c.LatLon()
			p.UpdatePosition(c, [2]float64{ll[0] + 0.01, ll[1] - 0.01}, 150000)
			p.Search(c, func(recipient *session.Session) bool { return true })
		}(clients[i])
	}
	wg.Wait()

	// Concurrent release.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(c *session.Session) {
			defer wg.Done()
			p.Release(c)
		}(clients[i])
	}
	wg.Wait()

	if len(p.Snapshot()) != 0 {
		t.Fatalf("registry not empty after release-all: %d entries", len(p.Snapshot()))
	}
}

// BenchmarkRegister measures client registration cost.
func BenchmarkRegister(b *testing.B) {
	r := rand.New(rand.NewSource(42))
	clients := make([]*session.Session, b.N)
	for i := 0; i < b.N; i++ {
		clients[i] = newTestClient(
			fmt.Sprintf("C%d", i),
			-90+r.Float64()*180,
			-180+r.Float64()*360,
			10000,
		)
	}

	p := New()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Register(clients[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// benchmarkSearchWithN benchmarks search performance with n clients.
func benchmarkSearchWithN(b *testing.B, n int) {
	p := New()
	r := rand.New(rand.NewSource(42))

	clients := make([]*session.Session, n)
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(
			fmt.Sprintf("Client%d", i),
			-90+r.Float64()*180,
			-180+r.Float64()*360,
			10000,
		)
		if err := p.Register(clients[i]); err != nil {
			b.Fatal(err)
		}
	}

	callback := func(recipient *session.Session) bool {
		return true
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		searchClient := clients[i%10]
		p.Search(searchClient, callback)
	}
}

// BenchmarkSearch runs benchmarks for different client counts.
func BenchmarkSearch(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			benchmarkSearchWithN(b, n)
		})
	}
}
