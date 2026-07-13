package fsdclient

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormatAuthPackets(t *testing.T) {
	zc := string(FormatAuthChallenge("N7938C", "SERVER", "0123456789abcdef"))
	if zc != "$ZCN7938C:SERVER:0123456789abcdef\r\n" {
		t.Errorf("zc = %q", zc)
	}
	zr := string(FormatAuthResponse("SERVER", "N7938C", "4d87482917dc9c2395bd2df151835734"))
	if zr != "$ZRSERVER:N7938C:4d87482917dc9c2395bd2df151835734\r\n" {
		t.Errorf("zr = %q", zr)
	}
}

func TestChallengeStateRoundTrip(t *testing.T) {
	// vPilot client id 35044; initial challenge from openfsd $DI key ascii.
	var s ChallengeState
	if err := s.Initialize(35044, []byte("6f70656e667364")); err != nil {
		t.Fatal(err)
	}
	res1 := s.ResponseForChallenge([]byte("0123456789abcdef"))
	if len(res1) != 32 {
		t.Fatalf("len = %d", len(res1))
	}
	// Hex digits only.
	for _, c := range res1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex in %q", res1)
		}
	}
	s.UpdateState(&res1)
	res2 := s.ResponseForChallenge([]byte("fedcba9876543210"))
	if res2 == res1 {
		t.Error("expected state change to alter subsequent responses")
	}
}

func TestChallengeStateUnsupported(t *testing.T) {
	var s ChallengeState
	if err := s.Initialize(1, []byte("ab")); err != ErrUnsupportedClient {
		t.Errorf("err = %v", err)
	}
}

func TestSendAuthPrecomputedAndComputed(t *testing.T) {
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

	if err := c.SendAuthChallenge("N1", "SERVER", "aabb"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendAuthResponse("N1", "SERVER", "deadbeef"); err != nil {
		t.Fatal(err)
	}

	var st ChallengeState
	if err := st.Initialize(35044, []byte("6f70656e667364")); err != nil {
		t.Fatal(err)
	}
	if err := c.SendChallengeResponse(&st, "N1", "SERVER", []byte("0123456789abcdef")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(40 * time.Millisecond)
	_ = c.Close(ctx)
	// Drain
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("got %v", got)
	}
	if !strings.HasPrefix(got[0], "$ZCN1:SERVER:aabb") {
		t.Errorf("zc = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "$ZRN1:SERVER:deadbeef") {
		t.Errorf("zr pre = %q", got[1])
	}
	if !strings.HasPrefix(got[2], "$ZRN1:SERVER:") || len(got[2]) < 20 {
		t.Errorf("zr computed = %q", got[2])
	}
}
