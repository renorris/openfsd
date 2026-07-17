package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
)

func TestNew_ZeroCoords(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N123"})
	ll := s.LatLon()
	if ll[0] != 0 || ll[1] != 0 {
		t.Fatalf("LatLon after New = %v, want [0 0]", ll)
	}
	if s.Callsign != "N123" {
		t.Fatalf("Callsign = %q, want N123", s.Callsign)
	}
}

func TestLatLon_SetLatLon(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{})
	s.SetLatLon(33.5, -117.25)
	ll := s.LatLon()
	if ll[0] != 33.5 || ll[1] != -117.25 {
		t.Fatalf("LatLon = %v, want [33.5 -117.25]", ll)
	}
	s.SetLatLon(-10, 170)
	ll = s.LatLon()
	if ll[0] != -10 || ll[1] != 170 {
		t.Fatalf("LatLon after second set = %v, want [-10 170]", ll)
	}
}

func TestSend_DequeueOutbound(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "TEST"})
	if err := s.Send("hello\r\n"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	pkt, ok := s.DequeueOutbound()
	if !ok || pkt != "hello\r\n" {
		t.Fatalf("DequeueOutbound = (%q, %v), want (hello\\r\\n, true)", pkt, ok)
	}
	if _, ok := s.DequeueOutbound(); ok {
		t.Fatal("expected empty outbound queue")
	}
}

func TestSendError_WireFormat(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "TEST"})
	const code = 4
	const msg = "Packet too short"
	if err := s.SendError(code, msg); err != nil {
		t.Fatalf("SendError: %v", err)
	}
	pkt, ok := s.DequeueOutbound()
	if !ok {
		t.Fatal("expected enqueued error packet")
	}
	want := protocol.FormatError(protocol.ErrorCode(code), msg)
	if pkt != want {
		t.Fatalf("SendError wire = %q, want %q", pkt, want)
	}
}

func TestSenderWorker_NilConnDrains(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "TEST"})
	done := make(chan struct{})
	go func() {
		s.SenderWorker()
		close(done)
	}()

	if err := s.Send("a\r\n"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := s.Send("b\r\n"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Cancel should stop the worker after draining or on next select.
	s.Cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SenderWorker did not exit after Cancel with nil Conn")
	}
}

func TestSend_UnblockedByCancel(t *testing.T) {
	// Unbuffered path: fill buffer then cancel so a blocked Send returns ctx err.
	s := New(context.Background(), nil, nil, LoginData{})
	// Fill the 32-buffer.
	for i := 0; i < 32; i++ {
		if err := s.Send("x"); err != nil {
			t.Fatalf("fill Send #%d: %v", i, err)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Send("blocked")
	}()

	// Give the goroutine a moment to block on the full channel.
	time.Sleep(20 * time.Millisecond)
	s.Cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send after cancel: err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send did not unblock after Cancel")
	}
}

func TestSendPosition_LatestWinsWhenFull(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	// Fill the buffer with ordinary sends.
	for i := 0; i < sendChanCap; i++ {
		if err := s.Send("old"); err != nil {
			t.Fatalf("fill: %v", err)
		}
	}
	// Must not block: drops oldest and enqueues fresh position.
	if err := s.SendPosition("fresh\r\n"); err != nil {
		t.Fatalf("SendPosition: %v", err)
	}
	// Drain: first may be "old" or after drop; last successful position should appear.
	var sawFresh bool
	for {
		pkt, ok := s.DequeueOutbound()
		if !ok {
			break
		}
		if pkt == "fresh\r\n" {
			sawFresh = true
		}
	}
	if !sawFresh {
		t.Fatal("expected fresh position to be enqueued after latest-wins drop")
	}
}
