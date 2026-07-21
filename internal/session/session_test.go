package session

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
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
	if s.Synthetic {
		t.Fatal("Synthetic must default false for normal clients")
	}
}

func TestSecondaryVisCenters_AndVisBoxesOverlap(t *testing.T) {
	const rangeM = 40 * 1852 // 40 NM
	atc := New(context.Background(), nil, nil, LoginData{Callsign: "CTR", IsAtc: true})
	atc.SetGeo(34.0, -118.0, rangeM)
	pilot := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	pilot.SetGeo(36.0, -118.0, 50*1852)

	if VisBoxesOverlap(atc, pilot) {
		t.Fatal("primary-only should not overlap at 2° separation with 40 NM range")
	}
	if !atc.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("SetSecondaryVisCenter")
	}
	if atc.SecondaryVisCenterCount() != 1 {
		t.Fatalf("count=%d", atc.SecondaryVisCenterCount())
	}
	if !VisBoxesOverlap(atc, pilot) {
		t.Fatal("expected overlap via secondary center")
	}
	if !VisBoxesOverlap(pilot, atc) {
		t.Fatal("overlap must be symmetric")
	}

	// Range change recomputes secondary boxes. Shrink both ranges so the
	// secondary slot at 36° no longer reaches a distant pilot.
	atc.SetVisRange(100) // 100 m secondary box at 36.0
	pilot.SetGeo(40.0, -118.0, 100)
	if VisBoxesOverlap(atc, pilot) {
		t.Fatal("tiny boxes far apart should not overlap")
	}
	atc.SetVisRange(rangeM)
	pilot.SetGeo(36.0, -118.0, 50*1852)
	if !VisBoxesOverlap(atc, pilot) {
		t.Fatal("restored range should overlap again")
	}

	if atc.SetSecondaryVisCenter(MaxSecondaryVisCenters, 0, 0) {
		t.Fatal("out-of-range index must fail")
	}
	atc.ClearSecondaryVisCenters()
	if atc.SecondaryVisCenterCount() != 0 || VisBoxesOverlap(atc, pilot) {
		t.Fatal("clear must drop secondaries")
	}
}

func TestRemoteIP_NilSafe(t *testing.T) {
	if got := (*Session)(nil).RemoteIP(); got != "" {
		t.Fatalf("nil session RemoteIP = %q, want \"\"", got)
	}
	s := New(context.Background(), nil, nil, LoginData{Callsign: "SYN"})
	s.Synthetic = true
	if got := s.RemoteIP(); got != "" {
		t.Fatalf("nil Conn RemoteIP = %q, want \"\"", got)
	}

	// net.Pipe yields a real Conn; RemoteIP must not panic.
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	s2 := New(context.Background(), c1, nil, LoginData{Callsign: "REAL"})
	ip := s2.RemoteIP()
	if ip == "" {
		t.Fatal("expected non-empty RemoteIP from net.Pipe Conn")
	}
	_ = strings.TrimSpace(ip) // ensure host string is usable
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

func TestSendPosition_EmptyBuffer(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	if err := s.SendPosition("p1\r\n"); err != nil {
		t.Fatalf("SendPosition: %v", err)
	}
	pkt, ok := s.DequeueOutbound()
	if !ok || pkt != "p1\r\n" {
		t.Fatalf("DequeueOutbound = (%q, %v)", pkt, ok)
	}
}

func TestSendPosition_CancelReturnsError(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	s.Cancel()
	err := s.SendPosition("x\r\n")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendPosition after cancel: %v, want context.Canceled", err)
	}
}

func TestSendPosition_SuccessiveLatestWins(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	// Fill with padding.
	for i := 0; i < sendChanCap; i++ {
		if err := s.Send("pad"); err != nil {
			t.Fatal(err)
		}
	}
	// Multiple position updates while full: each drops oldest and enqueues.
	for i := 0; i < 5; i++ {
		pkt := "pos" + string(rune('A'+i)) + "\r\n"
		if err := s.SendPosition(pkt); err != nil {
			t.Fatalf("SendPosition #%d: %v", i, err)
		}
	}
	// The most recent position must be present after drain.
	var last string
	var sawE bool
	for {
		pkt, ok := s.DequeueOutbound()
		if !ok {
			break
		}
		last = pkt
		if pkt == "posE\r\n" {
			sawE = true
		}
	}
	if !sawE {
		t.Fatalf("expected final position posE in queue; last=%q", last)
	}
}

func TestSendPosition_ConcurrentProducers(t *testing.T) {
	// Concurrent SendPosition must not block, panic, or leave the session unusable.
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	const producers = 16
	const per = 64
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				_ = s.SendPosition("x\r\n")
			}
		}(p)
	}
	wg.Wait()

	// Session still accepts traffic after the storm.
	if err := s.SendPosition("final\r\n"); err != nil {
		t.Fatalf("post-storm SendPosition: %v", err)
	}
	// Drain whatever remains; must not hang.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("drain hung")
		default:
		}
		if _, ok := s.DequeueOutbound(); !ok {
			break
		}
	}
}

func TestSendPosition_DoesNotBlockWhenFull(t *testing.T) {
	s := New(context.Background(), nil, nil, LoginData{Callsign: "N1"})
	for i := 0; i < sendChanCap; i++ {
		if err := s.Send("old"); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- s.SendPosition("fresh\r\n")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SendPosition: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendPosition blocked on full channel")
	}
}
