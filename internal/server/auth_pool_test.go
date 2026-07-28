package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTryAuthPool_ConcurrentWithShutdown ensures close/send on authPool cannot race.
// Reproduces the CI failure: shutdownCluster closechan vs beginLogin send.
func TestTryAuthPool_ConcurrentWithShutdown(t *testing.T) {
	authCh := make(chan func(), 256)
	s := &Server{
		authPool: authCh,
	}
	s.authPoolWG.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer s.authPoolWG.Done()
			// Range the captured channel (mirrors New) so s.authPool = nil cannot race.
			for fn := range authCh {
				if fn != nil {
					fn()
				}
			}
		}()
	}

	var enqueued atomic.Int64
	var closedHits atomic.Int64
	var fullHits atomic.Int64
	var ran atomic.Int64

	const producers = 8
	const perProducer = 200
	var wg sync.WaitGroup
	wg.Add(producers)
	for p := 0; p < producers; p++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				ok, shut := s.tryAuthPool(func() { ran.Add(1) })
				switch {
				case ok:
					enqueued.Add(1)
				case shut:
					closedHits.Add(1)
				default:
					fullHits.Add(1)
				}
			}
		}()
	}

	// Let some work land, then shut down while producers still send.
	time.Sleep(2 * time.Millisecond)
	s.shutdownCluster()
	wg.Wait()

	if enqueued.Load()+closedHits.Load()+fullHits.Load() != producers*perProducer {
		t.Fatalf("counts: enqueued=%d closed=%d full=%d", enqueued.Load(), closedHits.Load(), fullHits.Load())
	}
	// After shutdown, further tries must report closed.
	ok, shut := s.tryAuthPool(func() {})
	if ok || !shut {
		t.Fatalf("after shutdown: enqueued=%v closed=%v want enqueued=false closed=true", ok, shut)
	}
	// Workers should have drained and exited.
	done := make(chan struct{})
	go func() {
		s.authPoolWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("auth pool workers did not exit after shutdown")
	}
}

func TestTryAuthPool_FullReturnsNotClosed(t *testing.T) {
	s := &Server{
		authPool: make(chan func(), 2),
	}
	// No workers — fill buffer.
	if ok, shut := s.tryAuthPool(func() {}); !ok || shut {
		t.Fatalf("first: ok=%v shut=%v", ok, shut)
	}
	if ok, shut := s.tryAuthPool(func() {}); !ok || shut {
		t.Fatalf("second: ok=%v shut=%v", ok, shut)
	}
	ok, shut := s.tryAuthPool(func() {})
	if ok || shut {
		t.Fatalf("full: ok=%v shut=%v want false,false", ok, shut)
	}
	// Drain without closing via shutdown path: close under lock.
	s.authMu.Lock()
	close(s.authPool)
	s.authPool = nil
	s.authMu.Unlock()
}
