// Package metar fetches NOAA METAR observations via a worker pool.
//
// HTTP is injectable (HTTPDoer) so tests never hit the real network.
// ICAO codes are validated (exactly 4 letters) before URL construction
// to mitigate path-injection / SSRF via the stations path segment.
package metar

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// HTTPDoer performs an HTTP request. *http.Client implements this interface.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Service is a METAR fetch worker pool that delivers results via session.Sender.
type Service struct {
	numWorkers int
	http       HTTPDoer
	requests   chan request
}

type request struct {
	ctx      context.Context
	sender   session.Sender
	callsign string
	icaoCode string
}

// New constructs a Service with the given worker count and HTTP client.
// If httpDoer is nil, a default *http.Client is used.
// numWorkers is clamped to at least 1.
func New(numWorkers int, httpDoer HTTPDoer) *Service {
	if numWorkers < 1 {
		numWorkers = 1
	}
	if httpDoer == nil {
		httpDoer = &http.Client{}
	}
	return &Service{
		numWorkers: numWorkers,
		http:       httpDoer,
		requests:   make(chan request, 128),
	}
}

// Run starts the worker pool. It returns immediately; workers exit when ctx is done.
func (s *Service) Run(ctx context.Context) {
	for range s.numWorkers {
		go s.worker(ctx)
	}
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.requests:
			s.handle(req)
		}
	}
}

// Request queues a METAR fetch for icaoCode and delivers the response (or error)
// via sender. It returns immediately once the request is queued or ctx is done.
// callsign is the FSD callsign used in the $AR response packet.
func (s *Service) Request(ctx context.Context, sender session.Sender, callsign, icaoCode string) {
	select {
	case <-ctx.Done():
	case s.requests <- request{
		ctx:      ctx,
		sender:   sender,
		callsign: callsign,
		icaoCode: icaoCode,
	}:
	}
}

func (s *Service) handle(req request) {
	if req.ctx.Err() != nil {
		return
	}

	icao, ok := normalizeICAO(req.icaoCode)
	if !ok {
		sendError(req.sender, req.icaoCode)
		return
	}

	url := buildRequestURL(icao)
	httpReq, err := http.NewRequestWithContext(req.ctx, http.MethodGet, url, nil)
	if err != nil {
		sendError(req.sender, icao)
		return
	}

	res, err := s.http.Do(httpReq)
	if err != nil {
		sendError(req.sender, icao)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		sendError(req.sender, icao)
		return
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		sendError(req.sender, icao)
		return
	}

	metarLine, ok := parseNOAABody(body)
	if !ok {
		slog.Debug("NOAA METAR response was invalid", "icao", icao)
		sendError(req.sender, icao)
		return
	}

	_ = req.sender.Send(buildResponsePacket(req.callsign, metarLine))
}

// normalizeICAO accepts exactly 4 ASCII letters (case-insensitive) and returns
// the uppercased ICAO code. Any other input is rejected (SSRF mitigation).
func normalizeICAO(s string) (string, bool) {
	if len(s) != 4 {
		return "", false
	}
	var b strings.Builder
	b.Grow(4)
	for i := 0; i < 4; i++ {
		c := rune(s[i])
		if c > unicode.MaxASCII || !unicode.IsLetter(c) {
			return "", false
		}
		b.WriteRune(unicode.ToUpper(c))
	}
	return b.String(), true
}

// buildRequestURL builds the NOAA station URL for a validated uppercase ICAO.
func buildRequestURL(icao string) string {
	var url strings.Builder
	url.WriteString("https://tgftp.nws.noaa.gov/data/observations/metar/stations/")
	url.WriteString(icao)
	url.WriteString(".TXT")
	return url.String()
}

// buildResponsePacket formats a $AR METAR response packet.
// Wire shape: $ARSERVER:<callsign>:METAR:<metar>\r\n
func buildResponsePacket(callsign string, metar []byte) string {
	var packet strings.Builder
	packet.Grow(32 + len(callsign) + len(metar))
	packet.WriteString("$ARSERVER:")
	packet.WriteString(callsign)
	packet.WriteString(":METAR:")
	packet.Write(metar)
	packet.WriteString("\r\n")
	return packet.String()
}

// parseNOAABody expects NOAA station file format: timestamp line, METAR line, trailing newline.
// Returns the METAR line including its terminating newline.
func parseNOAABody(body []byte) (metar []byte, ok bool) {
	if bytes.Count(body, []byte("\n")) != 2 {
		return nil, false
	}
	// First line is timestamp
	body = body[bytes.IndexByte(body, '\n')+1:]
	// Second line is METAR and ends with \n
	end := bytes.IndexByte(body, '\n')
	if end < 0 {
		return nil, false
	}
	return body[:end+1], true
}

func sendError(sender session.Sender, icaoCode string) {
	if sender == nil {
		return
	}
	msg := "Error fetching METAR for " + icaoCode
	_ = sender.Send(protocol.FormatError(protocol.NoWeatherProfileError, msg))
}
