package protocol

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFormatError(t *testing.T) {
	got := FormatError(InvalidLogonError, "Invalid CID/password")
	want := "$ERserver:unknown:6::Invalid CID/password\r\n"
	if got != want {
		t.Errorf("FormatError = %q, want %q", got, want)
	}

	// Match metar service error shape used in fsd tests.
	got = FormatError(NoWeatherProfileError, "Error fetching METAR for KJFK")
	want = "$ERserver:unknown:9::Error fetching METAR for KJFK\r\n"
	if got != want {
		t.Errorf("FormatError metar = %q, want %q", got, want)
	}

	// All codes 1-17 produce expected prefix/suffix.
	for code := ErrorCode(1); code <= 17; code++ {
		s := FormatError(code, "x")
		if s[:len("$ERserver:unknown:")] != "$ERserver:unknown:" {
			t.Errorf("code %d bad prefix: %q", code, s)
		}
		if s[len(s)-2:] != "\r\n" {
			t.Errorf("code %d missing CRLF: %q", code, s)
		}
	}
}

func TestWriteError(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteError(&buf, SyntaxError, "Packet too short"); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := "$ERserver:unknown:4::Packet too short\r\n"
	if buf.String() != want {
		t.Errorf("WriteError wrote %q, want %q", buf.String(), want)
	}

	if err := WriteError(nil, SyntaxError, "x"); err == nil {
		t.Error("WriteError(nil) should error")
	}

	errWriter := errWriter{}
	if err := WriteError(errWriter, SyntaxError, "x"); err == nil {
		t.Error("WriteError should propagate writer error")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

// ensure io.Writer interface is satisfied by *bytes.Buffer in tests
var _ io.Writer = (*bytes.Buffer)(nil)
