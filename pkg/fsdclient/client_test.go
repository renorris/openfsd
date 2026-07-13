package fsdclient

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
)

// startStubServer listens on a random port, sends $DI, then runs handler.
func startStubServer(t *testing.T, handler func(net.Conn)) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				// Always send openfsd $DI first.
				_, _ = c.Write([]byte("$DISERVER:CLIENT:openfsd:6f70656e667364\r\n"))
				if handler != nil {
					handler(c)
				} else {
					// Drain until client closes.
					_, _ = io.Copy(io.Discard, c)
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		wg.Wait()
	}
}

func TestDialAndServerIdent(t *testing.T) {
	addr, cleanup := startStubServer(t, nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := Dial(ctx, Config{Addr: addr, DialTimeout: time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close(context.Background())

	ident := c.ServerIdent()
	if ident.Version != "openfsd" {
		t.Errorf("version = %q, want openfsd", ident.Version)
	}
	if ident.ChallengeKey != "6f70656e667364" {
		t.Errorf("challenge = %q", ident.ChallengeKey)
	}

	recs := c.Recorder().Received()
	if len(recs) != 1 {
		t.Fatalf("received count = %d, want 1", len(recs))
	}
	if recs[0].Type != protocol.PacketTypeServerIdent {
		t.Errorf("type = %v", recs[0].Type)
	}
}

func TestDialBadServerIdent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("#TMFOO:BAR:not a di\r\n"))
		time.Sleep(50 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = Dial(ctx, Config{Addr: ln.Addr().String(), DialTimeout: time.Second})
	if err == nil {
		t.Fatal("expected error for bad $DI")
	}
}

func TestDialEmptyAddr(t *testing.T) {
	_, err := Dial(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected empty addr error")
	}
}

func TestLoginPilotPasswordAndJWT(t *testing.T) {
	cases := []struct {
		name  string
		token string
	}{
		{"password", "s3cret"},
		{"jwt", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJkdW1teSI6InRydWUifQ.WC-jYTEoENgaWQ9WUj9A9_-olUELBHmNOwv7UrCVn9w"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			var mu sync.Mutex
			addr, cleanup := startStubServer(t, func(conn net.Conn) {
				sc := bufio.NewScanner(conn)
				for sc.Scan() {
					mu.Lock()
					got = append(got, sc.Text())
					mu.Unlock()
				}
			})
			defer cleanup()

			ctx := context.Background()
			c, err := Dial(ctx, Config{Addr: addr})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close(ctx)

			err = c.LoginPilot(ctx, PilotLogin{
				Callsign:      "N7938C",
				CID:           "100000",
				Token:         tc.token,
				NetworkRating: protocol.NetworkRatingObserver,
				ProtoRevision: 100,
				SimulatorType: 2,
				RealName:      "John Doe",
			})
			if err != nil {
				t.Fatal(err)
			}
			// Allow server to read.
			time.Sleep(30 * time.Millisecond)
			_ = c.Close(ctx)
			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			defer mu.Unlock()
			if len(got) < 1 {
				t.Fatalf("no packets received by server: %v", got)
			}
			// Last or only packet should be #AP
			ap := got[len(got)-1]
			if !strings.HasPrefix(ap, "#APN7938C:SERVER:100000:"+tc.token+":") {
				t.Errorf("add pilot wire = %q", ap)
			}
			if !strings.Contains(ap, ":1:100:2:John Doe") {
				t.Errorf("unexpected #AP fields: %q", ap)
			}
			// Default / explicit proto 100.
			parsed, err := protocol.ParseAddPilot([]byte(ap))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Token != tc.token {
				t.Errorf("token = %q", parsed.Token)
			}
			if parsed.ProtoRevision != 100 {
				t.Errorf("proto = %d", parsed.ProtoRevision)
			}
		})
	}
}

func TestLoginPilotWithClientIdent(t *testing.T) {
	var got []string
	var mu sync.Mutex
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			mu.Lock()
			got = append(got, sc.Text())
			mu.Unlock()
		}
	})
	defer cleanup()

	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	err = c.LoginPilot(ctx, PilotLogin{
		Callsign:      "N172SP",
		CID:           "123456",
		Token:         "pw",
		NetworkRating: protocol.NetworkRatingObserver,
		RealName:      "Test",
		ClientIdent: &protocol.ClientIdent{
			SoftwareID:   "88e4",
			SoftwareName: "vPilot",
			VersionMajor: 3,
			VersionMinor: 8,
			SystemUID:    -582057156,
			ChallengeKey: "6d6973746176",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_ = c.Close(ctx)
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("got %d packets, want $ID + #AP: %v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "$IDN172SP:SERVER:88e4:vPilot:3:8:123456:-582057156:6d6973746176") {
		t.Errorf("$ID = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "#APN172SP:") {
		t.Errorf("#AP = %q", got[1])
	}
	// Proto default 100
	ap, err := protocol.ParseAddPilot([]byte(got[1]))
	if err != nil {
		t.Fatal(err)
	}
	if ap.ProtoRevision != 100 {
		t.Errorf("default proto = %d, want 100", ap.ProtoRevision)
	}
}

func TestLoginATC(t *testing.T) {
	var got []string
	var mu sync.Mutex
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			mu.Lock()
			got = append(got, sc.Text())
			mu.Unlock()
		}
	})
	defer cleanup()

	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	err = c.LoginATC(ctx, ATCLogin{
		Callsign:      "RN_OBS",
		CID:           "100000",
		Token:         "tok",
		NetworkRating: protocol.NetworkRatingStudent1,
		ProtoRevision: 100,
		RealName:      "John Doe",
		ClientIdent: &protocol.ClientIdent{
			SoftwareID:   "88e4",
			SoftwareName: "vPilot",
			VersionMajor: 1,
			VersionMinor: 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_ = c.Close(ctx)
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("packets = %v", got)
	}
	if !strings.HasPrefix(got[0], "$IDRN_OBS:") {
		t.Errorf("$ID = %q", got[0])
	}
	aa, err := protocol.ParseAddATC([]byte(got[1]))
	if err != nil {
		t.Fatal(err)
	}
	if aa.Callsign != "RN_OBS" || aa.Token != "tok" || aa.RealName != "John Doe" {
		t.Errorf("parsed #AA = %+v", aa)
	}
}

func TestSendPilotPositionAndNext(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		// Read one position, then reply with a text message.
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			return
		}
		_, _ = conn.Write([]byte("#TMSERVER:N7938C:hello pilot\r\n"))
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

	pos := protocol.PilotPosition{
		TransponderMode:    "S",
		Callsign:           "GTI8197",
		TransponderCode:    "2000",
		NetworkRating:      protocol.NetworkRatingObserver,
		Latitude:           40.65906,
		Longitude:          -73.79891,
		TrueAltitude:       26,
		Groundspeed:        0,
		PitchBankHeading:   4290776072,
		AltitudeCorrection: 359,
	}
	if err := c.SendPilotPosition(pos); err != nil {
		t.Fatal(err)
	}

	got, err := c.WaitFor(ctx, func(r Received) bool {
		return r.Type == protocol.PacketTypeTextMessage
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got.Raw, []byte("hello pilot")) {
		t.Errorf("got %q", got.Raw)
	}

	sent := c.Recorder().Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
	}
	if sent[0].Type != protocol.PacketTypePilotPosition {
		t.Errorf("sent type = %v", sent[0].Type)
	}
}

func TestDialConnNetPipe(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	go func() {
		_, _ = server.Write([]byte("$DISERVER:CLIENT:VATSIM FSD V3.50:76617473696d\r\n"))
		_, _ = io.Copy(io.Discard, server)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := DialConn(ctx, Config{}, client)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	if c.ServerIdent().Version != "VATSIM FSD V3.50" {
		t.Errorf("version = %q", c.ServerIdent().Version)
	}
	if c.ServerIdent().ChallengeKey != "76617473696d" {
		t.Errorf("key = %q", c.ServerIdent().ChallengeKey)
	}
}

func TestConcurrentClients(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
		}
	})
	defer cleanup()

	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := Dial(ctx, Config{Addr: addr})
			if err != nil {
				errCh <- err
				return
			}
			defer c.Close(ctx)
			cs := "C" + string(rune('A'+i))
			if err := c.LoginPilot(ctx, PilotLogin{
				Callsign: cs,
				CID:      "1",
				Token:    "t",
				RealName: "N",
			}); err != nil {
				errCh <- err
				return
			}
			if err := c.SendPilotPosition(protocol.PilotPosition{
				TransponderMode: "N",
				Callsign:        cs,
				TransponderCode: "1200",
				Latitude:        1,
				Longitude:       2,
			}); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestConcurrentSendOnOneClient(t *testing.T) {
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

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = c.Send([]byte("#TMTEST:SERVER:msg"))
		}(i)
	}
	wg.Wait()
	if c.Recorder().Len() < 21 { // $DI + 20 sends
		t.Errorf("recorder len = %d", c.Recorder().Len())
	}
}

func TestAlreadyLoggedIn(t *testing.T) {
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
	login := PilotLogin{Callsign: "A", CID: "1", Token: "t", RealName: "n"}
	if err := c.LoginPilot(ctx, login); err != nil {
		t.Fatal(err)
	}
	if err := c.LoginPilot(ctx, login); err != ErrAlreadyLoggedIn {
		t.Errorf("err = %v", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	addr, cleanup := startStubServer(t, nil)
	defer cleanup()
	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != ErrClosed {
		t.Errorf("second close = %v", err)
	}
	if err := c.Send([]byte("x")); err != ErrClosed {
		t.Errorf("send after close = %v", err)
	}
}

func TestEnsureCRLF(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"foo", "foo\r\n"},
		{"foo\n", "foo\r\n"},
		{"foo\r\n", "foo\r\n"},
	}
	for _, tc := range cases {
		got := string(ensureCRLF([]byte(tc.in)))
		if got != tc.want {
			t.Errorf("ensureCRLF(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSendATCAndDelete(t *testing.T) {
	var got []string
	var mu sync.Mutex
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			mu.Lock()
			got = append(got, sc.Text())
			mu.Unlock()
		}
	})
	defer cleanup()

	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	if err := c.SendATCPosition(protocol.ATCPosition{
		Callsign:        "EWR_P_APP",
		Frequencies:     "28550",
		FacilityType:    5,
		VisibilityRange: 150,
		NetworkRating:   4,
		Latitude:        40.67317,
		Longitude:       -74.18533,
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.SendDeleteATC("EWR_P_APP", "100000"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendDeletePilot("N7938C", "100000"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_ = c.Close(ctx)
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("got %v", got)
	}
	if !strings.HasPrefix(got[0], "%EWR_P_APP:") {
		t.Errorf("pos = %q", got[0])
	}
	if got[1] != "#DAEWR_P_APP:100000" {
		t.Errorf("da = %q", got[1])
	}
	if got[2] != "#DPN7938C:100000" {
		t.Errorf("dp = %q", got[2])
	}
}

func TestProto101Helpers(t *testing.T) {
	var got []string
	var mu sync.Mutex
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			mu.Lock()
			got = append(got, sc.Text())
			mu.Unlock()
		}
	})
	defer cleanup()

	ctx := context.Background()
	c, err := Dial(ctx, Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)

	if err := c.SendFastPosition(FastPilotPosition{
		Callsign:         "DAL1151",
		Latitude:         40.6354992,
		Longitude:        -73.7795597,
		TrueAltitude:     16.81,
		AltitudeAGL:      8.10,
		PitchBankHeading: 12582828,
		VelX:             0.0015,
		VelY:             0.0001,
		VelZ:             0.0005,
		RotX:             0.0001,
		RotY:             0,
		RotZ:             -0.0029,
		NoseGearAngle:    -0.40,
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.SendSlowFastPosition([]byte("PRM4211:41.0:-73.0:1:1:0:0:0:0:0:0:0:0")); err != nil {
		t.Fatal(err)
	}
	if err := c.SendStoppedPosition([]byte("#STDAL2119:40.6:-73.7:13.56:-0.03:29360076:0.00")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_ = c.Close(ctx)
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("got %v", got)
	}
	if !strings.HasPrefix(got[0], "^DAL1151:") {
		t.Errorf("fast = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "#SLPRM4211:") {
		t.Errorf("slow = %q", got[1])
	}
	if !strings.HasPrefix(got[2], "#STDAL2119:") {
		t.Errorf("stopped = %q", got[2])
	}
}

func TestNextContextCancel(t *testing.T) {
	addr, cleanup := startStubServer(t, func(conn net.Conn) {
		// Never write after $DI; block on read.
		_, _ = io.Copy(io.Discard, conn)
	})
	defer cleanup()

	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.Next(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestLoginEmptyCallsign(t *testing.T) {
	addr, cleanup := startStubServer(t, nil)
	defer cleanup()
	c, err := Dial(context.Background(), Config{Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(context.Background())
	if err := c.LoginPilot(context.Background(), PilotLogin{}); err == nil {
		t.Fatal("expected empty callsign error")
	}
	if err := c.LoginATC(context.Background(), ATCLogin{}); err == nil {
		t.Fatal("expected empty callsign error")
	}
}
