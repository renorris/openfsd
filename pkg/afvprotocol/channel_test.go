package afvprotocol_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

func TestHeaderRoundTrip(t *testing.T) {
	h := afvprotocol.Header{
		ChannelTag: "abc123",
		Sequence:   42,
		Mode:       afvprotocol.ModeChaCha20Poly1305,
	}
	mp := afvprotocol.EncodeHeaderMsgpack(h)
	got, err := afvprotocol.DecodeHeaderMsgpack(mp)
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("got %+v want %+v", got, h)
	}

	prefix := afvprotocol.PackHeaderPrefix(h, nil)
	parsed, end, err := afvprotocol.ParseHeader(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != h {
		t.Fatalf("parse %+v", parsed)
	}
	if end != len(prefix) {
		t.Fatalf("end=%d len=%d", end, len(prefix))
	}
}

func TestHeaderParseErrors(t *testing.T) {
	if _, _, err := afvprotocol.ParseHeader(nil); err == nil {
		t.Fatal("short")
	}
	if _, _, err := afvprotocol.ParseHeader([]byte{0, 0}); err == nil {
		t.Fatal("zero size")
	}
	if _, _, err := afvprotocol.ParseHeader([]byte{5, 0, 1, 2}); err == nil {
		t.Fatal("truncated")
	}
	bad := []byte{1, 0, 0xff}
	if _, _, err := afvprotocol.ParseHeader(bad); err == nil {
		t.Fatal("bad mp")
	}
}

func TestEncapsulateDecapsulateHA(t *testing.T) {
	srv, err := afvprotocol.ServerChannel("abc123", goldenRxKey, goldenTxKey)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := afvprotocol.ClientChannel("abc123", goldenRxKey, goldenTxKey)
	if err != nil {
		t.Fatal(err)
	}

	const seq = uint64(7)
	pkt, err := srv.EncapsulateHA(seq, nil)
	if err != nil {
		t.Fatal(err)
	}

	hdr, name, payload, err := cli.Decapsulate(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.ChannelTag != "abc123" || hdr.Sequence != seq || hdr.Mode != afvprotocol.ModeChaCha20Poly1305 {
		t.Fatalf("hdr=%+v", hdr)
	}
	if name != afvprotocol.DTONameHeartbeatAck {
		t.Fatalf("name=%q", name)
	}
	if len(payload) != 0 {
		t.Fatalf("HA payload must be empty, got %d bytes", len(payload))
	}
}

func TestHRoundTripClientToServer(t *testing.T) {
	srv, err := afvprotocol.ServerChannel("tag1", goldenRxKey, goldenTxKey)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := afvprotocol.ClientChannel("tag1", goldenRxKey, goldenTxKey)
	if err != nil {
		t.Fatal(err)
	}

	hb := afvprotocol.Heartbeat{Callsign: "N123AB"}
	mp := hb.EncodeMsgpack()
	pkt, err := cli.Encapsulate(0, afvprotocol.DTONameHeartbeat, mp, nil)
	if err != nil {
		t.Fatal(err)
	}

	hdr, name, payload, err := srv.Decapsulate(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if name != "H" || hdr.Sequence != 0 {
		t.Fatalf("name=%s seq=%d", name, hdr.Sequence)
	}
	got, err := afvprotocol.DecodeHeartbeat(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Callsign != "N123AB" {
		t.Fatalf("callsign=%q", got.Callsign)
	}
}

func TestATARRoundTrip(t *testing.T) {
	rx := bytes.Repeat([]byte{0x11}, 32)
	tx := bytes.Repeat([]byte{0x22}, 32)
	srv, err := afvprotocol.ServerChannel("ch", rx, tx)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := afvprotocol.ClientChannel("ch", rx, tx)
	if err != nil {
		t.Fatal(err)
	}

	at := afvprotocol.AudioTx{
		Callsign:        "AAL100",
		SequenceCounter: 99,
		Audio:           []byte{1, 2, 3, 4, 5},
		LastPacket:      true,
		Transceivers:    []afvprotocol.TxTransceiver{{ID: 0}, {ID: 1}},
	}
	pkt, err := cli.Encapsulate(5, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}

	_, name, payload, err := srv.Decapsulate(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if name != "AT" {
		t.Fatal(name)
	}
	gotAT, err := afvprotocol.DecodeAudioTx(payload)
	if err != nil {
		t.Fatal(err)
	}
	if gotAT.Callsign != at.Callsign || gotAT.SequenceCounter != 99 || !gotAT.LastPacket {
		t.Fatalf("%+v", gotAT)
	}
	if !bytes.Equal(gotAT.Audio, at.Audio) || len(gotAT.Transceivers) != 2 {
		t.Fatalf("%+v", gotAT)
	}

	ar := afvprotocol.AudioRx{
		Callsign:        "AAL100",
		SequenceCounter: 99,
		Audio:           at.Audio,
		LastPacket:      true,
		Transceivers: []afvprotocol.RxTransceiver{
			{ID: 0, Frequency: 118700000, DistanceRatio: 0.5},
		},
	}
	pkt, err = srv.Encapsulate(1, afvprotocol.DTONameAudioRx, ar.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, name, payload, err = cli.Decapsulate(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if name != "AR" {
		t.Fatal(name)
	}
	gotAR, err := afvprotocol.DecodeAudioRx(payload)
	if err != nil {
		t.Fatal(err)
	}
	if gotAR.Callsign != "AAL100" || len(gotAR.Transceivers) != 1 {
		t.Fatalf("%+v", gotAR)
	}
	if gotAR.Transceivers[0].Frequency != 118700000 {
		t.Fatalf("freq=%d", gotAR.Transceivers[0].Frequency)
	}
	if gotAR.Transceivers[0].DistanceRatio < 0.49 || gotAR.Transceivers[0].DistanceRatio > 0.51 {
		t.Fatalf("ratio=%v", gotAR.Transceivers[0].DistanceRatio)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	rx := bytes.Repeat([]byte{1}, 32)
	tx := bytes.Repeat([]byte{2}, 32)
	cli, _ := afvprotocol.ClientChannel("t", rx, tx)
	pkt, err := cli.Encapsulate(0, "H", afvprotocol.Heartbeat{Callsign: "X"}.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}

	badRx := bytes.Repeat([]byte{9}, 32)
	srvBad, _ := afvprotocol.ServerChannel("t", rx, badRx)
	if _, _, _, err := srvBad.Decapsulate(pkt); err == nil {
		t.Fatal("expected decrypt fail")
	}
}

func TestRejectModeNone(t *testing.T) {
	h := afvprotocol.Header{ChannelTag: "t", Sequence: 0, Mode: afvprotocol.ModeNone}
	prefix := afvprotocol.PackHeaderPrefix(h, nil)
	pkt := append(prefix, make([]byte, 16)...)
	ch, _ := afvprotocol.ServerChannel("t", goldenRxKey, goldenTxKey)
	if _, _, _, err := ch.Decapsulate(pkt); err == nil {
		t.Fatal("expected mode reject or decrypt fail")
	}
}

func TestNonceLayout(t *testing.T) {
	n := afvprotocol.NonceFromSequence(0x0102030405060708)
	if n[0] != 0 || n[1] != 0 || n[2] != 0 || n[3] != 0 {
		t.Fatalf("prefix zeros: %x", n[:4])
	}
	want := []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}
	if !bytes.Equal(n[4:], want) {
		t.Fatalf("nonce seq bytes %x want %x", n[4:], want)
	}
}

func TestNewChannelKeySize(t *testing.T) {
	if _, err := afvprotocol.NewChannel("t", []byte{1}, []byte{2}); err == nil {
		t.Fatal("want key size error")
	}
}

func TestParseBodyErrors(t *testing.T) {
	if _, _, err := afvprotocol.ParseBody(nil); err == nil {
		t.Fatal()
	}
	b := []byte{10, 0, 'a'}
	if _, _, err := afvprotocol.ParseBody(b); err == nil {
		t.Fatal()
	}
	body := []byte{1, 0, 'H', 5, 0}
	if _, _, err := afvprotocol.ParseBody(body); err == nil {
		t.Fatal("dto len mismatch")
	}
}

func TestRandomKeysEncapsulate(t *testing.T) {
	var rx, tx [32]byte
	_, _ = rand.Read(rx[:])
	_, _ = rand.Read(tx[:])
	srv, err := afvprotocol.ServerChannel("rnd", rx[:], tx[:])
	if err != nil {
		t.Fatal(err)
	}
	cli, err := afvprotocol.ClientChannel("rnd", rx[:], tx[:])
	if err != nil {
		t.Fatal(err)
	}
	for seq := uint64(0); seq < 5; seq++ {
		pkt, err := srv.EncapsulateHA(seq, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, name, _, err := cli.Decapsulate(pkt)
		if err != nil || name != "HA" {
			t.Fatalf("seq=%d err=%v name=%s", seq, err, name)
		}
	}
}

func TestHeaderLargeSequence(t *testing.T) {
	h := afvprotocol.Header{ChannelTag: "x", Sequence: ^uint64(0), Mode: 2}
	mp := afvprotocol.EncodeHeaderMsgpack(h)
	got, err := afvprotocol.DecodeHeaderMsgpack(mp)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sequence != ^uint64(0) {
		t.Fatalf("seq=%d", got.Sequence)
	}
}

func TestDTOMsgpackErrors(t *testing.T) {
	if _, err := afvprotocol.DecodeHeartbeat([]byte{0x92}); err == nil {
		t.Fatal("truncated H")
	}
	if _, err := afvprotocol.DecodeHeartbeat([]byte{0x92, 0xa1, 'a', 0xa1, 'b'}); err == nil {
		t.Fatal("wrong len H")
	}
	if _, err := afvprotocol.DecodeAudioTx([]byte{0x90}); err == nil {
		t.Fatal("AT empty array")
	}
	if _, err := afvprotocol.DecodeAudioRx([]byte{0x90}); err == nil {
		t.Fatal("AR empty array")
	}
}

func TestEncodeBodyHA(t *testing.T) {
	b := afvprotocol.EncodeBody("HA", nil)
	// nameLen(2) + "HA"(2) + dtoMsgpackLen(2) = 6
	if len(b) != 6 {
		t.Fatalf("len=%d", len(b))
	}
	name, mp, err := afvprotocol.ParseBody(b)
	if err != nil || name != "HA" || mp != nil {
		t.Fatalf("name=%s mp=%v err=%v", name, mp, err)
	}
}

func TestNilChannel(t *testing.T) {
	var c *afvprotocol.Channel
	if _, err := c.Encapsulate(0, "H", nil, nil); err == nil {
		t.Fatal()
	}
	if _, err := c.DecryptBody(nil, 0, 0); err == nil {
		t.Fatal()
	}
}
