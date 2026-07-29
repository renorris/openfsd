package vpilotconfig

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	k1 := DeriveKey()
	k2 := DeriveKey()
	if len(k1) != 24 {
		t.Fatalf("key length %d want 24", len(k1))
	}
	if !bytes.Equal(k1, k2) {
		t.Fatal("DeriveKey not deterministic")
	}
	// key24 = key16 || key16[0:8]
	if !bytes.Equal(k1[16:24], k1[0:8]) {
		t.Fatalf("key extension mismatch: %x vs %x", k1[16:24], k1[0:8])
	}
	// Fixed production golden (MD5 of ConfigGUID || first 8 of that MD5).
	// MD5(5575ac09-f2de-4a1e-808b-e3398e17f8bf) = 9bf2bf12df6e3fb45c0db019e1982a10
	wantHex := "9bf2bf12df6e3fb45c0db019e1982a109bf2bf12df6e3fb4"
	gotHex := fmt.Sprintf("%x", k1)
	if gotHex != wantHex {
		t.Fatalf("DeriveKey hex:\n got %s\nwant %s", gotHex, wantHex)
	}
	// TripleDES accepts the key.
	if _, err := des.NewTripleDESCipher(k1); err != nil {
		t.Fatal(err)
	}
}

// Production ciphertext goldens (3DES-ECB-PKCS7 + std Base64), cross-checked
// against OpenSSL. Lock vPilot crypto so MD5/key/ECB/PKCS7 cannot soft-pass.
func TestEncryptGoldens(t *testing.T) {
	cases := []struct {
		plain string
		b64   string
	}{
		{"http://status.vatsim.net/", "pZ9u441bE4a2NCGgqMxKNvwAIy0qEA+AwXB8c3sV90c="},
		{"", "QV3c4DoqB1Y="},
		{"AUTOMATIC|fsd.connect.vatsim.net", "vN0yvTPHb8iCqOBSDgJOad3JH+XEldSS/AAjLSoQD4BBXdzgOioHVg=="},
	}
	for _, tc := range cases {
		got, err := Encrypt(tc.plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", tc.plain, err)
		}
		if got != tc.b64 {
			t.Fatalf("Encrypt(%q):\n got %s\nwant %s", tc.plain, got, tc.b64)
		}
		// Decrypt golden back to plaintext.
		plain, err := Decrypt(tc.b64)
		if err != nil {
			t.Fatalf("Decrypt golden for %q: %v", tc.plain, err)
		}
		if plain != tc.plain {
			t.Fatalf("Decrypt golden: got %q want %q", plain, tc.plain)
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	cases := []string{
		"",
		"a",
		"http://status.vatsim.net/",
		"AUTOMATIC|fsd.connect.vatsim.net",
		"OPENFSD|fsd.ex.co",
		"https://openfsd.example/api/v1/data/status.txt",
		// Multi-block ( > 8 bytes).
		strings.Repeat("x", 17),
		// Exact block boundary plaintext (multiple of 8) → full pad block.
		strings.Repeat("y", 8),
		strings.Repeat("z", 16),
	}
	for _, s := range cases {
		enc, err := Encrypt(s)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", s, err)
		}
		if enc == "" {
			t.Fatalf("Encrypt(%q) empty ciphertext", s)
		}
		// Base64 should decode cleanly.
		if _, err := base64.StdEncoding.DecodeString(enc); err != nil {
			t.Fatalf("Encrypt base64 invalid for %q: %v", s, err)
		}
		got, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", s, err)
		}
		if got != s {
			t.Fatalf("round-trip: got %q want %q", got, s)
		}
	}
}

func TestPKCS7PadUnpad(t *testing.T) {
	blockSize := 8
	// Empty → full block of 0x08.
	p := pkcs7Pad(nil, blockSize)
	if len(p) != 8 {
		t.Fatalf("empty pad len %d", len(p))
	}
	for _, b := range p {
		if b != 0x08 {
			t.Fatalf("empty pad bytes: %x", p)
		}
	}
	u, err := pkcs7Unpad(p, blockSize)
	if err != nil || len(u) != 0 {
		t.Fatalf("unpad empty: %q %v", u, err)
	}

	// 1 byte → pad 7.
	p = pkcs7Pad([]byte{0x41}, blockSize)
	if len(p) != 8 || p[0] != 0x41 || p[7] != 0x07 {
		t.Fatalf("1-byte pad: %x", p)
	}
	u, err = pkcs7Unpad(p, blockSize)
	if err != nil || string(u) != "A" {
		t.Fatalf("unpad 1: %q %v", u, err)
	}

	// Exact multiple of block → pad full block.
	p = pkcs7Pad([]byte("12345678"), blockSize)
	if len(p) != 16 {
		t.Fatalf("exact block pad len %d", len(p))
	}
	u, err = pkcs7Unpad(p, blockSize)
	if err != nil || string(u) != "12345678" {
		t.Fatalf("unpad exact: %q %v", u, err)
	}
}

func TestPKCS7UnpadErrors(t *testing.T) {
	// Empty.
	if _, err := pkcs7Unpad(nil, 8); !errors.Is(err, ErrBadPadding) {
		t.Fatalf("nil: %v", err)
	}
	// Bad length.
	if _, err := pkcs7Unpad([]byte{1, 2, 3}, 8); !errors.Is(err, ErrBadPadding) {
		t.Fatalf("short: %v", err)
	}
	// Pad value 0.
	b := []byte{1, 2, 3, 4, 5, 6, 7, 0}
	if _, err := pkcs7Unpad(b, 8); !errors.Is(err, ErrBadPadding) {
		t.Fatalf("pad0: %v", err)
	}
	// Pad value > block size.
	b = []byte{1, 2, 3, 4, 5, 6, 7, 9}
	if _, err := pkcs7Unpad(b, 8); !errors.Is(err, ErrBadPadding) {
		t.Fatalf("pad9: %v", err)
	}
	// Inconsistent pad bytes.
	b = []byte{1, 2, 3, 4, 5, 6, 0x02, 0x03}
	if _, err := pkcs7Unpad(b, 8); !errors.Is(err, ErrBadPadding) {
		t.Fatalf("inconsistent: %v", err)
	}
}

func TestDecryptErrors(t *testing.T) {
	if _, err := Decrypt("!!!not-base64!!!"); !errors.Is(err, ErrBadBase64) {
		t.Fatalf("bad b64: %v", err)
	}
	if _, err := Decrypt(""); !errors.Is(err, ErrBadCipher) {
		t.Fatalf("empty: %v", err)
	}
	// Valid base64 but wrong length (not multiple of 8).
	bad := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	if _, err := Decrypt(bad); !errors.Is(err, ErrBadCipher) {
		t.Fatalf("bad len: %v", err)
	}
	// Valid length but garbage padding after decrypt.
	enc, err := Encrypt("hello")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := Decrypt(tampered); !errors.Is(err, ErrBadPadding) {
		t.Fatalf("tampered: %v", err)
	}
}

func TestCipherInitError(t *testing.T) {
	old := newTripleDESCipher
	newTripleDESCipher = func(key []byte) (cipher.Block, error) {
		return nil, errors.New("forced cipher fail")
	}
	t.Cleanup(func() { newTripleDESCipher = old })

	if _, err := Encrypt("x"); err == nil || !strings.Contains(err.Error(), "triple-des") {
		t.Fatalf("Encrypt cipher err: %v", err)
	}
	// Valid-looking base64 of 8 zero bytes so we pass length checks after inject fails.
	if _, err := Decrypt(base64.StdEncoding.EncodeToString(make([]byte, 8))); err == nil || !strings.Contains(err.Error(), "triple-des") {
		t.Fatalf("Decrypt cipher err: %v", err)
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	cfg := &Config{
		NetworkStatusURL: "http://status.vatsim.net/",
		CachedServers:    []string{"AUTOMATIC|fsd.connect.vatsim.net"},
		NetworkLogin:     "USER123",
		NetworkPassword:  "secret",
	}
	xmlBytes, err := Format(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(xmlBytes, []byte("<vPilotConfig>")) {
		t.Fatalf("missing root: %s", xmlBytes)
	}
	// Ciphertext should not contain plaintext status URL.
	if bytes.Contains(xmlBytes, []byte("status.vatsim.net")) {
		t.Fatal("plaintext status leaked into XML")
	}

	got, err := Parse(xmlBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got.NetworkStatusURL != cfg.NetworkStatusURL {
		t.Fatalf("status: %q", got.NetworkStatusURL)
	}
	if len(got.CachedServers) != 1 || got.CachedServers[0] != cfg.CachedServers[0] {
		t.Fatalf("servers: %#v", got.CachedServers)
	}
	if got.NetworkLogin != cfg.NetworkLogin || got.NetworkPassword != cfg.NetworkPassword {
		t.Fatalf("creds: %q %q", got.NetworkLogin, got.NetworkPassword)
	}
}

func TestCachedServersMultiLine(t *testing.T) {
	cfg := &Config{
		NetworkStatusURL: "http://status.example/",
		CachedServers: []string{
			"OPENFSD|fsd.ex.co",
			"BACKUP|fsd2.ex.co:6809",
		},
	}
	xmlBytes, err := Format(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(xmlBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CachedServers) != 2 {
		t.Fatalf("servers: %#v", got.CachedServers)
	}
	if got.CachedServers[0] != "OPENFSD|fsd.ex.co" || got.CachedServers[1] != "BACKUP|fsd2.ex.co:6809" {
		t.Fatalf("servers: %#v", got.CachedServers)
	}

	// Plaintext join is newline-separated.
	plain := joinServers(cfg.CachedServers)
	if plain != "OPENFSD|fsd.ex.co\nBACKUP|fsd2.ex.co:6809" {
		t.Fatalf("join: %q", plain)
	}
}

func TestFormatEmptyServers(t *testing.T) {
	cfg := &Config{NetworkStatusURL: "http://x/"}
	xmlBytes, err := Format(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(xmlBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CachedServers) != 0 {
		t.Fatalf("%#v", got.CachedServers)
	}
	if joinServers(nil) != "" || joinServers([]string{}) != "" {
		t.Fatal("join empty")
	}
}

func TestApplyEndpoints(t *testing.T) {
	cfg := &Config{
		NetworkStatusURL: "http://old/",
		CachedServers:    []string{"OLD|old.host"},
		NetworkLogin:     "keep-or-clear",
		NetworkPassword:  "pw",
	}
	ApplyEndpoints(cfg, "https://new.example/status.txt", []string{"OPENFSD|fsd.ex.co"}, true)
	if cfg.NetworkStatusURL != "https://new.example/status.txt" {
		t.Fatal(cfg.NetworkStatusURL)
	}
	if len(cfg.CachedServers) != 1 || cfg.CachedServers[0] != "OPENFSD|fsd.ex.co" {
		t.Fatalf("%#v", cfg.CachedServers)
	}
	if cfg.NetworkLogin != "" || cfg.NetworkPassword != "" {
		t.Fatalf("creds not cleared: %q %q", cfg.NetworkLogin, cfg.NetworkPassword)
	}

	// clearCreds false preserves login.
	cfg.NetworkLogin = "u"
	cfg.NetworkPassword = "p"
	ApplyEndpoints(cfg, "http://x/", nil, false)
	if cfg.NetworkLogin != "u" || cfg.NetworkPassword != "p" {
		t.Fatal("creds should be preserved")
	}
	if cfg.CachedServers != nil {
		t.Fatalf("nil servers: %#v", cfg.CachedServers)
	}

	// nil cfg is no-op.
	ApplyEndpoints(nil, "x", nil, true)
}

func TestParseEmptyAndErrors(t *testing.T) {
	if _, err := Parse(nil); !errors.Is(err, ErrEmptyConfig) {
		t.Fatalf("nil: %v", err)
	}
	if _, err := Parse([]byte("   ")); !errors.Is(err, ErrEmptyConfig) {
		t.Fatalf("blank: %v", err)
	}
	if _, err := Parse([]byte("<notxml")); !errors.Is(err, ErrBadXML) {
		t.Fatalf("bad xml: %v", err)
	}
	// Wrong root element.
	if _, err := Parse([]byte(`<Other></Other>`)); !errors.Is(err, ErrBadXML) {
		t.Fatalf("wrong root: %v", err)
	}

	// Empty elements → empty plaintext.
	emptyXML := []byte(`<?xml version="1.0"?><vPilotConfig>
  <NetworkStatusURL></NetworkStatusURL>
  <CachedServers></CachedServers>
  <NetworkLogin></NetworkLogin>
  <NetworkPassword></NetworkPassword>
</vPilotConfig>`)
	cfg, err := Parse(emptyXML)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkStatusURL != "" || len(cfg.CachedServers) != 0 {
		t.Fatalf("%#v", cfg)
	}
}

func TestParseFieldDecryptErrors(t *testing.T) {
	// Build XML with one bad field at a time (others empty).
	fields := []string{"NetworkStatusURL", "CachedServers", "NetworkLogin", "NetworkPassword"}
	for _, badField := range fields {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><vPilotConfig>`)
		for _, f := range fields {
			if f == badField {
				b.WriteString("<" + f + ">!!!bad!!!</" + f + ">")
			} else {
				b.WriteString("<" + f + "></" + f + ">")
			}
		}
		b.WriteString(`</vPilotConfig>`)
		_, err := Parse([]byte(b.String()))
		if !errors.Is(err, ErrBadBase64) {
			t.Fatalf("%s: want ErrBadBase64, got %v", badField, err)
		}
		if !strings.Contains(err.Error(), badField) {
			t.Fatalf("%s: error should name field: %v", badField, err)
		}
	}
}

func TestFormatEncryptErrors(t *testing.T) {
	// Fail on the Nth NewTripleDESCipher call (each Encrypt uses one).
	// Format order: Status, Servers, Login, Password.
	wantNames := []string{"NetworkStatusURL", "CachedServers", "NetworkLogin", "NetworkPassword"}
	for failAt, name := range wantNames {
		t.Run(name, func(t *testing.T) {
			old := newTripleDESCipher
			n := 0
			newTripleDESCipher = func(key []byte) (cipher.Block, error) {
				if n == failAt {
					n++
					return nil, errors.New("forced")
				}
				n++
				return old(key)
			}
			t.Cleanup(func() { newTripleDESCipher = old })

			_, err := Format(&Config{
				NetworkStatusURL: "s",
				CachedServers:    []string{"A|h"},
				NetworkLogin:     "u",
				NetworkPassword:  "p",
			})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("failAt=%d want field %s, got %v", failAt, name, err)
			}
		})
	}
}

func TestFormatNil(t *testing.T) {
	if _, err := Format(nil); !errors.Is(err, ErrEmptyConfig) {
		t.Fatalf("%v", err)
	}
}

func TestSplitServers(t *testing.T) {
	if splitServers("") != nil {
		t.Fatal("empty")
	}
	if splitServers("  \n  ") != nil {
		t.Fatal("ws")
	}
	got := splitServers("A|h1\r\nB|h2\n\nC|h3\n")
	if len(got) != 3 || got[0] != "A|h1" || got[2] != "C|h3" {
		t.Fatalf("%#v", got)
	}
}

func TestConfigGUIDConstant(t *testing.T) {
	if ConfigGUID != "5575ac09-f2de-4a1e-808b-e3398e17f8bf" {
		t.Fatal(ConfigGUID)
	}
}

// Stock-like values: encrypt known plaintexts and ensure deterministic
// ciphertext (same plaintext → same base64 with ECB).
func TestEncryptDeterministic(t *testing.T) {
	a, err := Encrypt("http://status.vatsim.net/")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt("http://status.vatsim.net/")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("ECB encrypt should be deterministic")
	}
}
