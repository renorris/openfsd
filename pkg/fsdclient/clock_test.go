package fsdclient

import (
	"sync"
	"testing"
	"time"
)

func TestManualClock(t *testing.T) {
	start := time.Unix(42, 0).UTC()
	c := NewManualClock(start)
	if !c.Now().Equal(start) {
		t.Fatal(c.Now())
	}
	c.Advance(3 * time.Second)
	if c.Now().Unix() != 45 {
		t.Fatal(c.Now())
	}
	c.Set(time.Unix(100, 0))
	if c.Now().Unix() != 100 {
		t.Fatal(c.Now())
	}
}

func TestResolveClock(t *testing.T) {
	if _, ok := resolveClock(nil).(RealClock); !ok {
		t.Fatal("nil should be RealClock")
	}
	m := NewManualClock(time.Now())
	if resolveClock(m) != m {
		t.Fatal("manual passthrough")
	}
	// RealClock.Now should be close to wall time.
	n := RealClock{}.Now()
	if time.Since(n) > time.Second || time.Since(n) < -time.Second {
		t.Fatal(n)
	}
}

func TestManualClockConcurrent(t *testing.T) {
	c := NewManualClock(time.Unix(0, 0))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Advance(time.Millisecond)
				_ = c.Now()
				c.Set(time.Unix(int64(j), 0))
			}
		}()
	}
	wg.Wait()
}
