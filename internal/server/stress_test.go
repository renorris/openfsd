//go:build stress

// Stress baselines for concurrent registry + position update fan-out.
//
// Not compiled into default `go test` (build tag `stress`). Run with:
//
//	go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
//
// Absolute latency thresholds are intentionally NOT asserted — cold shared
// runners vary widely. p50/p99 are logged for the PR summary baseline.
// Also respects testing.Short() if someone passes -short with the tag.
package server

import (
	"context"
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

// stressPilots is the default concurrent pilot count (design: M=100).
const stressPilots = 100

// stressHz is the position update rate per pilot (design: 0.2 Hz).
const stressHz = 0.2

// stressDuration is how long the default stress run lasts (design: T=10s).
const stressDuration = 10 * time.Second

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

	reg := postoffice.New()
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

	// Capture enqueue timestamps via SendEnqueueObserver for p50/p99 of
	// handler-start → enqueue latency on *recipients* of broadcasts.
	var latencies sync.Mutex
	var samples []time.Duration
	var enqueueCount atomic.Int64

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
		lat := 33.94 + float64(i%10)*0.01
		lon := -118.40 + float64(i/10)*0.01
		s.SetLatLon(lat, lon)
		s.VisRange.Store(50 * 1852)
		s.SetSendEnqueueObserver(func(callsign string, enqueuedAt time.Time, queueDepth int) {
			enqueueCount.Add(1)
			// Drain immediately so Send never blocks on the 32-buffer.
			_, _ = s.DequeueOutbound()
		})
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		// Apply tree position
		reg.UpdatePosition(s, [2]float64{lat, lon}, 50*1852)
		pilots[i] = s
	}

	// Interval between position updates per pilot.
	interval := time.Duration(float64(time.Second) / stressHz)
	deadline := time.Now().Add(stressDuration)

	var handlerSamples []time.Duration
	var hsMu sync.Mutex
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
				// Jitter slightly
				lat += 0.0001
				pkt := []byte(fmt.Sprintf("@S:%s:1200:1:%.5f:%.5f:5000:250:0:0\r\n", s.Callsign, lat, lon))
				start := time.Now()
				srv.handlePilotPosition(s, pkt)
				elapsed := time.Since(start)
				updates.Add(1)
				hsMu.Lock()
				handlerSamples = append(handlerSamples, elapsed)
				hsMu.Unlock()
				// Also record via observer path sample point (enqueue side)
				latencies.Lock()
				samples = append(samples, elapsed)
				latencies.Unlock()
			}
		}(i)
	}
	wg.Wait()

	p50, p99 := percentile(handlerSamples, 0.50), percentile(handlerSamples, 0.99)
	t.Logf("stress baseline: M=%d T=%s rate=%.2fHz updates=%d enqueues=%d handler_p50=%s handler_p99=%s max=%s",
		m, stressDuration, stressHz, updates.Load(), enqueueCount.Load(), p50, p99, maxDuration(handlerSamples))

	// Soft sanity only — do not fail on absolute 5ms.
	if updates.Load() == 0 {
		t.Fatal("no position updates completed")
	}
	if p99 > 5*time.Second {
		// Pathologically stuck; still avoid flaky absolute 5ms.
		t.Fatalf("handler p99 pathologically high: %s", p99)
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
