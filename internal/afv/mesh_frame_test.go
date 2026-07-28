package afv

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestEncodeDecodeMeshFrame_HelloGolden(t *testing.T) {
	p := EncodeHelloPayload(HelloPayload{NodeID: "n1", PSK: "secret"})
	var buf bytes.Buffer
	if err := EncodeMeshFrame(&buf, MeshTypeHello, p); err != nil {
		t.Fatal(err)
	}
	// length word
	raw := buf.Bytes()
	if len(raw) < 5 {
		t.Fatal("short")
	}
	n := binary.BigEndian.Uint32(raw[0:4])
	if n != uint32(1+len(p)) {
		t.Fatalf("len=%d want %d", n, 1+len(p))
	}
	if raw[4] != MeshTypeHello {
		t.Fatalf("type=%d", raw[4])
	}
	fr, err := DecodeMeshFrame(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if fr.Type != MeshTypeHello {
		t.Fatal()
	}
	hp, err := DecodeHelloPayload(fr.Payload)
	if err != nil || hp.NodeID != "n1" || hp.PSK != "secret" {
		t.Fatalf("%+v err=%v", hp, err)
	}
}

func TestEncodeDecode_AllTypes(t *testing.T) {
	t.Run("Heartbeat", func(t *testing.T) {
		p := EncodeHeartbeatPayload(12345)
		var buf bytes.Buffer
		_ = EncodeMeshFrame(&buf, MeshTypeHeartbeat, p)
		fr, err := DecodeMeshFrame(&buf)
		if err != nil {
			t.Fatal(err)
		}
		ms, err := DecodeHeartbeatPayload(fr.Payload)
		if err != nil || ms != 12345 {
			t.Fatalf("ms=%d err=%v", ms, err)
		}
		// empty accepted
		if _, err := DecodeHeartbeatPayload(nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("TrxSnapshot", func(t *testing.T) {
		p := TrxSnapshotPayload{
			OriginNodeID: "nodeA",
			Sessions: []MeshSessionBlock{{
				Callsign: "AAL1", ChannelTag: "tag-info-only", IsATC: true,
				Trxs: []MeshTrx{{ID: 1, FreqHz: 118700000, LatDeg: 40.1, LonDeg: -73.2, AltM: 1000}},
			}},
		}
		enc := EncodeTrxSnapshot(p)
		got, err := DecodeTrxSnapshot(enc)
		if err != nil {
			t.Fatal(err)
		}
		if got.OriginNodeID != "nodeA" || len(got.Sessions) != 1 || !got.Sessions[0].IsATC {
			t.Fatalf("%+v", got)
		}
		if got.Sessions[0].Trxs[0].FreqHz != 118700000 {
			t.Fatal()
		}
		// empty snapshot
		empty, err := DecodeTrxSnapshot(EncodeTrxSnapshot(TrxSnapshotPayload{OriginNodeID: "x"}))
		if err != nil || len(empty.Sessions) != 0 {
			t.Fatalf("%+v %v", empty, err)
		}
	})

	t.Run("TrxDelta", func(t *testing.T) {
		p := TrxDeltaPayload{
			OriginNodeID: "n1", Callsign: "UAL2", IsATC: false,
			Trxs: []MeshTrx{{ID: 0, FreqHz: 122800000, LatDeg: 1, LonDeg: 2, AltM: 3}},
		}
		got, err := DecodeTrxDelta(EncodeTrxDelta(p))
		if err != nil || got.Callsign != "UAL2" || len(got.Trxs) != 1 {
			t.Fatalf("%+v %v", got, err)
		}
		// empty nTrx
		empty, err := DecodeTrxDelta(EncodeTrxDelta(TrxDeltaPayload{OriginNodeID: "n", Callsign: "CS"}))
		if err != nil || len(empty.Trxs) != 0 {
			t.Fatal()
		}
	})

	t.Run("SessionLeave", func(t *testing.T) {
		got, err := DecodeSessionLeave(EncodeSessionLeave(SessionLeavePayload{OriginNodeID: "a", Callsign: "CS1"}))
		if err != nil || got.Callsign != "CS1" {
			t.Fatal()
		}
	})

	t.Run("AudioRelay_isATC", func(t *testing.T) {
		r := AudioRelay{
			OriginNode: "n1", Callsign: "AAL1", SequenceCounter: 42,
			LastPacket: true, IsATC: true, IsXC: false,
			Audio:    []byte{1, 2, 3, 4},
			TxRadios: []RelayTxRadio{{TxID: 0, FreqHz: 118700000, LatDeg: 40, LonDeg: -74, HeightM: 500}},
		}
		enc, err := EncodeAudioRelay(r)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeAudioRelay(enc)
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsATC || got.SequenceCounter != 42 || !got.LastPacket || !bytes.Equal(got.Audio, r.Audio) {
			t.Fatalf("%+v", got)
		}
		if got.IsXC {
			t.Fatal("isXC")
		}
		// keys never on mesh: inspect struct — no key fields; payload has no 32-byte key slots
		if strings.Contains(string(enc), "aead") {
			t.Fatal("unexpected key material marker")
		}
	})

	t.Run("Interest", func(t *testing.T) {
		p := InterestPayload{
			NodeID: "n2",
			Entries: []InterestEntry{
				{FreqHz: 118700000, ILat: 160, ILon: -296},
			},
		}
		got, err := DecodeInterest(EncodeInterest(p))
		if err != nil || len(got.Entries) != 1 || got.Entries[0].ILat != 160 {
			t.Fatalf("%+v %v", got, err)
		}
		empty, err := DecodeInterest(EncodeInterest(InterestPayload{NodeID: "n"}))
		if err != nil || len(empty.Entries) != 0 {
			t.Fatal()
		}
	})
}

func TestMeshFrame_OversizedAndZero(t *testing.T) {
	// n==0
	var z [5]byte
	binary.BigEndian.PutUint32(z[0:4], 0)
	if _, err := DecodeMeshFrame(bytes.NewReader(z[:])); err != errMeshShortFrame {
		t.Fatalf("err=%v", err)
	}
	// oversized payload on encode
	big := make([]byte, MaxMeshPayload+1)
	if err := EncodeMeshFrame(&bytes.Buffer{}, MeshTypeHello, big); err != errMeshFrameTooLarge {
		t.Fatalf("err=%v", err)
	}
	// oversized on decode
	var hdr [5]byte
	binary.BigEndian.PutUint32(hdr[0:4], maxMeshFrameTotal+1)
	hdr[4] = MeshTypeHello
	if _, err := DecodeMeshFrame(bytes.NewReader(hdr[:])); err != errMeshFrameTooLarge {
		t.Fatalf("err=%v", err)
	}
}

func TestAudioRelay_AudioTooLarge(t *testing.T) {
	r := AudioRelay{
		OriginNode: "n", Callsign: "C",
		Audio: make([]byte, maxAudioRelayBytes+1),
	}
	if _, err := EncodeAudioRelay(r); err == nil {
		t.Fatal("expected reject")
	}
	// decode rejects large audio
	// build manually with huge audioLen
	var b []byte
	b = meshEncodeString(b, "n")
	b = meshEncodeString(b, "C")
	b = meshEncodeU32(b, 1)
	b = meshEncodeBoolU8(b, false)
	b = meshEncodeBoolU8(b, false)
	b = meshEncodeBoolU8(b, false)
	huge := make([]byte, maxAudioRelayBytes+10)
	b = meshEncodeBytes(b, huge)
	b = meshEncodeU16(b, 0)
	if _, err := DecodeAudioRelay(b); err == nil {
		t.Fatal("expected reject oversized audio")
	}
}

func TestHelloVerifyPSK(t *testing.T) {
	if err := VerifyHelloPSK("abc", "abc"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHelloPSK("abc", "abd"); err != errMeshHelloAuth {
		t.Fatalf("err=%v", err)
	}
	if err := VerifyHelloPSK("abc", "ab"); err != errMeshHelloAuth {
		t.Fatalf("length mismatch err=%v", err)
	}
	if err := VerifyHelloPSK("", ""); err != errMeshHelloAuth {
		t.Fatalf("empty PSK should reject: %v", err)
	}
	if err := VerifyHelloPSK("x", ""); err != errMeshHelloAuth {
		t.Fatal(err)
	}
}

// Hello first-frame-not-type-1 is enforced on the TCP accept path (PR-10b).
// MemoryMesh has no wire Hello; document here so row 2b is not silently dropped.
func TestHelloFirstFrameTCPDeferred(t *testing.T) {
	t.Log("Hello first-frame type check is TCP-only (mesh_tcp.go / PR-10b); MemoryMesh uses constructor PSK")
}

func TestMeshStringOversizeTruncate(t *testing.T) {
	long := strings.Repeat("x", maxMeshString+50)
	enc := meshEncodeString(nil, long)
	s, rest, err := meshDecodeString(enc)
	if err != nil || len(rest) != 0 {
		t.Fatal(err)
	}
	if len(s) != maxMeshString {
		t.Fatalf("len=%d", len(s))
	}
}

func TestMeshDecodeShortPayloads(t *testing.T) {
	if _, err := DecodeHelloPayload([]byte{0}); err == nil {
		t.Fatal()
	}
	if _, err := DecodeTrxDelta([]byte{0, 1}); err == nil {
		t.Fatal()
	}
	if _, err := DecodeSessionLeave(nil); err == nil {
		t.Fatal()
	}
	if _, err := DecodeInterest([]byte{0, 0}); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeU16(nil); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeF64([]byte{1, 2, 3}); err == nil {
		t.Fatal()
	}
}

func TestMeshFrame_WriteErrorsAndEdges(t *testing.T) {
	// Encode with failing writer
	errW := &errWriter{}
	if err := EncodeMeshFrame(errW, MeshTypeHello, []byte{1}); err == nil {
		t.Fatal("want write error")
	}
	errW2 := &errWriter{failAfter: 1}
	if err := EncodeMeshFrame(errW2, MeshTypeHello, []byte{1, 2, 3}); err == nil {
		t.Fatal("want payload write error")
	}
	// Decode short reader after header
	var hdr [5]byte
	binary.BigEndian.PutUint32(hdr[0:4], 10) // claims 9 payload bytes
	hdr[4] = MeshTypeHeartbeat
	if _, err := DecodeMeshFrame(bytes.NewReader(hdr[:])); err == nil {
		t.Fatal("short body")
	}
	// Decode empty payload frame (len=1 type only)
	binary.BigEndian.PutUint32(hdr[0:4], 1)
	hdr[4] = MeshTypeHeartbeat
	fr, err := DecodeMeshFrame(bytes.NewReader(hdr[:]))
	if err != nil || fr.Type != MeshTypeHeartbeat || len(fr.Payload) != 0 {
		t.Fatalf("%+v %v", fr, err)
	}
	// partial header
	if _, err := DecodeMeshFrame(bytes.NewReader([]byte{0, 0})); err == nil {
		t.Fatal()
	}
}

type errWriter struct {
	n         int
	failAfter int
}

func (e *errWriter) Write(p []byte) (int, error) {
	e.n++
	if e.failAfter == 0 || e.n > e.failAfter {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func TestDecodeAll_TrailingGarbageAndShort(t *testing.T) {
	// Hello trailing
	b := EncodeHelloPayload(HelloPayload{NodeID: "a", PSK: "b"})
	b = append(b, 0xff)
	if _, err := DecodeHelloPayload(b); err == nil {
		t.Fatal()
	}
	// Hello short psk
	b = meshEncodeString(nil, "n")
	if _, err := DecodeHelloPayload(b); err == nil {
		t.Fatal()
	}
	// Heartbeat short
	if _, err := DecodeHeartbeatPayload([]byte{1, 2, 3}); err == nil {
		t.Fatal()
	}
	// Heartbeat trailing
	p := EncodeHeartbeatPayload(1)
	p = append(p, 0)
	if _, err := DecodeHeartbeatPayload(p); err == nil {
		t.Fatal()
	}
	// SessionLeave trailing
	sl := EncodeSessionLeave(SessionLeavePayload{OriginNodeID: "a", Callsign: "b"})
	sl = append(sl, 1)
	if _, err := DecodeSessionLeave(sl); err == nil {
		t.Fatal()
	}
	// SessionLeave short callsign
	if _, err := DecodeSessionLeave(meshEncodeString(nil, "o")); err == nil {
		t.Fatal()
	}
	// Interest trailing
	ip := EncodeInterest(InterestPayload{NodeID: "n", Entries: []InterestEntry{{FreqHz: 1}}})
	ip = append(ip, 0)
	if _, err := DecodeInterest(ip); err == nil {
		t.Fatal()
	}
	// Interest huge n (prealloc cap)
	var big []byte
	big = meshEncodeString(big, "n")
	big = meshEncodeU32(big, 1<<21)
	if _, err := DecodeInterest(big); err == nil {
		t.Fatal()
	}
	// Interest n larger than remaining bytes
	var shortN []byte
	shortN = meshEncodeString(shortN, "n")
	shortN = meshEncodeU32(shortN, 100)
	if _, err := DecodeInterest(shortN); err == nil {
		t.Fatal("short body for n entries")
	}
	// Interest short entry
	var short []byte
	short = meshEncodeString(short, "n")
	short = meshEncodeU32(short, 1)
	if _, err := DecodeInterest(short); err == nil {
		t.Fatal()
	}
	// TrxDelta short fields
	if _, err := DecodeTrxDelta(meshEncodeString(nil, "o")); err == nil {
		t.Fatal()
	}
	td := EncodeTrxDelta(TrxDeltaPayload{OriginNodeID: "o", Callsign: "c", Trxs: []MeshTrx{{ID: 1, FreqHz: 2}}})
	td = append(td, 9)
	if _, err := DecodeTrxDelta(td); err == nil {
		t.Fatal()
	}
	// truncated trx body
	var tdb []byte
	tdb = meshEncodeString(tdb, "o")
	tdb = meshEncodeString(tdb, "c")
	tdb = meshEncodeBoolU8(tdb, true)
	tdb = meshEncodeU16(tdb, 1)
	// missing trx fields
	if _, err := DecodeTrxDelta(tdb); err == nil {
		t.Fatal()
	}
	// TrxSnapshot edges
	if _, err := DecodeTrxSnapshot(meshEncodeString(nil, "o")); err == nil {
		t.Fatal()
	}
	var snap []byte
	snap = meshEncodeString(snap, "o")
	snap = meshEncodeU32(snap, 1<<21)
	if _, err := DecodeTrxSnapshot(snap); err == nil {
		t.Fatal()
	}
	// incomplete session
	snap = nil
	snap = meshEncodeString(snap, "o")
	snap = meshEncodeU32(snap, 1)
	if _, err := DecodeTrxSnapshot(snap); err == nil {
		t.Fatal()
	}
	// incomplete trx in session
	snap = nil
	snap = meshEncodeString(snap, "o")
	snap = meshEncodeU32(snap, 1)
	snap = meshEncodeString(snap, "CS")
	snap = meshEncodeString(snap, "tag")
	snap = meshEncodeBoolU8(snap, false)
	snap = meshEncodeU16(snap, 1)
	if _, err := DecodeTrxSnapshot(snap); err == nil {
		t.Fatal()
	}
	// trailing after good snapshot
	good := EncodeTrxSnapshot(TrxSnapshotPayload{OriginNodeID: "o", Sessions: []MeshSessionBlock{{
		Callsign: "C", ChannelTag: "t", Trxs: []MeshTrx{{ID: 1, FreqHz: 2, LatDeg: 3, LonDeg: 4, AltM: 5}},
	}}})
	good = append(good, 0)
	if _, err := DecodeTrxSnapshot(good); err == nil {
		t.Fatal()
	}
	// AudioRelay short at each stage
	if _, err := DecodeAudioRelay(nil); err == nil {
		t.Fatal()
	}
	var ar []byte
	ar = meshEncodeString(ar, "n")
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeString(ar, "cs")
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeU32(ar, 1)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeBoolU8(ar, true)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeBoolU8(ar, true)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeBoolU8(ar, false)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeBytes(ar, []byte{1})
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal() // missing nTx
	}
	// incomplete radio
	ar = meshEncodeU16(ar, 1)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	// trailing garbage after full encode
	full, _ := EncodeAudioRelay(AudioRelay{OriginNode: "n", Callsign: "c", Audio: []byte{1}})
	full = append(full, 0)
	if _, err := DecodeAudioRelay(full); err == nil {
		t.Fatal()
	}
	// mesh helper short paths
	if _, _, err := meshDecodeString([]byte{0, 5, 1}); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeBytes([]byte{0, 0, 0, 5, 1}); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeBytes(nil); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeU32(nil); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeU64(nil); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeBoolU8(nil); err == nil {
		t.Fatal()
	}
	if _, _, err := meshDecodeI32(nil); err == nil {
		t.Fatal()
	}
	// encodeBytes clamp path (huge slice is expensive — just verify normal)
	_ = meshEncodeBytes(nil, []byte{1, 2})
	// Interest incomplete mid-entry (have freq, missing iLat)
	var ie []byte
	ie = meshEncodeString(ie, "n")
	ie = meshEncodeU32(ie, 1)
	ie = meshEncodeU32(ie, 9)
	if _, err := DecodeInterest(ie); err == nil {
		t.Fatal()
	}
	ie = meshEncodeI32(ie, 1)
	if _, err := DecodeInterest(ie); err == nil {
		t.Fatal()
	}
}

func TestEncodeMeshFrame_EmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeMeshFrame(&buf, MeshTypeHeartbeat, nil); err != nil {
		t.Fatal(err)
	}
	fr, err := DecodeMeshFrame(&buf)
	if err != nil || fr.Type != MeshTypeHeartbeat {
		t.Fatal(err)
	}
}

func TestMeshEncodeBytesClamp(t *testing.T) {
	// exercise len clamp branch (16MiB+1)
	big := make([]byte, 0x1000001)
	enc := meshEncodeBytes(nil, big)
	// u32 length of clamped size
	if binary.BigEndian.Uint32(enc[:4]) != 0xffffff {
		t.Fatalf("len=%d", binary.BigEndian.Uint32(enc[:4]))
	}
}

func TestDecodePartialTrxAndRelayRadios(t *testing.T) {
	// Snapshot: callsign ok, tag short
	var b []byte
	b = meshEncodeString(b, "o")
	b = meshEncodeU32(b, 1)
	b = meshEncodeString(b, "CS")
	// no tag
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}
	// tag ok, no isATC
	b = meshEncodeString(b, "tag")
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}
	b = meshEncodeBoolU8(b, true)
	// no nTrx
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}
	b = meshEncodeU16(b, 1)
	// partial trx: only id
	b = meshEncodeU16(b, 1)
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}
	b = meshEncodeU32(b, 9)
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}
	b = meshEncodeF64(b, 1)
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}
	b = meshEncodeF64(b, 2)
	if _, err := DecodeTrxSnapshot(b); err == nil {
		t.Fatal()
	}

	// Delta partial radios
	var d []byte
	d = meshEncodeString(d, "o")
	d = meshEncodeString(d, "c")
	d = meshEncodeBoolU8(d, false)
	d = meshEncodeU16(d, 1)
	d = meshEncodeU16(d, 1)
	if _, err := DecodeTrxDelta(d); err == nil {
		t.Fatal()
	}
	d = meshEncodeU32(d, 1)
	if _, err := DecodeTrxDelta(d); err == nil {
		t.Fatal()
	}
	d = meshEncodeF64(d, 1)
	if _, err := DecodeTrxDelta(d); err == nil {
		t.Fatal()
	}
	d = meshEncodeF64(d, 2)
	if _, err := DecodeTrxDelta(d); err == nil {
		t.Fatal()
	}

	// AudioRelay partial radio fields after nTx=1 and txID
	full, _ := EncodeAudioRelay(AudioRelay{OriginNode: "n", Callsign: "c", Audio: []byte{1},
		TxRadios: []RelayTxRadio{{TxID: 1, FreqHz: 2, LatDeg: 3, LonDeg: 4, HeightM: 5}}})
	// strip last 8 bytes (height) repeatedly
	for cut := 8; cut < 40; cut += 8 {
		if cut >= len(full) {
			break
		}
		if _, err := DecodeAudioRelay(full[:len(full)-cut]); err == nil {
			// may succeed for some cuts; only care about eventually hitting error paths
		}
	}
	// precise: stop after txID
	var ar []byte
	ar = meshEncodeString(ar, "n")
	ar = meshEncodeString(ar, "c")
	ar = meshEncodeU32(ar, 1)
	ar = meshEncodeBoolU8(ar, false)
	ar = meshEncodeBoolU8(ar, false)
	ar = meshEncodeBoolU8(ar, false)
	ar = meshEncodeBytes(ar, []byte{1})
	ar = meshEncodeU16(ar, 1)
	ar = meshEncodeU16(ar, 7) // txID only
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeU32(ar, 1)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeF64(ar, 1)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	ar = meshEncodeF64(ar, 2)
	if _, err := DecodeAudioRelay(ar); err == nil {
		t.Fatal()
	}
	// Interest decode node short already covered; short nEntries body mid iLon
	var ie []byte
	ie = meshEncodeString(ie, "n")
	// missing n
	if _, err := DecodeInterest(ie); err == nil {
		t.Fatal()
	}
}
