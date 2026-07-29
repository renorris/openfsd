// Package vpilotconfig provides pure vPilot config field crypto (3DES-ECB-PKCS7)
// and XML helpers for vPilotConfig.xml. It depends only on the Go standard library.
//
//	guid  = "5575ac09-f2de-4a1e-808b-e3398e17f8bf"
//	key16 = MD5(guid)
//	key24 = key16 || key16[0:8]
//	field = Base64(3DES-ECB-PKCS7(plaintext, key24))
//
// CachedServers plaintext is newline-separated "NAME|host" (or "NAME|host:port")
// entries before encryption.
package vpilotconfig

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// ConfigGUID is the fixed GUID whose MD5 seeds the 3DES key used by vPilot
// to obfuscate config fields (NetworkStatusURL, CachedServers, credentials).
const ConfigGUID = "5575ac09-f2de-4a1e-808b-e3398e17f8bf"

// Sentinel errors.
var (
	ErrBadBase64   = errors.New("vpilotconfig: bad base64")
	ErrBadCipher   = errors.New("vpilotconfig: bad ciphertext")
	ErrBadPadding  = errors.New("vpilotconfig: bad PKCS7 padding")
	ErrEmptyConfig = errors.New("vpilotconfig: empty config")
	ErrBadXML      = errors.New("vpilotconfig: bad XML")
)

// newTripleDESCipher is des.NewTripleDESCipher; overridable in tests.
var newTripleDESCipher = des.NewTripleDESCipher

// Config holds the decrypted network-related fields of vPilotConfig.xml.
type Config struct {
	NetworkStatusURL string
	// CachedServers is the list of "NAME|host" or "NAME|host:port" entries.
	// Plaintext form is newline-separated before encryption.
	CachedServers   []string
	NetworkLogin    string
	NetworkPassword string
}

// DeriveKey returns the 24-byte 3DES key: MD5(ConfigGUID) || first 8 of that MD5.
func DeriveKey() []byte {
	sum := md5.Sum([]byte(ConfigGUID))
	key := make([]byte, 24)
	copy(key, sum[:])
	copy(key[16:], sum[:8])
	return key
}

// Encrypt obfuscates plaintext as Base64(3DES-ECB-PKCS7(...)).
func Encrypt(plaintext string) (string, error) {
	block, err := newTripleDESCipher(DeriveKey())
	if err != nil {
		return "", fmt.Errorf("vpilotconfig: triple-des: %w", err)
	}
	plain := pkcs7Pad([]byte(plaintext), block.BlockSize())
	dst := make([]byte, len(plain))
	ecbEncrypt(block, dst, plain)
	return base64.StdEncoding.EncodeToString(dst), nil
}

// Decrypt reverses Encrypt. ciphertextB64 is standard Base64 of the ciphertext.
func Decrypt(ciphertextB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadBase64, err)
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("%w: empty ciphertext", ErrBadCipher)
	}
	block, err := newTripleDESCipher(DeriveKey())
	if err != nil {
		return "", fmt.Errorf("vpilotconfig: triple-des: %w", err)
	}
	if len(raw)%block.BlockSize() != 0 {
		return "", fmt.Errorf("%w: length %d not multiple of block size %d", ErrBadCipher, len(raw), block.BlockSize())
	}
	dst := make([]byte, len(raw))
	ecbDecrypt(block, dst, raw)
	unpadded, err := pkcs7Unpad(dst, block.BlockSize())
	if err != nil {
		return "", err
	}
	return string(unpadded), nil
}

// ApplyEndpoints sets NetworkStatusURL and CachedServers, and when clearCreds
// is true clears NetworkLogin and NetworkPassword.
func ApplyEndpoints(cfg *Config, statusURL string, servers []string, clearCreds bool) {
	if cfg == nil {
		return
	}
	cfg.NetworkStatusURL = statusURL
	if servers == nil {
		cfg.CachedServers = nil
	} else {
		cfg.CachedServers = append([]string(nil), servers...)
	}
	if clearCreds {
		cfg.NetworkLogin = ""
		cfg.NetworkPassword = ""
	}
}

// xmlRoot is used for encoding/decoding the known network fields.
type xmlRoot struct {
	XMLName          xml.Name `xml:"vPilotConfig"`
	NetworkStatusURL string   `xml:"NetworkStatusURL"`
	CachedServers    string   `xml:"CachedServers"`
	NetworkLogin     string   `xml:"NetworkLogin"`
	NetworkPassword  string   `xml:"NetworkPassword"`
}

// Parse decrypts obfuscated fields from vPilotConfig.xml bytes.
// Empty element text is treated as empty plaintext (not an error).
// Fields that fail to decrypt return an error.
func Parse(data []byte) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyConfig
	}
	var root xmlRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadXML, err)
	}

	cfg := &Config{}
	var err error
	if cfg.NetworkStatusURL, err = decryptField(root.NetworkStatusURL); err != nil {
		return nil, fmt.Errorf("NetworkStatusURL: %w", err)
	}
	var serversPlain string
	if serversPlain, err = decryptField(root.CachedServers); err != nil {
		return nil, fmt.Errorf("CachedServers: %w", err)
	}
	cfg.CachedServers = splitServers(serversPlain)
	if cfg.NetworkLogin, err = decryptField(root.NetworkLogin); err != nil {
		return nil, fmt.Errorf("NetworkLogin: %w", err)
	}
	if cfg.NetworkPassword, err = decryptField(root.NetworkPassword); err != nil {
		return nil, fmt.Errorf("NetworkPassword: %w", err)
	}
	return cfg, nil
}

// Format re-encrypts known fields and writes a minimal vPilotConfig.xml document.
// Synthetic fixtures only use the known fields; callers that need full DOM
// fidelity should merge externally. Output uses UTF-8 XML declaration.
func Format(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, ErrEmptyConfig
	}
	statusEnc, err := Encrypt(cfg.NetworkStatusURL)
	if err != nil {
		return nil, fmt.Errorf("NetworkStatusURL: %w", err)
	}
	serversEnc, err := Encrypt(joinServers(cfg.CachedServers))
	if err != nil {
		return nil, fmt.Errorf("CachedServers: %w", err)
	}
	loginEnc, err := Encrypt(cfg.NetworkLogin)
	if err != nil {
		return nil, fmt.Errorf("NetworkLogin: %w", err)
	}
	passEnc, err := Encrypt(cfg.NetworkPassword)
	if err != nil {
		return nil, fmt.Errorf("NetworkPassword: %w", err)
	}
	root := xmlRoot{
		NetworkStatusURL: statusEnc,
		CachedServers:    serversEnc,
		NetworkLogin:     loginEnc,
		NetworkPassword:  passEnc,
	}
	// xml.MarshalIndent only fails for unsupported types; xmlRoot is fixed.
	body, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("vpilotconfig: marshal: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.Write(body)
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// decryptField treats empty text as empty plaintext; otherwise Decrypt.
func decryptField(enc string) (string, error) {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return "", nil
	}
	return Decrypt(enc)
}

// splitServers splits newline-separated CachedServers plaintext.
// Empty lines are dropped. A single entry without newline is fine.
func splitServers(plain string) []string {
	plain = strings.ReplaceAll(plain, "\r\n", "\n")
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return nil
	}
	parts := strings.Split(plain, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinServers joins CachedServers with newlines for encryption.
func joinServers(servers []string) string {
	if len(servers) == 0 {
		return ""
	}
	return strings.Join(servers, "\n")
}

// pkcs7Pad pads b to a multiple of blockSize (PKCS#7).
// When len(b) is already a multiple of blockSize, a full block of padding is added
// (pad value == blockSize), including for empty input.
func pkcs7Pad(b []byte, blockSize int) []byte {
	pad := blockSize - (len(b) % blockSize)
	// When len%blockSize == 0, pad evaluates to blockSize (full block).
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

// pkcs7Unpad removes and validates PKCS#7 padding.
func pkcs7Unpad(b []byte, blockSize int) ([]byte, error) {
	if len(b) == 0 || len(b)%blockSize != 0 {
		return nil, fmt.Errorf("%w: length %d", ErrBadPadding, len(b))
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > blockSize || pad > len(b) {
		return nil, fmt.Errorf("%w: pad value %d", ErrBadPadding, pad)
	}
	for i := len(b) - pad; i < len(b); i++ {
		if b[i] != byte(pad) {
			return nil, fmt.Errorf("%w: inconsistent pad byte", ErrBadPadding)
		}
	}
	return b[:len(b)-pad], nil
}

// ecbEncrypt encrypts src into dst using ECB (len must be multiple of block size).
func ecbEncrypt(block cipher.Block, dst, src []byte) {
	bs := block.BlockSize()
	for i := 0; i < len(src); i += bs {
		block.Encrypt(dst[i:i+bs], src[i:i+bs])
	}
}

// ecbDecrypt decrypts src into dst using ECB.
func ecbDecrypt(block cipher.Block, dst, src []byte) {
	bs := block.BlockSize()
	for i := 0; i < len(src); i += bs {
		block.Decrypt(dst[i:i+bs], src[i:i+bs])
	}
}
