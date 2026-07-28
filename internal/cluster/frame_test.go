package cluster

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeFrame(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello-mesh")
	if err := EncodeFrame(&buf, TypeHello, payload); err != nil {
		t.Fatal(err)
	}
	fr, err := DecodeFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if fr.Type != TypeHello {
		t.Fatalf("type %d", fr.Type)
	}
	if string(fr.Payload) != "hello-mesh" {
		t.Fatalf("payload %q", fr.Payload)
	}
}

func TestEncodeDecodeEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeFrame(&buf, TypeHeartbeat, nil); err != nil {
		t.Fatal(err)
	}
	fr, err := DecodeFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if fr.Type != TypeHeartbeat || len(fr.Payload) != 0 {
		t.Fatalf("%+v", fr)
	}
}

func TestFrameTooLarge(t *testing.T) {
	big := make([]byte, MaxFramePayload+1)
	if err := EncodeFrame(&bytes.Buffer{}, TypeHello, big); err != ErrFrameTooLarge {
		t.Fatalf("got %v", err)
	}
}

func TestStringRoundTrip(t *testing.T) {
	b := encodeString(nil, "US-EAST-1")
	s, rest, err := decodeString(b)
	if err != nil || s != "US-EAST-1" || len(rest) != 0 {
		t.Fatalf("%q %v %v", s, rest, err)
	}
}

func TestDirMetaRoundTrip(t *testing.T) {
	m := DirMeta{
		Callsign: "N123AB", NodeID: "us", CID: 100, IsATC: false,
		FPLInfo: "VFR", AssignedBeacon: "1200", Fence: "abc", Routable: true,
		Lat: 40.0, Lon: -74.0,
	}
	raw := EncodeSnapshot([]DirMeta{m})
	out, err := DecodeSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Fence must NOT be broadcast in snapshots (security).
	if len(out) != 1 || out[0].Callsign != "N123AB" || out[0].Fence != "" {
		t.Fatalf("%+v", out)
	}
	d := EncodeDelta(DeltaJoin, m)
	kind, m2, err := DecodeDelta(d)
	if err != nil || kind != DeltaJoin || m2.Callsign != "N123AB" {
		t.Fatalf("%v %v %+v", kind, err, m2)
	}
}
