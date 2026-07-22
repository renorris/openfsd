//go:build stress

// Stress baselines for concurrent registry + position update fan-out.
//
// Not compiled into default `go test` (build tag `stress`). Run with:
//
//	go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
//
// Live VATSIM snapshot (positions from data.vatsim.net):
//
//	curl -sS https://data.vatsim.net/v3/vatsim-data.json -o /tmp/vatsim-data.json
//	curl -sS https://data.vatsim.net/v3/transceivers-data.json -o /tmp/vatsim-tx.json
//	OPENFSD_STRESS_VATSIM_JSON=/tmp/vatsim-data.json \
//	OPENFSD_STRESS_TRANSCEIVERS_JSON=/tmp/vatsim-tx.json \
//	  go test -tags=stress -count=1 -timeout=180s ./internal/server/ -run TestStressVATSIMLive -v
//
// Metrics:
//   - enqueue_p50/p99: handler-start → successful sendChan enqueue (design metric),
//     sampled when Search callbacks complete recipient.Send under an instrumented Registry.
//   - handler_wall_p50/p99: full handlePilotPosition wall time under concurrent load.
//
// Absolute latency thresholds are intentionally NOT asserted — cold shared
// runners vary widely. p50/p99 are logged for the PR summary baseline.
// Also respects testing.Short() if someone passes -short with the tag.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// stressPilots is the default concurrent pilot count (design: M=1000).
const stressPilots = 1000

// stressHz is the position update rate per pilot (design: 0.2 Hz).
const stressHz = 0.2

// stressDuration is how long the default stress run lasts (design: T=10s).
const stressDuration = 10 * time.Second

// stressRegistry wraps postoffice to sample handler-start → enqueue latency.
// broadcastRanged calls Search(s, fn) where fn does recipient.Send; Send returns
// after a successful sendChan enqueue, so time.Since(handlerStart) after fn is
// the design "handler-to-enqueue" metric.
type stressRegistry struct {
	inner *postoffice.PostOffice

	// handlerStart maps sender callsign → time.Now() at handlePilotPosition entry.
	handlerStart sync.Map // string -> time.Time

	enqMu      sync.Mutex
	enqSamples []time.Duration
	enqCount   atomic.Int64
}

func newStressRegistry() *stressRegistry {
	return &stressRegistry{inner: postoffice.New()}
}

func (r *stressRegistry) Register(s *session.Session) error { return r.inner.Register(s) }
func (r *stressRegistry) Release(s *session.Session)        { r.inner.Release(s) }
func (r *stressRegistry) UpdatePosition(s *session.Session, center [2]float64, visRangeM float64) {
	r.inner.UpdatePosition(s, center, visRangeM)
}
func (r *stressRegistry) All(except *session.Session, fn func(*session.Session) bool) {
	r.inner.All(except, fn)
}
func (r *stressRegistry) Send(callsign, packet string) error {
	return r.inner.Send(callsign, packet)
}
func (r *stressRegistry) Find(callsign string) (*session.Session, error) {
	return r.inner.Find(callsign)
}
func (r *stressRegistry) Snapshot() []*session.Session { return r.inner.Snapshot() }

func (r *stressRegistry) Search(s *session.Session, fn func(*session.Session) bool) {
	startI, ok := r.handlerStart.Load(s.Callsign)
	var start time.Time
	if ok {
		start = startI.(time.Time)
	}
	// Collect samples locally then merge once — a global mutex per recipient
	// (≈N² locks under full mesh) dominated wall time and hid real fan-out cost.
	var local []time.Duration
	r.inner.Search(s, func(recipient *session.Session) bool {
		// fn typically does recipient.Send — returns after sendChan enqueue
		// (SendPosition is non-blocking for position storms).
		ret := fn(recipient)
		if ok && !start.IsZero() {
			local = append(local, time.Since(start))
			r.enqCount.Add(1)
		}
		return ret
	})
	if len(local) > 0 {
		r.enqMu.Lock()
		r.enqSamples = append(r.enqSamples, local...)
		r.enqMu.Unlock()
	}
}

func (r *stressRegistry) markHandlerStart(callsign string, t time.Time) {
	r.handlerStart.Store(callsign, t)
}

func (r *stressRegistry) clearHandlerStart(callsign string) {
	r.handlerStart.Delete(callsign)
}

func TestStressConcurrentPositionUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress baseline under -short")
	}

	m := stressPilots
	if v := os.Getenv("OPENFSD_STRESS_M"); v != "" {
		var parsed int
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil && parsed > 0 {
			m = parsed
		}
	}

	reg := newStressRegistry()
	metar := &recordingMetar{}
	srv, err := New(Deps{
		Config:   &Config{FsdListenAddrs: []string{":0"}},
		Users:    stubUserStore{},
		ConfigKV: stubConfigStore{},
		Registry: reg,
		Metar:    metar,
		Clock:    realClock{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// SendEnqueueObserver: drain outbound so Send never blocks on the 32-buffer,
	// and count enqueues (observer is the design hook for enqueue-side work).
	var drainCount atomic.Int64

	pilots := make([]*session.Session, m)
	for i := 0; i < m; i++ {
		cs := fmt.Sprintf("N%04d", i)
		s := session.New(context.Background(), nil, nil, session.LoginData{
			Callsign:      cs,
			IsAtc:         false,
			NetworkRating: protocol.NetworkRatingObserver,
			ProtoRevision: 101,
			CID:           i + 1,
			RealName:      "Stress",
		})
		// Cluster around LAX so ranged search finds peers.
		// Position is applied via UpdatePosition (SetGeo); avoid double-writing coords.
		lat := 33.94 + float64(i%10)*0.01
		lon := -118.40 + float64(i/10)*0.01
		s.SetSendEnqueueObserver(func(callsign string, enqueuedAt time.Time, queueDepth int) {
			drainCount.Add(1)
			// Drain immediately so Send never blocks on the 32-buffer.
			_, _ = s.DequeueOutbound()
		})
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{lat, lon}, 50*1852)
		pilots[i] = s
	}

	interval := time.Duration(float64(time.Second) / stressHz)
	deadline := time.Now().Add(stressDuration)

	var wallSamples []time.Duration
	var wallMu sync.Mutex
	var updates atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < m; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := pilots[idx]
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if time.Now().After(deadline) {
					return
				}
				select {
				case <-ticker.C:
				case <-time.After(time.Until(deadline)):
					return
				}
				lat, lon := s.LatLon()[0], s.LatLon()[1]
				lat += 0.0001
				pkt := []byte(fmt.Sprintf("@S:%s:1200:1:%.5f:%.5f:5000:250:0:0\r\n", s.Callsign, lat, lon))

				start := time.Now()
				reg.markHandlerStart(s.Callsign, start)
				srv.handlePilotPosition(s, pkt)
				reg.clearHandlerStart(s.Callsign)
				elapsed := time.Since(start)

				updates.Add(1)
				wallMu.Lock()
				wallSamples = append(wallSamples, elapsed)
				wallMu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	reg.enqMu.Lock()
	enqCopy := append([]time.Duration(nil), reg.enqSamples...)
	reg.enqMu.Unlock()

	enqP50, enqP99 := percentile(enqCopy, 0.50), percentile(enqCopy, 0.99)
	wallP50, wallP99 := percentile(wallSamples, 0.50), percentile(wallSamples, 0.99)

	t.Logf("stress baseline: M=%d T=%s rate=%.2fHz updates=%d enqueues=%d observer_drains=%d enqueue_p50=%s enqueue_p99=%s enqueue_max=%s handler_wall_p50=%s handler_wall_p99=%s handler_wall_max=%s",
		m, stressDuration, stressHz, updates.Load(), reg.enqCount.Load(), drainCount.Load(),
		enqP50, enqP99, maxDuration(enqCopy),
		wallP50, wallP99, maxDuration(wallSamples))

	// Soft sanity only — do not fail on absolute 5ms.
	if updates.Load() == 0 {
		t.Fatal("no position updates completed")
	}
	if reg.enqCount.Load() == 0 {
		t.Fatal("no broadcast enqueues sampled (registry Search path unused?)")
	}
	if drainCount.Load() == 0 {
		t.Fatal("SendEnqueueObserver never fired")
	}
	if enqP99 > 5*time.Second {
		t.Fatalf("enqueue p99 pathologically high: %s", enqP99)
	}
	if wallP99 > 5*time.Second {
		t.Fatalf("handler wall p99 pathologically high: %s", wallP99)
	}
}

func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), ds...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(math.Ceil(p*float64(len(cp)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func maxDuration(ds []time.Duration) time.Duration {
	var m time.Duration
	for _, d := range ds {
		if d > m {
			m = d
		}
	}
	return m
}

// VATSIM live data feed shapes (subset of https://data.vatsim.net/v3/vatsim-data.json).
type vatsimDataFeed struct {
	General struct {
		UpdateTimestamp string `json:"update_timestamp"`
		Connected       int    `json:"connected_clients"`
		UniqueUsers     int    `json:"unique_users"`
	} `json:"general"`
	Pilots []struct {
		CID         int     `json:"cid"`
		Callsign    string  `json:"callsign"`
		Name        string  `json:"name"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Altitude    int     `json:"altitude"`
		Groundspeed int     `json:"groundspeed"`
		Heading     int     `json:"heading"`
		Transponder string  `json:"transponder"`
	} `json:"pilots"`
	Controllers []struct {
		CID         int    `json:"cid"`
		Callsign    string `json:"callsign"`
		Name        string `json:"name"`
		Facility    int    `json:"facility"`
		Rating      int    `json:"rating"`
		VisualRange int    `json:"visual_range"`
		Frequency   string `json:"frequency"`
	} `json:"controllers"`
	ATIS []struct {
		CID         int    `json:"cid"`
		Callsign    string `json:"callsign"`
		Name        string `json:"name"`
		Facility    int    `json:"facility"`
		Rating      int    `json:"rating"`
		VisualRange int    `json:"visual_range"`
		Frequency   string `json:"frequency"`
	} `json:"atis"`
}

// Transceiver positions (https://data.vatsim.net/v3/transceivers-data.json) —
// controllers/ATIS in the main feed lack lat/lon; radios carry them.
type vatsimTransceiverEntry struct {
	Callsign     string `json:"callsign"`
	Transceivers []struct {
		LatDeg float64 `json:"latDeg"`
		LonDeg float64 `json:"lonDeg"`
	} `json:"transceivers"`
}

func loadVatsimTransceiverPositions(path string) (map[string][2]float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []vatsimTransceiverEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	out := make(map[string][2]float64, len(entries))
	for _, e := range entries {
		if len(e.Transceivers) == 0 || e.Callsign == "" {
			continue
		}
		var latSum, lonSum float64
		for _, tr := range e.Transceivers {
			latSum += tr.LatDeg
			lonSum += tr.LonDeg
		}
		n := float64(len(e.Transceivers))
		out[e.Callsign] = [2]float64{latSum / n, lonSum / n}
	}
	return out, nil
}

func drainObserver(drainCount *atomic.Int64, s *session.Session) {
	s.SetSendEnqueueObserver(func(callsign string, enqueuedAt time.Time, queueDepth int) {
		drainCount.Add(1)
		_, _ = s.DequeueOutbound()
	})
}

// TestStressVATSIMLive replays concurrent pilot position updates against a
// registry populated from a live VATSIM network snapshot (real worldwide
// geometry + ATC visual ranges). Requires OPENFSD_STRESS_VATSIM_JSON; optional
// OPENFSD_STRESS_TRANSCEIVERS_JSON places controllers/ATIS.
//
// OPENFSD_STRESS_T (seconds, default 10) and OPENFSD_STRESS_HZ (default 0.2)
// override duration and per-pilot rate.
func TestStressVATSIMLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping VATSIM live stress under -short")
	}

	dataPath := os.Getenv("OPENFSD_STRESS_VATSIM_JSON")
	if dataPath == "" {
		t.Skip("set OPENFSD_STRESS_VATSIM_JSON to a vatsim-data.json snapshot")
	}
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read VATSIM snapshot: %v", err)
	}
	var feed vatsimDataFeed
	if err := json.Unmarshal(raw, &feed); err != nil {
		t.Fatalf("parse VATSIM snapshot: %v", err)
	}
	if len(feed.Pilots) == 0 {
		t.Fatal("VATSIM snapshot has zero pilots")
	}

	txPos := map[string][2]float64{}
	if txPath := os.Getenv("OPENFSD_STRESS_TRANSCEIVERS_JSON"); txPath != "" {
		txPos, err = loadVatsimTransceiverPositions(txPath)
		if err != nil {
			t.Fatalf("read transceivers snapshot: %v", err)
		}
	}

	hz := stressHz
	if v := os.Getenv("OPENFSD_STRESS_HZ"); v != "" {
		var parsed float64
		if _, err := fmt.Sscanf(v, "%f", &parsed); err == nil && parsed > 0 {
			hz = parsed
		}
	}
	duration := stressDuration
	if v := os.Getenv("OPENFSD_STRESS_T"); v != "" {
		var sec float64
		if _, err := fmt.Sscanf(v, "%f", &sec); err == nil && sec > 0 {
			duration = time.Duration(sec * float64(time.Second))
		}
	}

	reg := newStressRegistry()
	srv, err := New(Deps{
		Config:   &Config{FsdListenAddrs: []string{":0"}},
		Users:    stubUserStore{},
		ConfigKV: stubConfigStore{},
		Registry: reg,
		Metar:    &recordingMetar{},
		Clock:    realClock{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const pilotVisM = 50.0 * 1852.0
	var drainCount atomic.Int64
	var nATC int
	var skippedATC int

	// Register ATC/ATIS first (static positions for the run; they still receive
	// ranged broadcasts and affect Search cost via large visual ranges).
	registerATC := func(cs, name string, cid, facility, rating, visNM int) {
		pos, ok := txPos[cs]
		if !ok {
			skippedATC++
			return
		}
		if visNM <= 0 {
			visNM = 50
		}
		ratingN := protocol.NetworkRating(rating)
		if ratingN < protocol.NetworkRatingObserver {
			ratingN = protocol.NetworkRatingObserver
		}
		s := session.New(context.Background(), nil, nil, session.LoginData{
			Callsign:      cs,
			IsAtc:         true,
			NetworkRating: ratingN,
			ProtoRevision: 100,
			CID:           cid,
			RealName:      name,
		})
		s.FacilityType.Store(int32(facility))
		drainObserver(&drainCount, s)
		if err := reg.Register(s); err != nil {
			// Duplicate callsign across controllers/ATIS is rare; skip.
			t.Logf("skip ATC register %s: %v", cs, err)
			skippedATC++
			return
		}
		reg.UpdatePosition(s, pos, float64(visNM)*1852.0)
		nATC++
	}
	for _, c := range feed.Controllers {
		registerATC(c.Callsign, c.Name, c.CID, c.Facility, c.Rating, c.VisualRange)
	}
	for _, a := range feed.ATIS {
		registerATC(a.Callsign, a.Name, a.CID, a.Facility, a.Rating, a.VisualRange)
	}

	pilots := make([]*session.Session, 0, len(feed.Pilots))
	for i, p := range feed.Pilots {
		if p.Callsign == "" {
			continue
		}
		// Skip invalid / missing coords.
		if p.Latitude < -90 || p.Latitude > 90 || (p.Latitude == 0 && p.Longitude == 0) {
			continue
		}
		cid := p.CID
		if cid == 0 {
			cid = i + 1
		}
		s := session.New(context.Background(), nil, nil, session.LoginData{
			Callsign:      p.Callsign,
			IsAtc:         false,
			NetworkRating: protocol.NetworkRatingObserver,
			ProtoRevision: 101,
			CID:           cid,
			RealName:      p.Name,
		})
		drainObserver(&drainCount, s)
		if err := reg.Register(s); err != nil {
			t.Logf("skip pilot register %s: %v", p.Callsign, err)
			continue
		}
		reg.UpdatePosition(s, [2]float64{p.Latitude, p.Longitude}, pilotVisM)
		// Seed last known altitude/gs for more realistic packets (not used by fan-out).
		pilots = append(pilots, s)
	}
	if len(pilots) == 0 {
		t.Fatal("no pilots registered from VATSIM snapshot")
	}

	// Sample Search fan-out over a subset of pilots for context.
	fanSamples := make([]int, 0, 256)
	step := 1
	if len(pilots) > 256 {
		step = len(pilots) / 256
	}
	for i := 0; i < len(pilots); i += step {
		n := 0
		reg.inner.Search(pilots[i], func(*session.Session) bool {
			n++
			return true
		})
		fanSamples = append(fanSamples, n)
	}
	sort.Ints(fanSamples)
	fanP50 := fanSamples[len(fanSamples)/2]
	fanP99 := fanSamples[int(math.Ceil(0.99*float64(len(fanSamples))))-1]
	fanMax := fanSamples[len(fanSamples)-1]
	var fanSum int
	for _, n := range fanSamples {
		fanSum += n
	}
	fanAvg := float64(fanSum) / float64(len(fanSamples))

	t.Logf("vatsim snapshot: update=%s connected=%d unique=%d pilots_registered=%d atc_registered=%d atc_skipped=%d fanout_avg=%.1f fanout_p50=%d fanout_p99=%d fanout_max=%d",
		feed.General.UpdateTimestamp, feed.General.Connected, feed.General.UniqueUsers,
		len(pilots), nATC, skippedATC, fanAvg, fanP50, fanP99, fanMax)

	interval := time.Duration(float64(time.Second) / hz)
	deadline := time.Now().Add(duration)

	var wallSamples []time.Duration
	var wallMu sync.Mutex
	var updates atomic.Int64
	// Per-update recipient counts (enqueues attributed to each handler call).
	var recipSamples []int
	var recipMu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < len(pilots); i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := pilots[idx]
			// Stagger start slightly so tickers don't all fire on the same tick.
			time.Sleep(time.Duration(idx%50) * time.Millisecond)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if time.Now().After(deadline) {
					return
				}
				select {
				case <-ticker.C:
				case <-time.After(time.Until(deadline)):
					return
				}
				lat, lon := s.LatLon()[0], s.LatLon()[1]
				// Nudge position a few meters so UpdatePosition rewrites the index.
				lat += 0.0001
				xpdr := s.Transponder.Load()
				if xpdr == "" {
					xpdr = "1200"
				}
				pkt := []byte(fmt.Sprintf("@S:%s:%s:1:%.5f:%.5f:5000:250:0:0\r\n", s.Callsign, xpdr, lat, lon))

				beforeEnq := reg.enqCount.Load()
				start := time.Now()
				reg.markHandlerStart(s.Callsign, start)
				srv.handlePilotPosition(s, pkt)
				reg.clearHandlerStart(s.Callsign)
				elapsed := time.Since(start)
				recip := int(reg.enqCount.Load() - beforeEnq)

				updates.Add(1)
				wallMu.Lock()
				wallSamples = append(wallSamples, elapsed)
				wallMu.Unlock()
				recipMu.Lock()
				recipSamples = append(recipSamples, recip)
				recipMu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	reg.enqMu.Lock()
	enqCopy := append([]time.Duration(nil), reg.enqSamples...)
	reg.enqMu.Unlock()

	enqP50, enqP99 := percentile(enqCopy, 0.50), percentile(enqCopy, 0.99)
	wallP50, wallP99 := percentile(wallSamples, 0.50), percentile(wallSamples, 0.99)

	recipMu.Lock()
	rs := append([]int(nil), recipSamples...)
	recipMu.Unlock()
	sort.Ints(rs)
	var recipSum int64
	for _, n := range rs {
		recipSum += int64(n)
	}
	recipAvg := 0.0
	if len(rs) > 0 {
		recipAvg = float64(recipSum) / float64(len(rs))
	}
	recipP50, recipP99, recipMax := 0, 0, 0
	if len(rs) > 0 {
		recipP50 = rs[len(rs)/2]
		recipP99 = rs[int(math.Ceil(0.99*float64(len(rs))))-1]
		recipMax = rs[len(rs)-1]
	}

	elapsedWall := duration
	ups := updates.Load()
	upsPerSec := float64(ups) / elapsedWall.Seconds()
	enqPerSec := float64(reg.enqCount.Load()) / elapsedWall.Seconds()

	t.Logf("vatsim live stress: M_pilots=%d M_atc=%d T=%s rate=%.2fHz updates=%d updates/s=%.1f enqueues=%d enqueues/s=%.0f observer_drains=%d recip_avg=%.2f recip_p50=%d recip_p99=%d recip_max=%d enqueue_p50=%s enqueue_p99=%s enqueue_max=%s handler_wall_p50=%s handler_wall_p99=%s handler_wall_max=%s",
		len(pilots), nATC, duration, hz, ups, upsPerSec, reg.enqCount.Load(), enqPerSec, drainCount.Load(),
		recipAvg, recipP50, recipP99, recipMax,
		enqP50, enqP99, maxDuration(enqCopy),
		wallP50, wallP99, maxDuration(wallSamples))

	if ups == 0 {
		t.Fatal("no position updates completed")
	}
	// Worldwide sparse geometry can leave isolated pilots with zero nearby peers;
	// require at least some enqueues across the whole network (fan-out sample already logged).
	if reg.enqCount.Load() == 0 && fanMax == 0 {
		t.Fatal("no broadcast enqueues and zero measured fan-out — registry Search unused?")
	}
	if wallP99 > 5*time.Second {
		t.Fatalf("handler wall p99 pathologically high: %s", wallP99)
	}
}
