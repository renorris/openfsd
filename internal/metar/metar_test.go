package metar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/session"
)

// recordingSender captures packets sent via session.Sender.
// Callsign() supports the 3-arg Request API without *session.Session.
type recordingSender struct {
	mu       sync.Mutex
	packets  []string
	err      error
	callsign string
}

func (r *recordingSender) Send(packet string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.packets = append(r.packets, packet)
	return r.err
}

func (r *recordingSender) Callsign() string { return r.callsign }

func (r *recordingSender) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.packets))
	copy(out, r.packets)
	return out
}

// fakeDoer is an injectable HTTPDoer for tests (no real NOAA).
type fakeDoer struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}

func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestNormalizeICAO(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"KJFK", "KJFK", true},
		{"egll", "EGLL", true},
		{"KsAn", "KSAN", true},
		{"", "", false},
		{"JFK", "", false},
		{"KJFKX", "", false},
		{"KJ1K", "", false},
		{"KJ F", "", false},
		{"../x", "", false},
		{"ht1p", "", false},
		{"KJ\x00K", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := normalizeICAO(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("normalizeICAO(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestBuildRequestURL(t *testing.T) {
	tests := []struct {
		icao string
		want string
	}{
		{"KJFK", "https://tgftp.nws.noaa.gov/data/observations/metar/stations/KJFK.TXT"},
		{"EGLL", "https://tgftp.nws.noaa.gov/data/observations/metar/stations/EGLL.TXT"},
	}
	for _, tt := range tests {
		if got := buildRequestURL(tt.icao); got != tt.want {
			t.Errorf("buildRequestURL(%q) = %q, want %q", tt.icao, got, tt.want)
		}
	}
}

func TestBuildResponsePacket(t *testing.T) {
	tests := []struct {
		callsign string
		metar    []byte
		want     string
	}{
		{
			callsign: "TEST",
			metar:    []byte("KJFK 301951Z 18010KT 10SM FEW250 29/19 A2992"),
			want:     "$ARSERVER:TEST:METAR:KJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\r\n",
		},
		{
			callsign: "PILOT1",
			metar:    []byte("EGLL 301950Z 24008KT 9999 FEW040 18/12 Q1015"),
			want:     "$ARSERVER:PILOT1:METAR:EGLL 301950Z 24008KT 9999 FEW040 18/12 Q1015\r\n",
		},
	}
	for _, tt := range tests {
		got := buildResponsePacket(tt.callsign, tt.metar)
		if got != tt.want {
			t.Errorf("buildResponsePacket = %q, want %q", got, tt.want)
		}
	}
}

func TestParseNOAABody(t *testing.T) {
	okBody := []byte("2023/04/30 19:51\nKJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\n")
	got, ok := parseNOAABody(okBody)
	if !ok {
		t.Fatal("expected ok")
	}
	want := []byte("KJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\n")
	if !bytes.Equal(got, want) {
		t.Errorf("metar = %q, want %q", got, want)
	}

	if _, ok := parseNOAABody([]byte("one line only\n")); ok {
		t.Error("single newline should fail")
	}
	if _, ok := parseNOAABody([]byte("a\nb\nc\n")); ok {
		t.Error("three lines should fail")
	}
	if _, ok := parseNOAABody(nil); ok {
		t.Error("empty should fail")
	}
}

func TestHandle_Success(t *testing.T) {
	body := "2023/04/30 19:51\nKJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\n"
	var sawURL string
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		sawURL = req.URL.String()
		if req.Context() == nil {
			t.Error("expected request context")
		}
		return okResponse(body), nil
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   sender,
		icaoCode: "kjfk",
	})

	wantURL := "https://tgftp.nws.noaa.gov/data/observations/metar/stations/KJFK.TXT"
	if sawURL != wantURL {
		t.Errorf("URL = %q, want %q", sawURL, wantURL)
	}
	pkts := sender.all()
	if len(pkts) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(pkts))
	}
	if !strings.HasPrefix(pkts[0], "$ARSERVER:TEST:METAR:KJFK ") || !strings.HasSuffix(pkts[0], "\r\n") {
		t.Errorf("bad response packet: %q", pkts[0])
	}
}

func TestHandle_InvalidICAO_NoHTTP(t *testing.T) {
	called := false
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("should not be called")
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}

	for _, icao := range []string{"", "INVALID", "../x", "KJ1K", "http://evil"} {
		sender.packets = nil
		svc.handle(context.Background(), request{
			ctx:      context.Background(),
			sender:   sender,
			icaoCode: icao,
		})
		if called {
			t.Fatalf("HTTP must not run for ICAO %q", icao)
		}
		pkts := sender.all()
		if len(pkts) != 1 {
			t.Fatalf("icao %q: expected 1 error packet, got %d", icao, len(pkts))
		}
		want := "$ERserver:unknown:9::Error fetching METAR for " + icao + "\r\n"
		if pkts[0] != want {
			t.Errorf("icao %q: packet = %q, want %q", icao, pkts[0], want)
		}
	}
}

func TestHandle_HTTPStatusError(t *testing.T) {
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("Not Found")),
		}, nil
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   sender,
		icaoCode: "KJFK",
	})
	want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	pkts := sender.all()
	if len(pkts) != 1 || pkts[0] != want {
		t.Errorf("packets = %v, want %q", pkts, want)
	}
}

func TestHandle_NetworkError(t *testing.T) {
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network error")
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   sender,
		icaoCode: "KJFK",
	})
	want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	pkts := sender.all()
	if len(pkts) != 1 || pkts[0] != want {
		t.Errorf("packets = %v, want %q", pkts, want)
	}
}

func TestHandle_InvalidResponseBody(t *testing.T) {
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return okResponse("Invalid response\n"), nil
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   sender,
		icaoCode: "KJFK",
	})
	want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	pkts := sender.all()
	if len(pkts) != 1 || pkts[0] != want {
		t.Errorf("packets = %v, want %q", pkts, want)
	}
}

func TestHandle_MoreThanTwoLines(t *testing.T) {
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return okResponse("Line1\nLine2\nLine3\n"), nil
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   sender,
		icaoCode: "KJFK",
	})
	want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	pkts := sender.all()
	if len(pkts) != 1 || pkts[0] != want {
		t.Errorf("packets = %v, want %q", pkts, want)
	}
}

func TestHandle_BodyReadError(t *testing.T) {
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&errReader{}),
		}, nil
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   sender,
		icaoCode: "KJFK",
	})
	want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	pkts := sender.all()
	if len(pkts) != 1 || pkts[0] != want {
		t.Errorf("packets = %v, want %q", pkts, want)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

func TestHandle_CancelledSessionContext(t *testing.T) {
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		t.Error("HTTP should not run for cancelled context")
		return nil, errors.New("nope")
	}}
	svc := New(1, doer)
	sender := &recordingSender{callsign: "TEST"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.handle(context.Background(), request{
		ctx:      ctx,
		sender:   sender,
		icaoCode: "KJFK",
	})
	if len(sender.all()) != 0 {
		t.Errorf("expected no packets on cancelled ctx, got %v", sender.all())
	}
}

func TestHandle_RunContextCancelsInFlightDo(t *testing.T) {
	started := make(chan struct{})
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	svc := New(1, doer)
	runCtx, runCancel := context.WithCancel(context.Background())
	svc.Run(runCtx)

	sender := &recordingSender{callsign: "TEST"}
	svc.Request(context.Background(), sender, "KJFK")

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Do never started")
	}
	runCancel()

	deadline := time.After(2 * time.Second)
	for {
		pkts := sender.all()
		if len(pkts) == 1 {
			want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
			if pkts[0] != want {
				t.Errorf("packet = %q, want %q", pkts[0], want)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for cancelled fetch error")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestHandle_NilSenderOnError(t *testing.T) {
	// Should not panic when sender is nil after invalid ICAO.
	svc := New(1, &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("unused")
	}})
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   nil,
		icaoCode: "BAD",
	})
}

func TestHandle_NilSenderOnSuccess(t *testing.T) {
	body := "2023/04/30 19:51\nKJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\n"
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return okResponse(body), nil
	}}
	svc := New(1, doer)
	// Must not panic on nil sender after successful parse.
	svc.handle(context.Background(), request{
		ctx:      context.Background(),
		sender:   nil,
		icaoCode: "KJFK",
	})
}

func TestNew_Defaults(t *testing.T) {
	svc := New(0, nil)
	if svc.numWorkers != 1 {
		t.Errorf("numWorkers = %d, want 1", svc.numWorkers)
	}
	client, ok := svc.http.(*http.Client)
	if !ok {
		t.Fatalf("expected *http.Client, got %T", svc.http)
	}
	if client.Timeout != defaultHTTPTimeout {
		t.Errorf("Timeout = %v, want %v", client.Timeout, defaultHTTPTimeout)
	}
	if client.CheckRedirect == nil {
		t.Fatal("expected CheckRedirect set")
	}
	// Redirect policy must refuse to follow (ErrUseLastResponse).
	if err := client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("CheckRedirect = %v, want ErrUseLastResponse", err)
	}
	if cap(svc.requests) != 128 {
		t.Errorf("queue cap = %d, want 128", cap(svc.requests))
	}
}

func TestRequest_AndWorker_Integration(t *testing.T) {
	body := "2023/04/30 19:51\nEGLL 301950Z 24008KT 9999 FEW040 18/12 Q1015\n"
	doer := &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/EGLL.TXT") {
			t.Errorf("unexpected path %q", req.URL.Path)
		}
		return okResponse(body), nil
	}}
	svc := New(2, doer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Run(ctx)

	// Real session.Session: 3-arg Request resolves callsign from Session.
	sess := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "PILOT1"})
	svc.Request(context.Background(), sess, "egll")

	deadline := time.After(2 * time.Second)
	var pkt string
	for {
		var ok bool
		pkt, ok = sess.DequeueOutbound()
		if ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for METAR response")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if !strings.Contains(pkt, "EGLL 301950Z") || !strings.HasPrefix(pkt, "$ARSERVER:PILOT1:METAR:") {
		t.Errorf("unexpected packet %q", pkt)
	}
}

func TestRequest_CancelledBeforeQueue(t *testing.T) {
	svc := New(1, &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		t.Error("should not fetch")
		return nil, errors.New("nope")
	}})
	// Don't start workers; cancelled ctx should not block forever on full path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sender := &recordingSender{callsign: "TEST"}
	done := make(chan struct{})
	go func() {
		svc.Request(ctx, sender, "KJFK")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Request blocked on cancelled context")
	}
}

// TestRequest_FullQueueDoesNotBlock ensures METAR spam cannot stall the FSD loop.
// Workers are not started so the buffered channel fills; the next Request must
// return immediately and soft-fail with a weather $ER.
func TestRequest_FullQueueDoesNotBlock(t *testing.T) {
	svc := New(1, &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		t.Error("should not fetch when queue full / no workers")
		return nil, errors.New("nope")
	}})
	// Do not call Run — queue never drains.
	fill := cap(svc.requests)
	if fill < 1 {
		t.Fatal("expected buffered requests channel")
	}
	// Fill with dummy senders (invalid ICAO still occupies a queue slot until worker runs).
	for i := 0; i < fill; i++ {
		s := &recordingSender{callsign: "FILL"}
		svc.Request(context.Background(), s, "KJFK")
	}
	sender := &recordingSender{callsign: "PILOT"}
	done := make(chan struct{})
	go func() {
		svc.Request(context.Background(), sender, "KJFK")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Request blocked when queue full")
	}
	// Soft-fail $ER should be enqueued to sender.
	pkts := sender.all()
	if len(pkts) == 0 {
		t.Fatal("expected soft-fail error packet on full queue")
	}
	if !strings.Contains(pkts[0], "$ER") && !strings.Contains(strings.ToLower(pkts[0]), "metar") {
		// FormatError uses $ER prefix
		if !strings.HasPrefix(pkts[0], "$ER") {
			t.Fatalf("unexpected soft-fail packet %q", pkts[0])
		}
	}
}

func TestWorker_ExitsOnContextCancel(t *testing.T) {
	svc := New(1, &fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		return okResponse("a\nb\n"), nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	svc.Run(ctx)
	cancel()
	// Give worker a moment to observe cancel; no assertion race — just ensure no hang.
	time.Sleep(20 * time.Millisecond)
}

func TestSendError_WireFormat(t *testing.T) {
	sender := &recordingSender{}
	sendError(sender, "KJFK")
	want := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	pkts := sender.all()
	if len(pkts) != 1 || pkts[0] != want {
		t.Errorf("got %v, want %q", pkts, want)
	}
}

func TestCallsignFrom(t *testing.T) {
	if got := callsignFrom(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	sess := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "N123"})
	if got := callsignFrom(sess); got != "N123" {
		t.Errorf("session = %q, want N123", got)
	}
	rs := &recordingSender{callsign: "FAKE"}
	if got := callsignFrom(rs); got != "FAKE" {
		t.Errorf("recording = %q, want FAKE", got)
	}
	// Plain sender without Callsign() → empty.
	type plain struct{}
	// Can't implement session.Sender without Send — use minimal:
	ps := plainSender{}
	if got := callsignFrom(ps); got != "" {
		t.Errorf("plain = %q, want empty", got)
	}
}

type plainSender struct{}

func (plainSender) Send(string) error { return nil }
