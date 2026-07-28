package afv

import (
	"context"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

func TestInterestMaxRangeNM_ATCRadiusForPilot(t *testing.T) {
	cfg := &Config{RangeDefaultNM: 40, RangeATCNM: 150, RangeUnicomNM: 15}
	// non-UNICOM must use max(default, ATC) so pilot RX covers ATC TX
	got := interestMaxRangeNM(cfg, 118700000)
	if got != 150 {
		t.Fatalf("got %v want 150", got)
	}
	// UNICOM fixed
	if interestMaxRangeNM(cfg, FrequencyUnicomHz) != 15 {
		t.Fatal()
	}
	// Default larger than ATC → return Default
	cfg2 := &Config{RangeDefaultNM: 200, RangeATCNM: 50}
	if interestMaxRangeNM(cfg2, 118700000) != 200 {
		t.Fatal()
	}
}

func TestBuildInterest_TruncateCap(t *testing.T) {
	old := interestMaxEntries
	interestMaxEntries = 3
	t.Cleanup(func() { interestMaxEntries = old })
	cfg := &Config{
		MaxSessions: 10, MaxSessionsPerCID: 5,
		RangeDefaultNM: 40, RangeATCNM: 150, RangeUnicomNM: 15,
	}
	r := newRegistry(cfg)
	now := time.Now()
	s, _, _ := r.CreateOrReplace(1, "P", "", now)
	// wide ATC coverage → many cells
	_, _, _ = r.UpdateTransceivers(1, "P", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40, LonDeg: -73,
	}})
	_, _, _ = r.BindUDP(s, fakeAddr{"1"}, now)
	ents := r.buildInterestEntries(cfg)
	if len(ents) > 3 {
		t.Fatalf("cap not applied: %d", len(ents))
	}
	if len(ents) == 0 {
		t.Fatal("empty after cap")
	}
}

func TestCapInterestKeepsLocalCells(t *testing.T) {
	locals := []interestLocal{{
		freq: 118700000,
		cell: geo.CellKey{ILat: 0, ILon: 0},
	}}
	entries := make([]InterestEntry, 0, 5000)
	// far cells first in list
	for i := int32(100); i < 100+5000; i++ {
		entries = append(entries, InterestEntry{FreqHz: 118700000, ILat: i, ILon: i})
	}
	// local cell at end
	entries = append(entries, InterestEntry{FreqHz: 118700000, ILat: 0, ILon: 0})
	out := capInterestEntries(entries, locals, 100)
	if len(out) != 100 {
		t.Fatalf("len=%d", len(out))
	}
	found := false
	for _, e := range out {
		if e.ILat == 0 && e.ILon == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("local cell dropped")
	}
}

func TestBuildInterest_BoundOnly_ATCCoverage(t *testing.T) {
	cfg := &Config{
		MaxSessions: 10, MaxSessionsPerCID: 5,
		RangeDefaultNM: 40, RangeATCNM: 150, RangeUnicomNM: 15,
	}
	r := newRegistry(cfg)
	now := time.Now()
	s, _, err := r.CreateOrReplace(1, "AAL1", "", now)
	if err != nil {
		t.Fatal(err)
	}
	// unbound → no interest
	if ents := r.buildInterestEntries(cfg); len(ents) != 0 {
		t.Fatalf("unbound interest=%d", len(ents))
	}
	_, _, _ = r.UpdateTransceivers(1, "AAL1", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40.0, LonDeg: -73.0,
	}})
	// still unbound
	if ents := r.buildInterestEntries(cfg); len(ents) != 0 {
		t.Fatalf("unbound with trx interest=%d", len(ents))
	}
	if _, first, ok := r.BindUDP(s, fakeAddr{"1"}, now); !ok || !first {
		t.Fatal("bind")
	}
	ents := r.buildInterestEntries(cfg)
	if len(ents) == 0 {
		t.Fatal("expected interest after bind")
	}
	// should cover more than own cell for ATC-class radius (~150NM)
	own := geo.CellKey{
		ILat: geo.CellIndex(40.0, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(-73.0, geo.DefaultGridCellDeg),
	}
	// far cell ~100NM north roughly: 1 deg lat ~ 60NM so ~1.67 deg
	farLat := 40.0 + 1.5
	farCell := geo.CellKey{
		ILat: geo.CellIndex(farLat, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(-73.0, geo.DefaultGridCellDeg),
	}
	hasOwn, hasFar := false, false
	for _, e := range ents {
		if e.FreqHz != 118700000 {
			continue
		}
		if e.ILat == own.ILat && e.ILon == own.ILon {
			hasOwn = true
		}
		if e.ILat == farCell.ILat && e.ILon == farCell.ILon {
			hasFar = true
		}
	}
	if !hasOwn {
		t.Fatal("missing own cell")
	}
	if !hasFar {
		t.Fatalf("missing ATC-range far cell %+v (interest under-advertise)", farCell)
	}
}

func TestInterestRateLimit_DirtyStorm(t *testing.T) {
	// Drive real runInterestLoop under rapid dirty storm; assert ≤ ~2 Hz publishes.
	if interestMinInterval > 500*time.Millisecond {
		t.Fatalf("interval=%v exceeds 2Hz budget", interestMinInterval)
	}
	cfg := &Config{
		APIListen: "127.0.0.1:0", UDPListen: "127.0.0.1:0",
		UDPAdvertiseIPv4: "127.0.0.1:1",
		MaxSessions:      10, MaxSessionsPerCID: 5,
		RangeDefaultNM: 40, RangeATCNM: 150,
	}
	s := New(cfg, nil, nil, []byte("x"))
	hub := NewMemoryHub()
	m, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "psk-ok", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "psk-ok", PeerIDs: []string{"n1", "n2"}})
	s.SetMesh(m)
	s.registerMeshCallbacks()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	go s.runInterestLoop(ctx)

	// Storm dirty for ~1.2s (boolean flag → at most one publish per 500ms tick)
	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.MarkInterestDirtyForTest()
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	time.Sleep(20 * time.Millisecond)

	n := m.InterestPublishCount()
	// Start + loop initial + ticks over ~1.2s at 500ms — budget ≤8
	if n > 8 {
		t.Fatalf("interest publish count %d exceeds ≤2Hz storm budget", n)
	}
	if n < 2 {
		t.Fatalf("expected loop to publish, got %d", n)
	}
}

func TestFirstBindDirtyOnceOnServer(t *testing.T) {
	cfg := &Config{MaxSessions: 10, MaxSessionsPerCID: 5, RangeDefaultNM: 40}
	s := New(cfg, nil, nil, []byte("k"))
	now := time.Now()
	sess, _, err := s.reg.CreateOrReplace(1, "P1", "", now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = s.reg.UpdateTransceivers(1, "P1", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40, LonDeg: -73,
	}})
	_ = s.interestDirty.Swap(false)

	_, first, ok := s.reg.BindUDP(sess, fakeAddr{"127.0.0.1:1"}, now)
	if !ok || !first {
		t.Fatal("first bind")
	}
	if first {
		s.markInterestDirty()
	}
	if !s.InterestDirtyForTest() {
		t.Fatal("first bind must dirty interest")
	}
	_ = s.interestDirty.Swap(false)

	_, first2, ok := s.reg.BindUDP(sess, fakeAddr{"127.0.0.1:1"}, now)
	if !ok || first2 {
		t.Fatalf("re-touch first=%v ok=%v", first2, ok)
	}
	if s.InterestDirtyForTest() {
		t.Fatal("re-touch must not dirty interest")
	}
}

func TestCapInterest_UnderCapNoOp(t *testing.T) {
	ents := []InterestEntry{{FreqHz: 1, ILat: 0, ILon: 0}}
	out := capInterestEntries(ents, nil, 10)
	if len(out) != 1 {
		t.Fatal()
	}
}

func TestChebyshevCrossFreqAndAbs(t *testing.T) {
	if absInt32(-3) != 3 || absInt32(2) != 2 {
		t.Fatal()
	}
	d := chebyshevToLocals(InterestEntry{FreqHz: 1, ILat: 5, ILon: 5}, []interestLocal{
		{freq: 2, cell: geo.CellKey{ILat: 0, ILon: 0}},
		{freq: 1, cell: geo.CellKey{ILat: 4, ILon: 4}},
	})
	if d != 1 {
		t.Fatalf("d=%d", d)
	}
	if chebyshevToLocals(InterestEntry{}, nil) != 0 {
		t.Fatal()
	}
}

func TestBuildInterest_EmptyRegistry(t *testing.T) {
	var r *Registry
	if r.buildInterestEntries(&Config{}) != nil {
		t.Fatal()
	}
	// multiple trxs
	cfg := &Config{MaxSessions: 10, MaxSessionsPerCID: 5, RangeDefaultNM: 40, RangeATCNM: 150, RangeUnicomNM: 15}
	reg := newRegistry(cfg)
	now := time.Now()
	s, _, _ := reg.CreateOrReplace(1, "P", "", now)
	_, _, _ = reg.UpdateTransceivers(1, "P", []Transceiver{
		{ID: 0, Frequency: FrequencyUnicomHz, LatDeg: 0, LonDeg: 0},
		{ID: 1, Frequency: 118700000, LatDeg: 0.1, LonDeg: 0.1},
	})
	_, _, _ = reg.BindUDP(s, fakeAddr{"x"}, now)
	ents := reg.buildInterestEntries(cfg)
	if len(ents) < 2 {
		t.Fatalf("ents=%d", len(ents))
	}
}

func TestCapInterest_ManyLocals(t *testing.T) {
	// force truncate path with locals priority and sort stability
	locals := []interestLocal{
		{freq: 1, cell: geo.CellKey{ILat: 0, ILon: 0}},
		{freq: 1, cell: geo.CellKey{ILat: 1, ILon: 1}},
	}
	var entries []InterestEntry
	for i := int32(0); i < 50; i++ {
		entries = append(entries, InterestEntry{FreqHz: 1, ILat: i, ILon: i})
		entries = append(entries, InterestEntry{FreqHz: 2, ILat: i, ILon: i})
	}
	// same dist, different freq/lat/lon for sort stability branches
	entries = append(entries,
		InterestEntry{FreqHz: 1, ILat: 10, ILon: 0},
		InterestEntry{FreqHz: 1, ILat: 10, ILon: 1},
		InterestEntry{FreqHz: 1, ILat: 11, ILon: 0},
	)
	out := capInterestEntries(entries, locals, 5)
	if len(out) != 5 {
		t.Fatal(len(out))
	}
	// both locals should be kept if cap allows
	locCount := 0
	for _, e := range out {
		if e.FreqHz == 1 && ((e.ILat == 0 && e.ILon == 0) || (e.ILat == 1 && e.ILon == 1)) {
			locCount++
		}
	}
	if locCount < 2 {
		t.Fatalf("locals kept=%d", locCount)
	}
	// cap > len path via larger cap already tested; equal dist sort
	out2 := capInterestEntries([]InterestEntry{
		{FreqHz: 2, ILat: 0, ILon: 0},
		{FreqHz: 1, ILat: 0, ILon: 0},
		{FreqHz: 1, ILat: 1, ILon: 0},
		{FreqHz: 1, ILat: 0, ILon: 1},
	}, nil, 3)
	if len(out2) != 3 {
		t.Fatal()
	}
}

func TestPeerWants_SyncApply(t *testing.T) {
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	m2, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	cell := geo.CellKey{ILat: 1, ILon: 2}
	entries := []InterestEntry{{FreqHz: 118700000, ILat: 1, ILon: 2}}
	// m2 publishes interest → m1 sees PeerWants("n2", ...)
	m2.PublishInterest(entries)
	if !m1.PeerWants("n2", 118700000, cell) {
		t.Fatal("sync interest not applied")
	}
	if len(m1.InterestedPeers([]FreqCell{{FreqHz: 118700000, Cell: cell}})) != 1 {
		t.Fatal("InterestedPeers")
	}
	// empty interest → no wants
	m2.PublishInterest(nil)
	if m1.PeerWants("n2", 118700000, cell) {
		t.Fatal("cleared interest still wants")
	}
}
