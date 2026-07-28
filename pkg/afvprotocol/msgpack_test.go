package afvprotocol

import (
	"math"
	"testing"
)

// Internal tests for msgpack edge paths (coverage).

func TestMsgpackEncodeDecodeRoundtrips(t *testing.T) {
	// string lengths: fixstr, str8, str16
	for _, s := range []string{"", "hi", string(make([]byte, 40)), string(make([]byte, 300))} {
		b := encodeString(nil, s)
		d := decoder{b: b}
		got, err := d.string()
		if err != nil || got != s {
			t.Fatalf("string len=%d err=%v", len(s), err)
		}
	}

	// uint sizes
	for _, v := range []uint64{0, 1, 0x80, 0x100, 0x10000, 0x100000000, math.MaxUint64} {
		b := encodeUint64(nil, v)
		d := decoder{b: b}
		got, err := d.uint64()
		if err != nil || got != v {
			t.Fatalf("uint %d got %d err %v", v, got, err)
		}
	}

	// negative ints
	for _, v := range []int64{-1, -32, -33, -128, -129, -32768, -32769, math.MinInt32, math.MinInt32 - 1} {
		b := encodeInt(nil, v)
		d := decoder{b: b}
		got, err := d.int()
		if err != nil || got != v {
			t.Fatalf("int %d got %d err %v", v, got, err)
		}
	}

	// bool / bin / float
	for _, bv := range []bool{true, false} {
		b := encodeBool(nil, bv)
		d := decoder{b: b}
		got, err := d.bool()
		if err != nil || got != bv {
			t.Fatal(bv, err)
		}
	}
	bin := make([]byte, 300)
	for i := range bin {
		bin[i] = byte(i)
	}
	b := encodeBin(nil, bin)
	d := decoder{b: b}
	gotBin, err := d.bin()
	if err != nil || len(gotBin) != 300 {
		t.Fatal(err, len(gotBin))
	}
	b = encodeFloat32(nil, 0.5)
	d = decoder{b: b}
	f, err := d.float32()
	if err != nil || f != 0.5 {
		t.Fatal(f, err)
	}

	// array headers
	for _, n := range []int{0, 1, 15, 16, 0xffff} {
		b := encodeArrayHeader(nil, n)
		d := decoder{b: b}
		got, err := d.arrayLen()
		if err != nil || got != n {
			t.Fatalf("array n=%d got=%d err=%v", n, got, err)
		}
	}
}

func TestMsgpackErrors(t *testing.T) {
	d := decoder{b: nil}
	if _, err := d.u8(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}} // nil
	if _, err := d.arrayLen(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}}
	if _, err := d.string(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}}
	if _, err := d.uint64(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}}
	if _, err := d.int(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}}
	if _, err := d.bool(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}}
	if _, err := d.bin(); err == nil {
		t.Fatal()
	}
	d = decoder{b: []byte{0xc0}}
	if _, err := d.float32(); err == nil {
		t.Fatal()
	}
}

func TestTrailingZeros(t *testing.T) {
	if trailingZeros64(0) != 64 {
		t.Fatal()
	}
	if trailingZeros64(1) != 0 {
		t.Fatal()
	}
	if trailingZeros64(8) != 3 {
		t.Fatal()
	}
}
