package afvprotocol_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

// Golden from AFV-Native test/cryptodto/test_ChannelConfig.cpp
var (
	goldenRxKey = []byte{
		0x37, 0xfb, 0xf8, 0x70, 0x61, 0x2b, 0xd2, 0x4a, 0x34, 0xe3, 0x3b, 0x25, 0x8b, 0x7e, 0x34, 0xab,
		0xea, 0xf9, 0x79, 0x1a, 0xdc, 0xb4, 0xa6, 0xdf, 0x40, 0x9a, 0x3a, 0xb4, 0x9f, 0x3c, 0x52, 0xd3,
	}
	goldenTxKey = []byte{
		0xe7, 0xde, 0x0d, 0xf4, 0x08, 0xdf, 0x88, 0x22, 0xfc, 0x6f, 0xf3, 0xf5, 0xa1, 0x60, 0x34, 0x8b,
		0x42, 0x20, 0x50, 0xd8, 0x36, 0x9b, 0x06, 0xd5, 0xbd, 0x02, 0x3b, 0xc9, 0xf6, 0xf4, 0x51, 0xdf,
	}
)

func TestChannelConfigAFVNativeGolden(t *testing.T) {
	path := filepath.Join("testdata", "channelconfig_afvnative.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	cc, err := afvprotocol.DecodeChannelConfigJSON(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cc.ChannelTag != "abc123" {
		t.Errorf("channelTag=%q want abc123", cc.ChannelTag)
	}
	if !bytes.Equal(cc.AeadReceiveKey, goldenRxKey) {
		t.Errorf("rx key mismatch\n got %x\nwant %x", cc.AeadReceiveKey, goldenRxKey)
	}
	if !bytes.Equal(cc.AeadTransmitKey, goldenTxKey) {
		t.Errorf("tx key mismatch\n got %x\nwant %x", cc.AeadTransmitKey, goldenTxKey)
	}
	if cc.HmacKey == nil || *cc.HmacKey != "totallyFakeHmacKey" {
		t.Errorf("hmacKey=%v want totallyFakeHmacKey", cc.HmacKey)
	}

	// Round-trip JSON preserves base64 strings.
	out, err := afvprotocol.EncodeChannelConfigJSON(cc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	cc2, err := afvprotocol.DecodeChannelConfigJSON(out)
	if err != nil {
		t.Fatalf("redecode: %v", err)
	}
	if !bytes.Equal(cc2.AeadReceiveKey, goldenRxKey) || !bytes.Equal(cc2.AeadTransmitKey, goldenTxKey) {
		t.Fatal("round-trip keys mismatch")
	}

	// Explicit base64 strings from design doc.
	wantRxB64 := "N/v4cGEr0ko04zsli340q+r5eRrctKbfQJo6tJ88UtM="
	wantTxB64 := "594N9AjfiCL8b/P1oWA0i0IgUNg2mwbVvQI7yfb0Ud8="
	if got := base64.StdEncoding.EncodeToString(goldenRxKey); got != wantRxB64 {
		t.Errorf("rx b64 %q want %q", got, wantRxB64)
	}
	if got := base64.StdEncoding.EncodeToString(goldenTxKey); got != wantTxB64 {
		t.Errorf("tx b64 %q want %q", got, wantTxB64)
	}
}

func TestChannelConfigBadKeyLen(t *testing.T) {
	raw := []byte(`{"channelTag":"x","aeadReceiveKey":"YWI=","aeadTransmitKey":"YWI=","hmacKey":null}`)
	_, err := afvprotocol.DecodeChannelConfigJSON(raw)
	if err == nil {
		t.Fatal("expected error for short keys")
	}
}

func TestChannelConfigBadBase64(t *testing.T) {
	raw := []byte(`{"channelTag":"x","aeadReceiveKey":"!!!","aeadTransmitKey":"N/v4cGEr0ko04zsli340q+r5eRrctKbfQJo6tJ88UtM=","hmacKey":null}`)
	_, err := afvprotocol.DecodeChannelConfigJSON(raw)
	if err == nil {
		t.Fatal("expected base64 error")
	}
}
