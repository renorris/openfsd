package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendEnqueueObserver(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "OBS1"})
	var calls atomic.Int32
	var lastDepth int
	s.SetSendEnqueueObserver(func(callsign string, enqueuedAt time.Time, queueDepth int) {
		if callsign != "OBS1" {
			t.Errorf("callsign=%q", callsign)
		}
		if enqueuedAt.IsZero() {
			t.Error("zero time")
		}
		lastDepth = queueDepth
		calls.Add(1)
	})
	if err := s.Send("a\r\n"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	if lastDepth < 1 {
		t.Fatalf("queueDepth=%d", lastDepth)
	}
	// clear
	s.SetSendEnqueueObserver(nil)
	if err := s.Send("b\r\n"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("observer should be cleared, calls=%d", calls.Load())
	}
}
