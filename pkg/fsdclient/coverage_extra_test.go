package fsdclient

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
)

func TestCallsignAndRecorderAll(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()

	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr, ReadTimeout: time.Second, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	if c.Callsign() != "" {
		t.Fatalf("callsign before login = %q", c.Callsign())
	}
	if err := c.LoginPilot(ctx, PilotLogin{
		Callsign: "CS1",
		CID:      "9",
		Token:    "t",
		RealName: "n",
	}); err != nil {
		t.Fatal(err)
	}
	if c.Callsign() != "CS1" {
		t.Fatalf("callsign = %q", c.Callsign())
	}
	all := c.Recorder().All()
	if len(all) < 2 { // $DI + #AP
		t.Fatalf("all = %d", len(all))
	}
}

func TestChallengeObfuscationBranches(t *testing.T) {
	// 35044 % 3 == 1, 35044 & 1 == 0
	// 24515 % 3 == 2, 24515 & 1 == 1  (swap challenge halves)
	// 56862 % 3 == 0, 56862 & 1 == 0
	for _, id := range []uint16{35044, 24515, 56862} {
		var s ChallengeState
		if err := s.Initialize(id, []byte("0123456789abcdef")); err != nil {
			t.Fatalf("id %d: %v", id, err)
		}
		res := s.ResponseForChallenge([]byte("aabbccddeeff0011"))
		if len(res) != 32 {
			t.Fatalf("id %d len %d", id, len(res))
		}
	}
}

func TestDialConnNil(t *testing.T) {
	_, err := DialConn(context.Background(), Config{}, nil)
	if err == nil {
		t.Fatal("expected nil conn error")
	}
}

func TestDialCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Dial(ctx, Config{Addr: "127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected canceled error")
	}
}

func TestLoginATCAlreadyAndCanceled(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()
	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := c.LoginATC(canceled, ATCLogin{Callsign: "A", CID: "1", Token: "t"}); err == nil {
		t.Fatal("expected canceled")
	}

	if err := c.LoginATC(ctx, ATCLogin{Callsign: "A", CID: "1", Token: "t", RealName: "n"}); err != nil {
		t.Fatal(err)
	}
	if c.Callsign() != "A" {
		t.Fatal(c.Callsign())
	}
	if err := c.LoginATC(ctx, ATCLogin{Callsign: "B", CID: "1", Token: "t"}); err != ErrAlreadyLoggedIn {
		t.Fatal(err)
	}
}

func TestLoginPilotCanceledAndClosed(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()
	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.LoginPilot(ctx, PilotLogin{Callsign: "X", CID: "1", Token: "t"}); err == nil {
		t.Fatal("expected cancel")
	}
	_ = c.Close(context.Background())
	if err := c.LoginPilot(context.Background(), PilotLogin{Callsign: "X", CID: "1", Token: "t"}); err != ErrClosed {
		t.Fatal(err)
	}
	if err := c.LoginATC(context.Background(), ATCLogin{Callsign: "X", CID: "1", Token: "t"}); err != ErrClosed {
		t.Fatal(err)
	}
}

func TestNextAfterCloseAndWaitForNil(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()
	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.WaitFor(context.Background(), nil)
	if err == nil {
		t.Fatal("nil pred")
	}
	_ = c.Close(context.Background())
	_, err = c.Next(context.Background())
	if err != ErrClosed {
		t.Fatal(err)
	}
}

func TestReadServerIdentEdgeCases(t *testing.T) {
	// Wrong field layout: $DI but not SERVER:CLIENT
	{
		server, client := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = server.Write([]byte("$DIFOO:BAR:v:key\r\n"))
			_ = server.Close()
		}()
		_, err := DialConn(context.Background(), Config{}, client)
		if err == nil {
			t.Fatal("expected bad server ident")
		}
		_ = client.Close()
		<-done
	}

	// Too few fields
	{
		server, client := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = server.Write([]byte("$DISERVER:CLIENT:onlyversion\r\n"))
			_ = server.Close()
		}()
		_, err := DialConn(context.Background(), Config{}, client)
		if err == nil {
			t.Fatal("expected parse failure")
		}
		_ = client.Close()
		<-done
	}
}

func TestSendChallengeResponseErrorPath(t *testing.T) {
	// Closed client should fail SendChallengeResponse
	addr, cleanup := startStubServer(t, nil)
	defer cleanup()
	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close(context.Background())
	var st ChallengeState
	_ = st.Initialize(35044, []byte("ab"))
	if err := c.SendChallengeResponse(&st, "A", "B", []byte("0123456789abcdef")); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsurePrefixAlreadyPrefixed(t *testing.T) {
	p := ensurePrefix([]byte("#SLfoo:bar\r\n"), "#SL")
	if string(p) != "#SLfoo:bar\r\n" {
		t.Fatal(string(p))
	}
	// with only \n
	p = ensurePrefix([]byte("#SLfoo\n"), "#SL")
	if string(ensureCRLF(p)) != "#SLfoo\r\n" && string(p) != "#SLfoo\n" {
		// ensurePrefix returns original when prefix matches
		if string(p) != "#SLfoo\n" {
			t.Fatal(string(p))
		}
	}
}

func TestCloseWithCanceledContext(t *testing.T) {
	addr, cleanup := startStubServer(t, nil)
	defer cleanup()
	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Still closes successfully.
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNotDialedMethods(t *testing.T) {
	c := &Client{rec: newRecorder(nil)}
	if err := c.Send([]byte("x")); err != ErrNotDialed {
		t.Fatal(err)
	}
	if _, err := c.Next(context.Background()); err != ErrNotDialed {
		t.Fatal(err)
	}
	if err := c.LoginPilot(context.Background(), PilotLogin{Callsign: "A"}); err != ErrNotDialed {
		t.Fatal(err)
	}
	if err := c.LoginATC(context.Background(), ATCLogin{Callsign: "A"}); err != ErrNotDialed {
		t.Fatal(err)
	}
}

func TestATCLoginDefaultProto(t *testing.T) {
	// Covered indirectly; assert marshal via LoginATC without ClientIdent.
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()
	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	if err := c.LoginATC(context.Background(), ATCLogin{
		Callsign: "Z_TWR",
		CID:      "2",
		Token:    "jwt-or-pw",
		RealName: "Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Default proto 100 should appear in sent #AA
	sent := c.Recorder().Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d", len(sent))
	}
	aa, err := protocol.ParseAddATC(sent[0].Raw)
	if err != nil {
		t.Fatal(err)
	}
	if aa.ProtoRevision != 100 {
		t.Fatal(aa.ProtoRevision)
	}
}
