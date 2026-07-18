package postoffice

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/session"
)

// Real-world FSD/VATSIM-style geometry: clients clustered around major hubs
// rather than uniform worldwide scatter. Mirrors production load where dense
// events dominate Search/UpdatePosition cost.
//
// Vis ranges (meters = NM * 1852):
//   pilot cruise ~40–80 NM, tower ~50 NM, center/FSS ~200–400 NM
//
// Spatial hash is always on (no dual linear/tree threshold).

const nm = 1852.0

// hub is a geographic traffic center (airport / FIR cluster).
type hub struct {
	name    string
	lat     float64
	lon     float64
	// radiusDeg is the 1σ spread of client positions around the hub.
	radiusDeg float64
}

// Major world hubs weighted like a busy network event day.
var realWorldHubs = []hub{
	{"KLAX", 33.94, -118.41, 0.35},
	{"KJFK", 40.64, -73.78, 0.30},
	{"EGLL", 51.47, -0.46, 0.25},
	{"EDDF", 50.04, 8.56, 0.28},
	{"LFPG", 49.01, 2.55, 0.25},
	{"RJTT", 35.55, 139.78, 0.30},
	{"YSSY", -33.95, 151.18, 0.28},
	{"OMDB", 25.25, 55.36, 0.22},
	{"SBGR", -23.43, -46.47, 0.30},
	{"VHHH", 22.31, 113.91, 0.22},
	{"EHAM", 52.31, 4.77, 0.22},
	{"CYYZ", 43.68, -79.63, 0.28},
}

// realWorldPopulation builds n sessions with realistic mix and geometry.
// ~4% ATC (towers/centers), rest pilots; ~80% of pilots use ProtoRevision 101
// so the closest-velocity path is exercised.
func realWorldPopulation(n int, seed int64) []*session.Session {
	r := rand.New(rand.NewSource(seed))
	clients := make([]*session.Session, n)

	// ~4% ATC floors at least a few controllers at large N.
	nATC := n / 25
	if nATC < 1 {
		nATC = 1
	}

	for i := 0; i < n; i++ {
		h := realWorldHubs[i%len(realWorldHubs)]
		// Gaussian-ish cluster around hub; occasional long-haul outliers.
		lat := h.lat + r.NormFloat64()*h.radiusDeg
		lon := h.lon + r.NormFloat64()*h.radiusDeg
		if lat > 89 {
			lat = 89
		}
		if lat < -89 {
			lat = -89
		}
		if lon > 180 {
			lon -= 360
		}
		if lon < -180 {
			lon += 360
		}

		isATC := i < nATC
		var vis float64
		var cs string
		if isATC {
			// Mix of tower (~50 NM) and center (~250 NM) ranges.
			if i%4 == 0 {
				vis = 250 * nm
				cs = fmt.Sprintf("%s_CTR%d", h.name, i)
			} else {
				vis = 50 * nm
				cs = fmt.Sprintf("%s_TWR%d", h.name, i)
			}
		} else {
			// Pilot cruise visibility 40–80 NM.
			vis = (40 + r.Float64()*40) * nm
			cs = fmt.Sprintf("N%05d", i)
		}

		s := newTestClient(cs, lat, lon, vis)
		s.IsAtc = isATC
		if !isATC && r.Float64() < 0.80 {
			s.ProtoRevision = 101
		} else {
			s.ProtoRevision = 100
		}
		clients[i] = s
	}
	return clients
}

// populate registers every client; b.Fatal on error.
func populate(b *testing.B, p *PostOffice, clients []*session.Session) {
	b.Helper()
	for _, c := range clients {
		if err := p.Register(c); err != nil {
			b.Fatalf("Register %s: %v", c.Callsign, err)
		}
	}
}

func reportPopulationMeta(b *testing.B, p *PostOffice, clients []*session.Session) {
	b.Helper()
	nATC := 0
	for _, c := range clients {
		if c.IsAtc {
			nATC++
		}
	}
	// Warm one Search to sample recipient fan-out from a dense hub pilot.
	var sampleSrc *session.Session
	for _, c := range clients {
		if !c.IsAtc {
			sampleSrc = c
			break
		}
	}
	if sampleSrc == nil {
		sampleSrc = clients[0]
	}
	var recipients int
	p.Search(sampleSrc, func(*session.Session) bool {
		recipients++
		return true
	})

	b.ReportMetric(float64(len(clients)), "clients")
	b.ReportMetric(float64(nATC), "atc")
	b.ReportMetric(float64(recipients), "sample_recipients")
}

// ---------------------------------------------------------------------------
// Per-function real-world benchmarks at N=1000 and N=10000
// ---------------------------------------------------------------------------

func BenchmarkRealWorld(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.Run("Register", func(b *testing.B) { benchRWRegister(b, n) })
			b.Run("Release", func(b *testing.B) { benchRWRelease(b, n) })
			b.Run("Find", func(b *testing.B) { benchRWFind(b, n) })
			b.Run("Send", func(b *testing.B) { benchRWSend(b, n) })
			b.Run("Snapshot", func(b *testing.B) { benchRWSnapshot(b, n) })
			b.Run("All", func(b *testing.B) { benchRWAll(b, n) })
			b.Run("UpdatePosition", func(b *testing.B) { benchRWUpdatePosition(b, n) })
			b.Run("Search", func(b *testing.B) { benchRWSearch(b, n) })
			b.Run("SearchATC", func(b *testing.B) { benchRWSearchATC(b, n) })
			b.Run("BroadcastRanged", func(b *testing.B) { benchRWBroadcastRanged(b, n) })
			b.Run("PositionUpdateThenSearch", func(b *testing.B) { benchRWPosThenSearch(b, n) })
			b.Run("ConcurrentMixed", func(b *testing.B) { benchRWConcurrentMixed(b, n) })
		})
	}
}

// benchRWRegister measures sequential Register of a full population into an empty office.
// Each b.N iteration rebuilds the office so we amortize map growth / tree rebuild.
func benchRWRegister(b *testing.B, n int) {
	clients := realWorldPopulation(n, 42)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := New()
		// Re-seed callsigns unique per outer iter by reusing same sessions only once
		// per New office — sessions can re-register after Release of all.
		b.StartTimer()
		for _, c := range clients {
			if err := p.Register(c); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		// Drain so sessions can be re-registered next iter.
		for _, c := range clients {
			p.Release(c)
		}
	}
	// Report cost per client registration (ns/op is for full population).
	b.ReportMetric(float64(b.N)*float64(n)/b.Elapsed().Seconds(), "reg/s")
	b.ReportMetric(float64(n), "clients_per_op")
}

func benchRWRelease(b *testing.B, n int) {
	clients := realWorldPopulation(n, 43)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := New()
		populate(b, p, clients)
		b.StartTimer()
		for _, c := range clients {
			p.Release(c)
		}
	}
	b.ReportMetric(float64(b.N)*float64(n)/b.Elapsed().Seconds(), "rel/s")
	b.ReportMetric(float64(n), "clients_per_op")
}

func benchRWFind(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 44)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)
	// Precompute callsigns to avoid fmt in the hot loop.
	css := make([]string, n)
	for i, c := range clients {
		css[i] = c.Callsign
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Find(css[i%n]); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRWSend(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 45)
	populate(b, p, clients)
	css := make([]string, n)
	for i, c := range clients {
		css[i] = c.Callsign
	}
	pkt := "#TMN123AB:LAX_TWR:hello\r\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Drain before Send so the 32-slot buffer never blocks (no sender worker).
		dst := clients[i%n]
		_, _ = dst.DequeueOutbound()
		if err := p.Send(css[i%n], pkt); err != nil {
			b.Fatal(err)
		}
	}
}

func benchRWSnapshot(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 46)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)
	b.ReportAllocs()
	b.ResetTimer()
	var sink int
	for i := 0; i < b.N; i++ {
		snap := p.Snapshot()
		sink += len(snap)
	}
	runtime.KeepAlive(sink)
}

func benchRWAll(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 47)
	populate(b, p, clients)
	src := clients[n/2]
	b.ReportAllocs()
	b.ResetTimer()
	var visited int64
	for i := 0; i < b.N; i++ {
		p.All(src, func(*session.Session) bool {
			visited++
			return true
		})
	}
	b.ReportMetric(float64(visited)/float64(b.N), "visited/op")
}

func benchRWUpdatePosition(b *testing.B, n int) {
	p := New()
	r := rand.New(rand.NewSource(48))
	clients := realWorldPopulation(n, 48)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)

	// Precompute small position deltas (taxi / cruise drift) — typical FSD updates
	// move a few hundred meters, not half a continent.
	type move struct {
		c          *session.Session
		lat, lon   float64
		vis        float64
	}
	moves := make([]move, n)
	for i, c := range clients {
		ll := c.LatLon()
		// ~0.01° ≈ 1 km; small continuous drift.
		moves[i] = move{
			c:   c,
			lat: ll[0] + (r.Float64()-0.5)*0.02,
			lon: ll[1] + (r.Float64()-0.5)*0.02,
			vis: c.VisRange.Load(),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := moves[i%n]
		// Keep drifting so quantized boxes eventually change on tree path.
		dlat := float64((i/n)%7) * 0.005
		dlon := float64((i/n)%5) * 0.005
		p.UpdatePosition(m.c, [2]float64{m.lat + dlat, m.lon + dlon}, m.vis)
	}
}

func benchRWSearch(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 49)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)

	// Prefer pilots as searchers (position-broadcast source).
	searchers := make([]*session.Session, 0, n)
	for _, c := range clients {
		if !c.IsAtc {
			searchers = append(searchers, c)
		}
	}
	if len(searchers) == 0 {
		searchers = clients
	}

	var totalRecipients int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := searchers[i%len(searchers)]
		var hit int
		p.Search(src, func(*session.Session) bool {
			hit++
			return true
		})
		totalRecipients += int64(hit)
	}
	b.ReportMetric(float64(totalRecipients)/float64(b.N), "recipients/op")
}

func benchRWSearchATC(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 50)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)

	var totalRecipients int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := clients[i%n]
		var hit int
		p.SearchATC(src, func(*session.Session) bool {
			hit++
			return true
		})
		totalRecipients += int64(hit)
	}
	b.ReportMetric(float64(totalRecipients)/float64(b.N), "atc_recipients/op")
}

// benchRWBroadcastRanged is the production hot path: Search + SendPosition fan-out
// (position packet to every in-range peer), with channel drain to avoid blocking.
func benchRWBroadcastRanged(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 51)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)

	searchers := make([]*session.Session, 0, n)
	for _, c := range clients {
		if !c.IsAtc {
			searchers = append(searchers, c)
		}
	}
	if len(searchers) == 0 {
		searchers = clients
	}

	pkt := "@S:N00001:1200:1:33.94:-118.41:5000:250:4261294148:0\r\n"
	var totalRecipients int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := searchers[i%len(searchers)]
		var hit int
		p.Search(src, func(r *session.Session) bool {
			_ = r.SendPosition(pkt)
			_, _ = r.DequeueOutbound()
			hit++
			return true
		})
		totalRecipients += int64(hit)
	}
	b.ReportMetric(float64(totalRecipients)/float64(b.N), "recipients/op")
	// Approximate sustained position broadcasts/sec for this N.
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "broadcasts/s")
}

// benchRWPosThenSearch models a pilot position update: UpdatePosition then ranged broadcast.
func benchRWPosThenSearch(b *testing.B, n int) {
	p := New()
	r := rand.New(rand.NewSource(52))
	clients := realWorldPopulation(n, 52)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)

	pilots := make([]*session.Session, 0, n)
	for _, c := range clients {
		if !c.IsAtc {
			pilots = append(pilots, c)
		}
	}
	if len(pilots) == 0 {
		pilots = clients
	}

	pkt := "@S:N00001:1200:1:33.94:-118.41:5000:250:0:0\r\n"
	var totalRecipients int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := pilots[i%len(pilots)]
		ll := src.LatLon()
		p.UpdatePosition(src, [2]float64{
			ll[0] + (r.Float64()-0.5)*0.01,
			ll[1] + (r.Float64()-0.5)*0.01,
		}, src.VisRange.Load())
		var hit int
		p.Search(src, func(rec *session.Session) bool {
			_ = rec.SendPosition(pkt)
			_, _ = rec.DequeueOutbound()
			hit++
			return true
		})
		totalRecipients += int64(hit)
	}
	b.ReportMetric(float64(totalRecipients)/float64(b.N), "recipients/op")
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "pos_cycles/s")
}

// benchRWConcurrentMixed simulates a dense event: many goroutines updating
// positions while others run Search fan-out. Reports aggregate ops/s under contention.
func benchRWConcurrentMixed(b *testing.B, n int) {
	p := New()
	clients := realWorldPopulation(n, 53)
	populate(b, p, clients)
	reportPopulationMeta(b, p, clients)

	workers := runtime.GOMAXPROCS(0)
	if workers < 4 {
		workers = 4
	}
	updaters := workers / 2
	if updaters < 1 {
		updaters = 1
	}
	searchers := workers - updaters
	if searchers < 1 {
		searchers = 1
	}

	pkt := "@S:N00001:1200:1:34.0:-118.0:5000:250:0:0\r\n"
	var updates, searches, recipients atomic.Int64

	b.ReportAllocs()
	b.ResetTimer()

	// Each b.N is one "wave" of concurrent work across all workers.
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		// Position updaters: each touches a stripe of clients once.
		for w := 0; w < updaters; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				r := rand.New(rand.NewSource(int64(id*1000 + i)))
				for j := id; j < n; j += updaters {
					c := clients[j]
					ll := c.LatLon()
					p.UpdatePosition(c, [2]float64{
						ll[0] + (r.Float64()-0.5)*0.01,
						ll[1] + (r.Float64()-0.5)*0.01,
					}, c.VisRange.Load())
					updates.Add(1)
				}
			}(w)
		}
		// Search fan-out workers.
		for w := 0; w < searchers; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				src := clients[(id*97+i)%n]
				var hit int
				p.Search(src, func(rec *session.Session) bool {
					_ = rec.SendPosition(pkt)
					_, _ = rec.DequeueOutbound()
					hit++
					return true
				})
				searches.Add(1)
				recipients.Add(int64(hit))
			}(w)
		}
		wg.Wait()
	}

	elapsed := b.Elapsed().Seconds()
	if elapsed <= 0 {
		elapsed = 1e-9
	}
	b.ReportMetric(float64(updates.Load())/elapsed, "updates/s")
	b.ReportMetric(float64(searches.Load())/elapsed, "searches/s")
	if s := searches.Load(); s > 0 {
		b.ReportMetric(float64(recipients.Load())/float64(s), "recipients/search")
	}
	b.ReportMetric(float64(workers), "workers")
}

// ---------------------------------------------------------------------------
// Latency histogram helper: fixed-iteration real-world sample (not Go bench loop)
// ---------------------------------------------------------------------------

// TestRealWorldLatencySample is a non-benchmark that prints p50/p95/p99 latency
// for the hot path at 1k and 10k. Run with:
//
//	go test -run TestRealWorldLatencySample -v -count=1 ./internal/postoffice/
func TestRealWorldLatencySample(t *testing.T) {
	const samples = 2000
	for _, n := range []int{1000, 10000} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			p := New()
			clients := realWorldPopulation(n, 99)
			for _, c := range clients {
				if err := p.Register(c); err != nil {
					t.Fatal(err)
				}
			}
			mode := "slab"

			pilots := make([]*session.Session, 0, n)
			for _, c := range clients {
				if !c.IsAtc {
					pilots = append(pilots, c)
				}
			}
			pkt := "@S:N00001:1200:1:33.94:-118.41:5000:250:0:0\r\n"
			r := rand.New(rand.NewSource(99))

			// Warmup.
			for i := 0; i < 50; i++ {
				src := pilots[i%len(pilots)]
				p.Search(src, func(rec *session.Session) bool {
					_ = rec.SendPosition(pkt)
					_, _ = rec.DequeueOutbound()
					return true
				})
			}

			type sample struct {
				searchNs   int64
				updateNs   int64
				findNs     int64
				broadcastNs int64
				recipients int
			}
			out := make([]sample, samples)

			for i := 0; i < samples; i++ {
				src := pilots[i%len(pilots)]
				cs := clients[i%n].Callsign

				t0 := time.Now()
				_, _ = p.Find(cs)
				findNs := time.Since(t0).Nanoseconds()

				ll := src.LatLon()
				t0 = time.Now()
				p.UpdatePosition(src, [2]float64{
					ll[0] + (r.Float64()-0.5)*0.01,
					ll[1] + (r.Float64()-0.5)*0.01,
				}, src.VisRange.Load())
				updateNs := time.Since(t0).Nanoseconds()

				t0 = time.Now()
				var hit int
				p.Search(src, func(*session.Session) bool {
					hit++
					return true
				})
				searchNs := time.Since(t0).Nanoseconds()

				t0 = time.Now()
				var hit2 int
				p.Search(src, func(rec *session.Session) bool {
					_ = rec.SendPosition(pkt)
					_, _ = rec.DequeueOutbound()
					hit2++
					return true
				})
				broadcastNs := time.Since(t0).Nanoseconds()

				out[i] = sample{
					searchNs:    searchNs,
					updateNs:    updateNs,
					findNs:      findNs,
					broadcastNs: broadcastNs,
					recipients:  hit2,
				}
			}

			pct := func(vals []int64, p float64) float64 {
				cp := append([]int64(nil), vals...)
				// insertion sort — samples is small
				for i := 1; i < len(cp); i++ {
					j := i
					for j > 0 && cp[j] < cp[j-1] {
						cp[j], cp[j-1] = cp[j-1], cp[j]
						j--
					}
				}
				idx := int(math.Ceil(p*float64(len(cp)))) - 1
				if idx < 0 {
					idx = 0
				}
				if idx >= len(cp) {
					idx = len(cp) - 1
				}
				return float64(cp[idx])
			}
			col := func(get func(sample) int64) []int64 {
				v := make([]int64, len(out))
				for i, s := range out {
					v[i] = get(s)
				}
				return v
			}
			meanRec := 0.0
			for _, s := range out {
				meanRec += float64(s.recipients)
			}
			meanRec /= float64(len(out))

			finds := col(func(s sample) int64 { return s.findNs })
			updates := col(func(s sample) int64 { return s.updateNs })
			searches := col(func(s sample) int64 { return s.searchNs })
			broadcasts := col(func(s sample) int64 { return s.broadcastNs })

			t.Logf("n=%d mode=%s samples=%d mean_recipients=%.1f", n, mode, samples, meanRec)
			t.Logf("  Find           p50=%.0fns p95=%.0fns p99=%.0fns", pct(finds, 0.50), pct(finds, 0.95), pct(finds, 0.99))
			t.Logf("  UpdatePosition p50=%.0fns p95=%.0fns p99=%.0fns", pct(updates, 0.50), pct(updates, 0.95), pct(updates, 0.99))
			t.Logf("  Search         p50=%.0fns p95=%.0fns p99=%.0fns", pct(searches, 0.50), pct(searches, 0.95), pct(searches, 0.99))
			t.Logf("  BroadcastRanged p50=%.0fns p95=%.0fns p99=%.0fns", pct(broadcasts, 0.50), pct(broadcasts, 0.95), pct(broadcasts, 0.99))
		})
	}
}
