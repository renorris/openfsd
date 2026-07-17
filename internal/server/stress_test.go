//go:build stress

// Stress baselines for concurrent registry + position update fan-out.
//
// Not compiled into default `go test` (build tag `stress`). Run with:
//
//	go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
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
		// Do not SetLatLon before UpdatePosition — identical old/new bbox skips the tree rewrite.
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
