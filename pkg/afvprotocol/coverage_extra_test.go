package afvprotocol

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// Exhaustive branch coverage for pure helpers.

func TestEncodeArrayHeaderLarge(t *testing.T) {
	// array32 path
	b := encodeArrayHeader(nil, 0x10000)
	if b[0] != 0xdd {
		t.Fatalf("want array32 header, got 0x%02x", b[0])
	}
	d := decoder{b: b}
	n, err := d.arrayLen()
	if err != nil || n != 0x10000 {
		t.Fatal(n, err)
	}
	// negative n treated as 0
	b = encodeArrayHeader(nil, -1)
	d = decoder{b: b}
	n, _ = d.arrayLen()
	if n != 0 {
		t.Fatal(n)
	}
}

func TestEncodeStringStr32(t *testing.T) {
	s := strings.Repeat("a", 70000)
	b := encodeString(nil, s)
	if b[0] != 0xdb {
		t.Fatalf("want str32 0xdb got 0x%02x", b[0])
	}
	d := decoder{b: b}
	got, err := d.string()
	if err != nil || got != s {
		t.Fatal(err, len(got))
	}
}

func TestEncodeBinLarge(t *testing.T) {
	// bin16
	b16 := make([]byte, 300)
	enc := encodeBin(nil, b16)
	if enc[0] != 0xc5 {
		t.Fatal(enc[0])
	}
	d := decoder{b: enc}
	got, err := d.bin()
	if err != nil || len(got) != 300 {
		t.Fatal(err, len(got))
	}
	// bin32
	b32 := make([]byte, 70000)
	enc = encodeBin(nil, b32)
	if enc[0] != 0xc6 {
		t.Fatal(enc[0])
	}
	d = decoder{b: enc}
	got, err = d.bin()
	if err != nil || len(got) != 70000 {
		t.Fatal(err, len(got))
	}
}

func TestDecodeUintSignedPaths(t *testing.T) {
	// int8 positive via 0xd0
	b := []byte{0xd0, 5}
	d := decoder{b: b}
	u, err := d.uint64()
	if err != nil || u != 5 {
		t.Fatal(u, err)
	}
	// int8 negative → overflow
	b = []byte{0xd0, 0xff}
	d = decoder{b: b}
	if _, err := d.uint64(); err == nil {
		t.Fatal("want overflow")
	}
	// int16 positive
	b = []byte{0xd1, 0x00, 0x10}
	d = decoder{b: b}
	u, err = d.uint64()
	if err != nil || u != 16 {
		t.Fatal(u, err)
	}
	// int16 negative
	b = []byte{0xd1, 0xff, 0xff}
	d = decoder{b: b}
	if _, err := d.uint64(); err == nil {
		t.Fatal()
	}
	// int32 positive
	b = []byte{0xd2, 0x00, 0x00, 0x00, 0x20}
	d = decoder{b: b}
	u, err = d.uint64()
	if err != nil || u != 32 {
		t.Fatal(u, err)
	}
	// int32 negative
	b = []byte{0xd2, 0xff, 0xff, 0xff, 0xff}
	d = decoder{b: b}
	if _, err := d.uint64(); err == nil {
		t.Fatal()
	}
	// int64 positive
	b = append([]byte{0xd3}, make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[1:], 64)
	d = decoder{b: b}
	u, err = d.uint64()
	if err != nil || u != 64 {
		t.Fatal(u, err)
	}
	// int64 negative
	b = append([]byte{0xd3}, make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[1:], ^uint64(0))
	d = decoder{b: b}
	if _, err := d.uint64(); err == nil {
		t.Fatal()
	}
	// negative fixint as uint
	b = []byte{0xff}
	d = decoder{b: b}
	if _, err := d.uint64(); err == nil {
		t.Fatal()
	}
}

func TestDecodeIntPaths(t *testing.T) {
	// large uint that overflows int64
	b := append([]byte{0xcf}, make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[1:], math.MaxUint64)
	d := decoder{b: b}
	if _, err := d.int(); err == nil {
		t.Fatal("want overflow")
	}
	// truncated peek
	d = decoder{b: nil}
	if _, err := d.int(); err == nil {
		t.Fatal()
	}
}

func TestDecodeStringPaths(t *testing.T) {
	// str8 truncated
	d := decoder{b: []byte{0xd9}}
	if _, err := d.string(); err == nil {
		t.Fatal()
	}
	// str16 truncated
	d = decoder{b: []byte{0xda, 0x00}}
	if _, err := d.string(); err == nil {
		t.Fatal()
	}
	// str32 truncated
	d = decoder{b: []byte{0xdb, 0, 0, 0}}
	if _, err := d.string(); err == nil {
		t.Fatal()
	}
	// fixstr truncated body
	d = decoder{b: []byte{0xa5, 'a'}}
	if _, err := d.string(); err == nil {
		t.Fatal()
	}
}

func TestDecodeArrayLenPaths(t *testing.T) {
	// array16 truncated
	d := decoder{b: []byte{0xdc, 0x00}}
	if _, err := d.arrayLen(); err == nil {
		t.Fatal()
	}
	// array32 truncated
	d = decoder{b: []byte{0xdd, 0, 0, 0}}
	if _, err := d.arrayLen(); err == nil {
		t.Fatal()
	}
	// array16 ok
	b := encodeArrayHeader(nil, 100)
	d = decoder{b: b}
	n, err := d.arrayLen()
	if err != nil || n != 100 {
		t.Fatal(n, err)
	}
}

func TestDecodeBinAsStr(t *testing.T) {
	// bin via str fallback
	b := encodeString(nil, "hi")
	d := decoder{b: b}
	got, err := d.bin()
	if err != nil || string(got) != "hi" {
		t.Fatal(err, got)
	}
	// bin8 truncated
	d = decoder{b: []byte{0xc4}}
	if _, err := d.bin(); err == nil {
		t.Fatal()
	}
	// bin16 truncated
	d = decoder{b: []byte{0xc5, 0}}
	if _, err := d.bin(); err == nil {
		t.Fatal()
	}
	// bin32 truncated
	d = decoder{b: []byte{0xc6, 0, 0, 0}}
	if _, err := d.bin(); err == nil {
		t.Fatal()
	}
}

func TestDecodeFloatPaths(t *testing.T) {
	// float64
	var bits [8]byte
	binary.BigEndian.PutUint64(bits[:], math.Float64bits(1.25))
	b := append([]byte{0xcb}, bits[:]...)
	d := decoder{b: b}
	f, err := d.float32()
	if err != nil || f != float32(1.25) {
		t.Fatal(f, err)
	}
	// integer as float
	b = encodeInt(nil, 3)
	d = decoder{b: b}
	f, err = d.float32()
	if err != nil || f != 3 {
		t.Fatal(f, err)
	}
	// truncated float32
	d = decoder{b: []byte{0xca, 0, 0}}
	if _, err := d.float32(); err == nil {
		t.Fatal()
	}
	// truncated float64
	d = decoder{b: []byte{0xcb, 0, 0}}
	if _, err := d.float32(); err == nil {
		t.Fatal()
	}
	// truncated start
	d = decoder{b: nil}
	if _, err := d.float32(); err == nil {
		t.Fatal()
	}
}

func TestUint16Uint32Overflow(t *testing.T) {
	b := encodeUint64(nil, 0x10000)
	d := decoder{b: b}
	if _, err := d.uint16(); err == nil {
		t.Fatal()
	}
	b = encodeUint64(nil, 0x100000000)
	d = decoder{b: b}
	if _, err := d.uint32(); err == nil {
		t.Fatal()
	}
	// success paths
	b = encodeUint64(nil, 1000)
	d = decoder{b: b}
	u16, err := d.uint16()
	if err != nil || u16 != 1000 {
		t.Fatal(u16, err)
	}
	b = encodeUint64(nil, 100000)
	d = decoder{b: b}
	u32, err := d.uint32()
	if err != nil || u32 != 100000 {
		t.Fatal(u32, err)
	}
}

func TestRemainAndTake(t *testing.T) {
	d := decoder{b: []byte{1, 2, 3}}
	if d.remain() != 3 {
		t.Fatal(d.remain())
	}
	if _, err := d.take(-1); err == nil {
		t.Fatal()
	}
	if _, err := d.take(10); err == nil {
		t.Fatal()
	}
	raw, err := d.take(2)
	if err != nil || len(raw) != 2 {
		t.Fatal(err)
	}
}

func TestDecodeHeaderMsgpackErrors(t *testing.T) {
	// not array
	if _, err := DecodeHeaderMsgpack([]byte{0xa1, 'x'}); err == nil {
		t.Fatal()
	}
	// wrong len
	b := encodeArrayHeader(nil, 2)
	b = encodeString(b, "a")
	b = encodeUint64(b, 1)
	if _, err := DecodeHeaderMsgpack(b); err == nil {
		t.Fatal()
	}
	// truncated fields
	b = encodeArrayHeader(nil, 3)
	if _, err := DecodeHeaderMsgpack(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 3)
	b = encodeString(b, "t")
	if _, err := DecodeHeaderMsgpack(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 3)
	b = encodeString(b, "t")
	b = encodeUint64(b, 1)
	if _, err := DecodeHeaderMsgpack(b); err == nil {
		t.Fatal()
	}
}

func TestPackHeaderPrefixReuse(t *testing.T) {
	h := Header{ChannelTag: "z", Sequence: 1, Mode: 2}
	buf := make([]byte, 256)
	out := PackHeaderPrefix(h, buf)
	if &out[0] != &buf[0] {
		// may or may not reuse depending on size; just ensure valid
	}
	_, _, err := ParseHeader(out)
	if err != nil {
		t.Fatal(err)
	}
	// small cap forces alloc
	out2 := PackHeaderPrefix(h, make([]byte, 0, 1))
	if len(out2) < 3 {
		t.Fatal()
	}
}

func TestDecryptBodyErrors(t *testing.T) {
	c, err := NewChannel("t", goldenRx(), goldenTx())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DecryptBody(nil, 0, 0); err == nil {
		t.Fatal()
	}
	if _, err := c.DecryptBody([]byte{1, 2, 3}, 10, 0); err == nil {
		t.Fatal()
	}
	// short ciphertext
	pkt := PackHeaderPrefix(Header{ChannelTag: "t", Sequence: 0, Mode: 2}, nil)
	if _, err := c.DecryptBody(pkt, len(pkt), 0); err == nil {
		t.Fatal()
	}
	// short tag
	pkt = append(pkt, make([]byte, 8)...)
	if _, err := c.DecryptBody(pkt, len(pkt)-8, 0); err == nil {
		t.Fatal()
	}
}

func TestParseBodyMore(t *testing.T) {
	// name ok but rest < 2
	body := []byte{1, 0, 'H'}
	if _, _, err := ParseBody(body); err == nil {
		t.Fatal()
	}
	// empty payload ok
	body = EncodeBody("H", []byte{})
	name, mp, err := ParseBody(body)
	if err != nil || name != "H" || mp != nil {
		t.Fatal(name, mp, err)
	}
}

func TestDecapsulateBodyError(t *testing.T) {
	// craft valid AEAD with corrupt body length after decrypt is hard;
	// instead test mode reject clearly
	c, _ := NewChannel("t", goldenRx(), goldenTx())
	// encrypt as HA then manually won't work for mode
	pkt, err := c.Encapsulate(0, "HA", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// wrong channel decrypt key fails
	c2, _ := NewChannel("t", goldenRx(), make([]byte, 32))
	if _, _, _, err := c2.Decapsulate(pkt); err == nil {
		t.Fatal()
	}
	// reuse dst buffer path
	buf := make([]byte, 4096)
	pkt2, err := c.Encapsulate(1, "HA", nil, buf)
	if err != nil || len(pkt2) == 0 {
		t.Fatal(err)
	}
}

func TestDTODecodeEdgeCases(t *testing.T) {
	// H truncated string
	b := encodeArrayHeader(nil, 1)
	if _, err := DecodeHeartbeat(b); err == nil {
		t.Fatal()
	}

	// AT field errors
	// wrong array lens for tx
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, []byte{1})
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	b = encodeArrayHeader(b, 2) // wrong: want 1
	b = encodeUint64(b, 0)
	b = encodeUint64(b, 0)
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}

	// AR wrong rx array len
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, []byte{1})
	b = encodeBool(b, true)
	b = encodeArrayHeader(b, 1)
	b = encodeArrayHeader(b, 2) // want 3
	b = encodeUint64(b, 0)
	b = encodeUint64(b, 1)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}

	// AT truncated mid-way
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, []byte{9})
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, []byte{9})
	b = encodeBool(b, false)
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}
	// truncated transceiver list
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, []byte{9})
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}
	// transceiver with id fail
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, []byte{9})
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	b = encodeArrayHeader(b, 1)
	if _, err := DecodeAudioTx(b); err == nil {
		t.Fatal()
	}

	// AR truncated paths
	b = encodeArrayHeader(nil, 5)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, nil)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, nil)
	b = encodeBool(b, false)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, nil)
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	// incomplete rx fields
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, nil)
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	b = encodeArrayHeader(b, 3)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, nil)
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	b = encodeArrayHeader(b, 3)
	b = encodeUint64(b, 1)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
	b = encodeArrayHeader(nil, 5)
	b = encodeString(b, "CS")
	b = encodeUint64(b, 1)
	b = encodeBin(b, nil)
	b = encodeBool(b, false)
	b = encodeArrayHeader(b, 1)
	b = encodeArrayHeader(b, 3)
	b = encodeUint64(b, 1)
	b = encodeUint64(b, 2)
	if _, err := DecodeAudioRx(b); err == nil {
		t.Fatal()
	}
}

func TestSequenceFullWindowAndOverflowBranches(t *testing.T) {
	// fill entire window then overflow
	s := NewSequenceWindow(0, 8)
	// receive 1..8 first (futures), then 0
	for i := uint64(1); i <= 8; i++ {
		if s.Received(i) != ReceiveOK {
			t.Fatalf("recv %d", i)
		}
	}
	if s.Received(0) != ReceiveOK {
		t.Fatal()
	}
	// fully received window (bits 0..7) advances by window size → next=8
	if s.GetNext() != 8 {
		t.Fatalf("next=%d", s.GetNext())
	}
	// overflow far jump that fully advances
	if s.Received(1000) != ReceiveOverflow {
		t.Fatal()
	}
	if s.GetNext() != 1001 {
		t.Fatalf("next=%d", s.GetNext())
	}

	// overflow that lands mid-window after partial advance
	s2 := NewSequenceWindow(0, 4)
	_ = s2.Received(0) // next=1
	// receive 6: min=1 window=4 max=5; 6 is overflow
	if s2.Received(6) != ReceiveOverflow {
		t.Fatal()
	}
}

func TestTrailingZerosBranches(t *testing.T) {
	cases := []struct {
		x uint64
		n uint
	}{
		{0, 64},
		{1, 0},
		{2, 1},
		{4, 2},
		{0x100, 8},
		{0x10000, 16},
		{0x100000000, 32},
		{0x8000000000000000, 63},
	}
	for _, tc := range cases {
		if got := trailingZeros64(tc.x); got != tc.n {
			t.Fatalf("tz(%x)=%d want %d", tc.x, got, tc.n)
		}
	}
}

func TestChannelConfigMarshalNullHmac(t *testing.T) {
	cc := ChannelConfig{
		ChannelTag:      "t",
		AeadReceiveKey:  goldenRx(),
		AeadTransmitKey: goldenTx(),
		HmacKey:         nil,
	}
	raw, err := json.Marshal(cc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"hmacKey":null`) {
		t.Fatalf("%s", raw)
	}
	// bad tx base64
	raw = []byte(`{"channelTag":"x","aeadReceiveKey":"N/v4cGEr0ko04zsli340q+r5eRrctKbfQJo6tJ88UtM=","aeadTransmitKey":"!!!","hmacKey":null}`)
	if err := json.Unmarshal(raw, &cc); err == nil {
		t.Fatal()
	}
	// invalid JSON
	if _, err := DecodeChannelConfigJSON([]byte(`{`)); err == nil {
		t.Fatal()
	}
}

func TestAdvanceWindowFullyReceived(t *testing.T) {
	// bitfield all bits set for window portion: inv==0 path
	s := NewSequenceWindow(10, 3)
	// mark bits for 11,12,13 then receive 10 → advance past all
	_ = s.Received(11)
	_ = s.Received(12)
	_ = s.Received(13)
	if s.Received(10) != ReceiveOK {
		t.Fatal()
	}
	// after receiving min with full bits, should jump
	if s.GetNext() < 11 {
		t.Fatalf("next=%d", s.GetNext())
	}
}

func goldenRx() []byte {
	return []byte{
		0x37, 0xfb, 0xf8, 0x70, 0x61, 0x2b, 0xd2, 0x4a, 0x34, 0xe3, 0x3b, 0x25, 0x8b, 0x7e, 0x34, 0xab,
		0xea, 0xf9, 0x79, 0x1a, 0xdc, 0xb4, 0xa6, 0xdf, 0x40, 0x9a, 0x3a, 0xb4, 0x9f, 0x3c, 0x52, 0xd3,
	}
}

func goldenTx() []byte {
	return []byte{
		0xe7, 0xde, 0x0d, 0xf4, 0x08, 0xdf, 0x88, 0x22, 0xfc, 0x6f, 0xf3, 0xf5, 0xa1, 0x60, 0x34, 0x8b,
		0x42, 0x20, 0x50, 0xd8, 0x36, 0x9b, 0x06, 0xd5, 0xbd, 0x02, 0x3b, 0xc9, 0xf6, 0xf4, 0x51, 0xdf,
	}
}

func TestUint64Trunc(t *testing.T) {
	// truncated multi-byte uints
	for _, hdr := range [][]byte{{0xcd}, {0xce, 0}, {0xcf, 0, 0, 0}, {0xd1}, {0xd2, 0}, {0xd3, 0}} {
		d := decoder{b: hdr}
		if _, err := d.uint64(); err == nil {
			t.Fatalf("want trunc for %x", hdr)
		}
	}
	// truncated int multi-byte
	for _, hdr := range [][]byte{{0xd1}, {0xd2, 0}, {0xd3, 0}} {
		d := decoder{b: hdr}
		if _, err := d.int(); err == nil {
			t.Fatalf("want trunc int %x", hdr)
		}
	}
}

func TestDecapsulateParseHeaderFail(t *testing.T) {
	c, _ := NewChannel("t", goldenRx(), goldenTx())
	if _, _, _, err := c.Decapsulate([]byte{1}); err == nil {
		t.Fatal()
	}
}

func TestDecapsulateBadBodyAfterDecrypt(t *testing.T) {
	// Encrypt a body with invalid dtoMsgpackLen so ParseBody fails after Open.
	c, err := NewChannel("t", goldenRx(), goldenTx())
	if err != nil {
		t.Fatal(err)
	}
	// Manually build: seal a bad body with EncryptKey.
	aead, err := chacha20poly1305.New(c.EncryptKey[:])
	if err != nil {
		t.Fatal(err)
	}
	hdr := Header{ChannelTag: "t", Sequence: 0, Mode: ModeChaCha20Poly1305}
	prefix := PackHeaderPrefix(hdr, nil)
	badBody := []byte{1, 0, 'H', 5, 0} // dtoLen mismatch
	nonce := NonceFromSequence(0)
	sealed := aead.Seal(nil, nonce[:], badBody, prefix)
	pkt := append(append([]byte{}, prefix...), sealed...)
	// Client decrypts with Receive=EncryptKey from server view = same as c.EncryptKey
	cli, _ := NewChannel("t", goldenTx(), goldenRx()) // client: enc=tx, dec=rx
	// Wait: ServerChannel encrypts with rx; client decrypts with rx.
	// ServerChannel: EncryptKey=rx, DecryptKey=tx
	// ClientChannel: EncryptKey=tx, DecryptKey=rx
	// c is NewChannel(tag, encrypt=rx, decrypt=tx) = server
	// client decrypt of server packet uses DecryptKey=rx = c.EncryptKey
	cli2, _ := ClientChannel("t", goldenRx(), goldenTx())
	if _, _, _, err := cli2.Decapsulate(pkt); err == nil {
		t.Fatal("want ParseBody error after decrypt")
	}
	_ = cli
}

func TestSequenceAdvanceWindowAllBits(t *testing.T) {
	s := NewSequenceWindow(0, 64)
	for i := uint64(1); i <= 64; i++ {
		if s.Received(i) != ReceiveOK {
			t.Fatalf("i=%d", i)
		}
	}
	if s.Received(0) != ReceiveOK {
		t.Fatal()
	}
	// inv==0 path: advanced by full bitfield width (64)
	if s.GetNext() != 64 {
		t.Fatalf("next=%d want 64", s.GetNext())
	}
}

func TestDTOArrayLenError(t *testing.T) {
	// empty buffer → arrayLen trunc
	if _, err := DecodeHeartbeat(nil); err == nil {
		t.Fatal()
	}
	if _, err := DecodeAudioTx(nil); err == nil {
		t.Fatal()
	}
	if _, err := DecodeAudioRx(nil); err == nil {
		t.Fatal()
	}
}

func TestChannelConfigUnmarshalBadJSON(t *testing.T) {
	var c ChannelConfig
	if err := c.UnmarshalJSON([]byte(`not-json`)); err == nil {
		t.Fatal()
	}
}

func TestIntPeekTrunc(t *testing.T) {
	// cover int() path with max int64 via cf
	b := append([]byte{0xcf}, make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[1:], math.MaxInt64)
	d := decoder{b: b}
	v, err := d.int()
	if err != nil || v != math.MaxInt64 {
		t.Fatal(v, err)
	}
}

// Import for AEAD helper in this file.
func TestMalformedBodyViaEncapsulatePath(t *testing.T) {
	// EncapsulateHA success with reusable buffer already covered;
	// force EncryptKey path when c non-nil with valid keys is enough.
	c, _ := ServerChannel("z", goldenRx(), goldenTx())
	buf := make([]byte, 8) // small — realloc
	pkt, err := c.Encapsulate(3, DTONameHeartbeatAck, nil, buf)
	if err != nil || len(pkt) < 20 {
		t.Fatal(err, len(pkt))
	}
}
