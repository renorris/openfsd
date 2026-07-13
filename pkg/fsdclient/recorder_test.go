package fsdclient

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
)

func TestRecorderWaitFor(t *testing.T) {
	clock := NewManualClock(time.Unix(1000, 0))
	r := newRecorder(clock)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	var got Received
	go func() {
		var err error
		got, err = r.WaitFor(ctx, func(rec Received) bool {
			return rec.Type == protocol.PacketTypeTextMessage
		})
		errCh <- err
	}()

	// Unrelated packet first.
	time.Sleep(20 * time.Millisecond)
	clock.Advance(time.Second)
	r.record(DirReceived, []byte("$DISERVER:CLIENT:openfsd:abc\r\n"))
	clock.Advance(time.Second)
	r.record(DirReceived, []byte("#TMSERVER:CLIENT:hello\r\n"))

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got.Type != protocol.PacketTypeTextMessage {
		t.Errorf("type = %v", got.Type)
	}
	if got.At.Unix() != 1002 {
		t.Errorf("at = %v", got.At)
	}
}

func TestRecorderWaitForContextCancel(t *testing.T) {
	r := newRecorder(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := r.WaitFor(ctx, func(Received) bool { return false })
	if err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestRecorderWaitForAlreadyRecorded(t *testing.T) {
	r := newRecorder(RealClock{})
	r.record(DirReceived, []byte("#TMSERVER:C:x\r\n"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := r.WaitFor(ctx, func(rec Received) bool {
		return rec.Type == protocol.PacketTypeTextMessage
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != protocol.PacketTypeTextMessage {
		t.Fatal(got.Type)
	}
}

func TestRecorderWaitForNilPred(t *testing.T) {
	r := newRecorder(nil)
	_, err := r.WaitFor(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRecorderFiltersAndClose(t *testing.T) {
	r := newRecorder(nil)
	r.record(DirSent, []byte("@S:A:1200:1:0:0:0:0:0:0\r\n"))
	r.record(DirReceived, []byte("#TMSERVER:A:hi\r\n"))
	if len(r.Sent()) != 1 || len(r.Received()) != 1 {
		t.Fatalf("sent=%d recv=%d", len(r.Sent()), len(r.Received()))
	}
	if r.Len() != 2 {
		t.Fatal(r.Len())
	}
	r.close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := r.WaitFor(ctx, func(Received) bool { return true })
	// Already has received matching true — WaitFor succeeds before closed check
	// if a match exists. Use a never-match predicate after close with empty match.
	r2 := newRecorder(nil)
	r2.close()
	_, err = r2.WaitFor(ctx, func(Received) bool { return false })
	if err == nil {
		t.Fatal("expected closed error")
	}
}

func TestClientWaitForWithPump(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		// After $DI, wait for any client data then send two messages.
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte("#TMSERVER:C:one\r\n"))
		_, _ = conn.Write([]byte("#TMSERVER:C:two\r\n"))
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	// Kick the server.
	if err := c.Send([]byte("#TMC:SERVER:ping")); err != nil {
		t.Fatal(err)
	}

	// Background pump into recorder for Recorder.WaitFor.
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()
	go func() {
		for {
			if _, err := c.Next(pumpCtx); err != nil {
				return
			}
		}
	}()

	got, err := c.Recorder().WaitFor(ctx, func(r Received) bool {
		return r.Type == protocol.PacketTypeTextMessage &&
			bytesContains(r.Raw, []byte("two"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytesContains(got.Raw, []byte("two")) {
		t.Errorf("got %q", got.Raw)
	}
}

func bytesContains(b, sub []byte) bool {
	return bytes.Contains(b, sub)
}
