package fsd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/session"
)

// newMockSession creates a session with a buffered outbound channel for tests.
func newMockSession(callsign string) *session.Session {
	return session.New(context.Background(), nil, nil, session.LoginData{Callsign: callsign})
}

// collectPackets drains the outbound send channel and returns all sent packets.
func collectPackets(s *session.Session) []string {
	var packets []string
	for {
		pkt, ok := s.DequeueOutbound()
		if !ok {
			return packets
		}
		packets = append(packets, pkt)
	}
}

// mockTransport simulates HTTP responses for testing handleMetarRequest.
type mockTransport struct {
	response *http.Response
	err      error
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.response, t.err
}

// TestBuildMetarRequestURL verifies that buildMetarRequestURL correctly formats URLs for given ICAO codes.
func TestBuildMetarRequestURL(t *testing.T) {
	tests := []struct {
		name     string
		icaoCode string
		expected string
	}{
		{
			name:     "Valid ICAO KJFK",
			icaoCode: "KJFK",
			expected: "https://tgftp.nws.noaa.gov/data/observations/metar/stations/KJFK.TXT",
		},
		{
			name:     "Valid ICAO EGLL",
			icaoCode: "EGLL",
			expected: "https://tgftp.nws.noaa.gov/data/observations/metar/stations/EGLL.TXT",
		},
		{
			name:     "Empty ICAO",
			icaoCode: "",
			expected: "https://tgftp.nws.noaa.gov/data/observations/metar/stations/.TXT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMetarRequestURL(tt.icaoCode)
			if got != tt.expected {
				t.Errorf("buildMetarRequestURL(%q) = %q, want %q", tt.icaoCode, got, tt.expected)
			}
		})
	}
}

// TestBuildMetarResponsePacket verifies that buildMetarResponsePacket correctly formats METAR response packets.
func TestBuildMetarResponsePacket(t *testing.T) {
	tests := []struct {
		name     string
		callsign string
		metar    []byte
		expected string
	}{
		{
			name:     "Valid METAR for KJFK",
			callsign: "TEST",
			metar:    []byte("KJFK 301951Z 18010KT 10SM FEW250 29/19 A2992"),
			expected: "$ARSERVER:TEST:METAR:KJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\r\n",
		},
		{
			name:     "Valid METAR for EGLL",
			callsign: "PILOT1",
			metar:    []byte("EGLL 301950Z 24008KT 9999 FEW040 18/12 Q1015"),
			expected: "$ARSERVER:PILOT1:METAR:EGLL 301950Z 24008KT 9999 FEW040 18/12 Q1015\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMetarResponsePacket(tt.callsign, tt.metar)
			if got != tt.expected {
				t.Errorf("buildMetarResponsePacket(%q, %q) = %q, want %q", tt.callsign, tt.metar, got, tt.expected)
			}
		})
	}
}

// TestSendMetarServiceError verifies that sendMetarServiceError sends the correct error packet to the client.
func TestSendMetarServiceError(t *testing.T) {
	mock := newMockSession("TEST")
	req := &metarRequest{
		client:   mock,
		icaoCode: "KJFK",
	}
	sendMetarServiceError(req)

	packets := collectPackets(mock)
	expectedPacket := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	if len(packets) != 1 {
		t.Errorf("expected 1 packet sent, got %d", len(packets))
	} else if packets[0] != expectedPacket {
		t.Errorf("expected packet %q, got %q", expectedPacket, packets[0])
	}
}

// TestHandleMetarRequest_Success verifies that handleMetarRequest correctly processes a valid METAR response.
func TestHandleMetarRequest_Success(t *testing.T) {
	responseBody := []byte("2023/04/30 19:51\nKJFK 301951Z 18010KT 10SM FEW250 29/19 A2992\n")
	mockResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}
	mockTransport := &mockTransport{response: mockResponse}

	service := &metarService{
		httpClient: &http.Client{Transport: mockTransport},
	}

	mock := newMockSession("TEST")
	req := &metarRequest{
		client:   mock,
		icaoCode: "KJFK",
	}

	service.handleMetarRequest(req)

	packets := collectPackets(mock)
	if len(packets) != 1 {
		t.Errorf("expected 1 packet sent, got %d", len(packets))
	}

	if !strings.HasPrefix(packets[0], "$ARSERVER:TEST:METAR:KJFK ") || !strings.HasSuffix(packets[0], "\r\n") {
		t.Errorf("bad response packet")
	}
}

// TestHandleMetarRequest_HTTPError verifies that handleMetarRequest handles HTTP errors correctly.
func TestHandleMetarRequest_HTTPError(t *testing.T) {
	mockResponse := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("Not Found")),
	}
	mockTransport := &mockTransport{response: mockResponse}

	service := &metarService{
		httpClient: &http.Client{Transport: mockTransport},
	}

	mock := newMockSession("TEST")
	req := &metarRequest{
		client:   mock,
		icaoCode: "INVALID",
	}

	service.handleMetarRequest(req)

	packets := collectPackets(mock)
	expectedPacket := "$ERserver:unknown:9::Error fetching METAR for INVALID\r\n"
	if len(packets) != 1 {
		t.Errorf("expected 1 packet sent, got %d", len(packets))
	} else if packets[0] != expectedPacket {
		t.Errorf("expected packet %q, got %q", expectedPacket, packets[0])
	}
}

// TestHandleMetarRequest_NetworkError verifies that handleMetarRequest handles network errors correctly.
func TestHandleMetarRequest_NetworkError(t *testing.T) {
	mockTransport := &mockTransport{err: errors.New("network error")}

	service := &metarService{
		httpClient: &http.Client{Transport: mockTransport},
	}

	mock := newMockSession("TEST")
	req := &metarRequest{
		client:   mock,
		icaoCode: "KJFK",
	}

	service.handleMetarRequest(req)

	packets := collectPackets(mock)
	expectedPacket := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	if len(packets) != 1 {
		t.Errorf("expected 1 packet sent, got %d", len(packets))
	} else if packets[0] != expectedPacket {
		t.Errorf("expected packet %q, got %q", expectedPacket, packets[0])
	}
}

// TestHandleMetarRequest_InvalidResponse verifies that handleMetarRequest handles responses with invalid formats.
func TestHandleMetarRequest_InvalidResponse(t *testing.T) {
	responseBody := []byte("Invalid response\n")
	mockResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}
	mockTransport := &mockTransport{response: mockResponse}

	service := &metarService{
		httpClient: &http.Client{Transport: mockTransport},
	}

	mock := newMockSession("TEST")
	req := &metarRequest{
		client:   mock,
		icaoCode: "KJFK",
	}

	service.handleMetarRequest(req)

	packets := collectPackets(mock)
	expectedPacket := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	if len(packets) != 1 {
		t.Errorf("expected 1 packet sent, got %d", len(packets))
	} else if packets[0] != expectedPacket {
		t.Errorf("expected packet %q, got %q", expectedPacket, packets[0])
	}
}

// TestHandleMetarRequest_MoreThanTwoLines verifies that handleMetarRequest handles responses with too many lines.
func TestHandleMetarRequest_MoreThanTwoLines(t *testing.T) {
	responseBody := []byte("Line1\nLine2\nLine3\n")
	mockResponse := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}
	mockTransport := &mockTransport{response: mockResponse}

	service := &metarService{
		httpClient: &http.Client{Transport: mockTransport},
	}

	mock := newMockSession("TEST")
	req := &metarRequest{
		client:   mock,
		icaoCode: "KJFK",
	}

	service.handleMetarRequest(req)

	packets := collectPackets(mock)
	expectedPacket := "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	if len(packets) != 1 {
		t.Errorf("expected 1 packet sent, got %d", len(packets))
	} else if packets[0] != expectedPacket {
		t.Errorf("expected packet %q, got %q", expectedPacket, packets[0])
	}
}
