package fsd

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/renorris/openfsd/internal/geo"
)

func newTestClient(callsign string, lat, lon, visRange float64) *Client {
	c := &Client{loginData: loginData{callsign: callsign}}
	c.setLatLon(lat, lon)
	c.visRange.Store(visRange)
	return c
}

// TestRegister tests the registration of clients with unique and duplicate callsigns.
func TestRegister(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("client1", 0, 0, 100000)
	err := p.register(client1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if p.clientMap["client1"] != client1 {
		t.Errorf("expected client1 in map")
	}
	client2 := newTestClient("client1", 0, 0, 100000)
	err = p.register(client2)
	if err != ErrCallsignInUse {
		t.Errorf("expected ErrCallsignInUse, got %v", err)
	}
	if p.clientMap["client1"] != client1 {
		t.Errorf("expected original client1 in map")
	}
}

// TestRelease tests the removal of a client and its effect on search results.
func TestRelease(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("client1", 0, 0, 100000)
	if err := p.register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("client2", 0, 0, 200000)
	if err := p.register(client2); err != nil {
		t.Fatal(err)
	}

	var found []*Client
	p.search(client2, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0] != client1 {
		t.Errorf("expected to find client1, got %v", found)
	}

	p.release(client1)
	if _, exists := p.clientMap["client1"]; exists {
		t.Errorf("expected client1 to be removed from map")
	}

	found = nil
	p.search(client2, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found after release, got %v", found)
	}
}

// TestUpdatePosition tests updating a client's position and its effect on search.
func TestUpdatePosition(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("client1", 0, 0, 100000)
	if err := p.register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("client2", 0.5, 0.5, 100000)
	if err := p.register(client2); err != nil {
		t.Fatal(err)
	}

	var found []*Client
	p.search(client1, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0] != client2 {
		t.Errorf("expected to find client2, got %v", found)
	}

	p.updatePosition(client2, [2]float64{100.0, 100.0}, 100000)

	found = nil
	p.search(client1, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found after position update, got %v", found)
	}
}

// TestUpdatePosition_NoopWhenUnchanged covers the early-return path when the
// derived bounding box does not change.
func TestUpdatePosition_NoopWhenUnchanged(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("client1", 10, 20, 50000)
	if err := p.register(client1); err != nil {
		t.Fatal(err)
	}
	// Same center and range → identical bbox → tree not rewritten.
	p.updatePosition(client1, [2]float64{10, 20}, 50000)
	latLon := client1.latLon()
	if latLon[0] != 10 || latLon[1] != 20 {
		t.Fatalf("latLon after noop update = %v", latLon)
	}
	if client1.visRange.Load() != 50000 {
		t.Fatalf("visRange after noop update = %v", client1.visRange.Load())
	}
}

// TestSearch tests the search functionality with multiple clients.
func TestSearch(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("client1", 32.0, -117.0, 100000)
	if err := p.register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("client2", 33.0, -117.0, 50000)
	if err := p.register(client2); err != nil {
		t.Fatal(err)
	}
	client3 := newTestClient("client3", 34.0, -117.0, 50000)
	if err := p.register(client3); err != nil {
		t.Fatal(err)
	}

	var found []*Client
	p.search(client1, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0].callsign != "client2" {
		t.Errorf("expected to find client2, got %v", found)
	}

	found = nil
	p.search(client2, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 1 || found[0].callsign != "client1" {
		t.Errorf("expected to find client1, got %v", found)
	}

	found = nil
	p.search(client3, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found, got %v", found)
	}

	client4 := newTestClient("client4", 31.0, -117.0, 50000)
	if err := p.register(client4); err != nil {
		t.Fatal(err)
	}

	found = nil
	p.search(client1, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	foundCallsigns := make([]string, len(found))
	for i, c := range found {
		foundCallsigns[i] = c.callsign
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
// closestVelocityClientDistance via geo.Distance.
func TestSearch_ClosestVelocityDistance(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("v1", 0, 0, 500000)
	client1.protoRevision = 101
	client1.isAtc = false
	if err := p.register(client1); err != nil {
		t.Fatal(err)
	}
	client2 := newTestClient("v2", 0.1, 0, 500000)
	client2.protoRevision = 101
	client2.isAtc = false
	if err := p.register(client2); err != nil {
		t.Fatal(err)
	}
	// Non-101 neighbor should not affect closest velocity distance.
	client3 := newTestClient("old", 0.05, 0, 500000)
	client3.protoRevision = 100
	if err := p.register(client3); err != nil {
		t.Fatal(err)
	}

	p.search(client1, func(recipient *Client) bool { return true })

	want := geo.Distance(0, 0, 0.1, 0)
	if !approxEqual(client1.closestVelocityClientDistance, want) {
		t.Fatalf("closestVelocityClientDistance = %v, want %v", client1.closestVelocityClientDistance, want)
	}
}

// TestFind covers lookup success and ErrCallsignDoesNotExist.
func TestFind(t *testing.T) {
	p := newPostOffice()
	client1 := newTestClient("N123", 1, 2, 1000)
	if err := p.register(client1); err != nil {
		t.Fatal(err)
	}

	got, err := p.find("N123")
	if err != nil || got != client1 {
		t.Fatalf("find existing: got (%v, %v), want client1", got, err)
	}

	_, err = p.find("MISSING")
	if err != ErrCallsignDoesNotExist {
		t.Fatalf("find missing: err = %v, want ErrCallsignDoesNotExist", err)
	}
}

// TestAll iterates all other clients and honors early stop.
func TestAll(t *testing.T) {
	p := newPostOffice()
	self := newTestClient("self", 0, 0, 1000)
	a := newTestClient("a", 0, 0, 1000)
	b := newTestClient("b", 0, 0, 1000)
	for _, c := range []*Client{self, a, b} {
		if err := p.register(c); err != nil {
			t.Fatal(err)
		}
	}

	var seen []string
	p.all(self, func(recipient *Client) bool {
		seen = append(seen, recipient.callsign)
		return true
	})
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"a", "b"}) {
		t.Fatalf("all recipients = %v, want [a b]", seen)
	}

	// Early stop: callback returns false.
	count := 0
	p.all(self, func(recipient *Client) bool {
		count++
		return false
	})
	if count != 1 {
		t.Fatalf("early-stop all invoked callback %d times, want 1", count)
	}
}

// TestSend delivers a packet to a registered client and errors for missing callsigns.
func TestSend(t *testing.T) {
	p := newPostOffice()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newTestClient("RECV", 0, 0, 1000)
	client.ctx = ctx
	client.sendChan = make(chan string, 1)
	if err := p.register(client); err != nil {
		t.Fatal(err)
	}

	if err := p.send("RECV", "hello\r\n"); err != nil {
		t.Fatalf("send existing: %v", err)
	}
	select {
	case pkt := <-client.sendChan:
		if pkt != "hello\r\n" {
			t.Fatalf("packet = %q, want hello\\r\\n", pkt)
		}
	default:
		t.Fatal("expected packet on sendChan")
	}

	if err := p.send("NOPE", "x"); err != ErrCallsignDoesNotExist {
		t.Fatalf("send missing: err = %v, want ErrCallsignDoesNotExist", err)
	}
}

func approxEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) < epsilon
}

// BenchmarkRegister measures client registration cost.
func BenchmarkRegister(b *testing.B) {
	r := rand.New(rand.NewSource(42))
	clients := make([]*Client, b.N)
	for i := 0; i < b.N; i++ {
		clients[i] = newTestClient(
			fmt.Sprintf("C%d", i),
			-90+r.Float64()*180,
			-180+r.Float64()*360,
			10000,
		)
	}

	p := newPostOffice()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.register(clients[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// benchmarkSearchWithN benchmarks search performance with n clients.
func benchmarkSearchWithN(b *testing.B, n int) {
	p := newPostOffice()
	r := rand.New(rand.NewSource(42))

	clients := make([]*Client, n)
	for i := 0; i < n; i++ {
		clients[i] = newTestClient(
			fmt.Sprintf("Client%d", i),
			-90+r.Float64()*180,
			-180+r.Float64()*360,
			10000,
		)
		if err := p.register(clients[i]); err != nil {
			b.Fatal(err)
		}
	}

	callback := func(recipient *Client) bool {
		return true
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		searchClient := clients[i%10]
		p.search(searchClient, callback)
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
