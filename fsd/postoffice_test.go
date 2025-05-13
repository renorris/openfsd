package fsd

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func TestRegister(t *testing.T) {
	p := newPostOffice()
	client1 := &Client{loginData: loginData{callsign: "client1"}, latLon: [2]float64{0, 0}, visRange: 100000}
	err := p.register(client1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if p.clientMap["client1"] != client1 {
		t.Errorf("expected client1 in map")
	}
	client2 := &Client{loginData: loginData{callsign: "client1"}, latLon: [2]float64{0, 0}, visRange: 100000}
	err = p.register(client2)
	if err != ErrCallsignInUse {
		t.Errorf("expected ErrCallsignInUse, got %v", err)
	}
	if p.clientMap["client1"] != client1 {
		t.Errorf("expected original client1 in map")
	}
}

func TestRelease(t *testing.T) {
	p := newPostOffice()
	client1 := &Client{
		loginData: loginData{callsign: "client1"},
		latLon:    [2]float64{0, 0},
		visRange:  100000,
	}
	err := p.register(client1)
	if err != nil {
		t.Fatal(err)
	}
	client2 := &Client{
		loginData: loginData{callsign: "client2"},
		latLon:    [2]float64{0, 0},
		visRange:  200000,
	}
	err = p.register(client2)
	if err != nil {
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
	_, exists := p.clientMap["client1"]
	if exists {
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

func TestUpdatePosition(t *testing.T) {
	p := newPostOffice()
	client1 := &Client{
		loginData: loginData{callsign: "client1"},
		latLon:    [2]float64{0, 0},
		visRange:  100000,
	}
	err := p.register(client1)
	if err != nil {
		t.Fatal(err)
	}
	client2 := &Client{
		loginData: loginData{callsign: "client2"},
		latLon:    [2]float64{0.5, 0.5},
		visRange:  100000,
	}
	err = p.register(client2)
	if err != nil {
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

	newLatLon := [2]float64{100, 100}
	newVisRange := 100000.0
	p.updatePosition(client2, newLatLon, newVisRange)

	found = nil
	p.search(client1, func(recipient *Client) bool {
		found = append(found, recipient)
		return true
	})
	if len(found) != 0 {
		t.Errorf("expected no clients found after position update, got %v", found)
	}
}

func TestSearch(t *testing.T) {
	p := newPostOffice()
	client1 := &Client{
		loginData: loginData{callsign: "client1"},
		latLon:    [2]float64{32.0, -117.0},
		visRange:  100000,
	}
	err := p.register(client1)
	if err != nil {
		t.Fatal(err)
	}
	client2 := &Client{
		loginData: loginData{callsign: "client2"},
		latLon:    [2]float64{33.0, -117.0},
		visRange:  50000,
	}
	err = p.register(client2)
	if err != nil {
		t.Fatal(err)
	}
	client3 := &Client{
		loginData: loginData{callsign: "client3"},
		latLon:    [2]float64{34.0, -117.0},
		visRange:  50000,
	}
	err = p.register(client3)
	if err != nil {
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

	client4 := &Client{
		loginData: loginData{callsign: "client4"},
		latLon:    [2]float64{31.0, -117.0},
		visRange:  50000,
	}
	err = p.register(client4)
	if err != nil {
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

func TestCalculateBoundingBox(t *testing.T) {
	const earthRadius = 6371000.0
	tests := []struct {
		name        string
		center      [2]float64
		radius      float64
		expectedMin [2]float64
		expectedMax [2]float64
	}{
		{
			name:        "equator",
			center:      [2]float64{0, 0},
			radius:      100000,
			expectedMin: [2]float64{-0.8993216059187304, -0.8993216059187304},
			expectedMax: [2]float64{0.8993216059187304, 0.8993216059187304},
		},
		{
			name:        "45 degrees latitude",
			center:      [2]float64{45, 0},
			radius:      100000,
			expectedMin: [2]float64{44.10067839408127, -1.2718328120254205},
			expectedMax: [2]float64{45.89932160591873, 1.2718328120254205},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			min, max := calculateBoundingBox(tt.center, tt.radius)
			if !approxEqual(min[0], tt.expectedMin[0]) || !approxEqual(min[1], tt.expectedMin[1]) {
				t.Errorf("min mismatch: got %v, expected %v", min, tt.expectedMin)
			}
			if !approxEqual(max[0], tt.expectedMax[0]) || !approxEqual(max[1], tt.expectedMax[1]) {
				t.Errorf("max mismatch: got %v, expected %v", max, tt.expectedMax)
			}
		})
	}
}

func approxEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) < epsilon
}

// BenchmarkDistance measures the performance of the distance function using pre-generated pseudo-random coordinates.
func BenchmarkDistance(b *testing.B) {
	const numPairs = 1000
	lats1 := make([]float64, numPairs)
	lons1 := make([]float64, numPairs)
	lats2 := make([]float64, numPairs)
	lons2 := make([]float64, numPairs)

	// Seed for reproducible results
	rand.Seed(42)

	// Pre-generate random latitude and longitude pairs
	for i := 0; i < numPairs; i++ {
		lats1[i] = -90 + rand.Float64()*180  // Latitude: -90 to 90
		lons1[i] = -180 + rand.Float64()*360 // Longitude: -180 to 180
		lats2[i] = -90 + rand.Float64()*180  // Latitude: -90 to 90
		lons2[i] = -180 + rand.Float64()*360 // Longitude: -180 to 180
	}

	// Reset timer to exclude setup time from measurement
	b.ResetTimer()

	// Run the benchmark loop
	for i := 0; i < b.N; i++ {
		idx := i % numPairs
		_ = distance(lats1[idx], lons1[idx], lats2[idx], lons2[idx])
	}
}

func benchmarkSearchWithN(b *testing.B, n int) {
	// Create postOffice
	p := newPostOffice()

	// Create n clients
	clients := make([]*Client, n)
	for i := 0; i < n; i++ {
		clients[i] = &Client{
			loginData: loginData{callsign: fmt.Sprintf("Client%d", i)},
			latLon:    [2]float64{rand.Float64(), rand.Float64()},
			visRange:  10000,
		}
		p.register(clients[i])
	}

	// Define callback
	callback := func(recipient *Client) bool {
		return true
	}

	// Report allocations
	b.ReportAllocs()

	// Reset timer
	b.ResetTimer()

	// Run the benchmark loop
	for i := 0; i < b.N; i++ {
		searchClient := clients[i%10]
		p.search(searchClient, callback)
	}
}

func BenchmarkSearch(b *testing.B) {
	rand.Seed(42)
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			benchmarkSearchWithN(b, n)
		})
	}
}
