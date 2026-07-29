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
//
// # XML rewrite
//
// Prefer Rewrite (or ParseDocument + Document.Format) when updating an existing
// install config: unknown elements and attributes are preserved. Format alone
// emits a minimal four-field document and is for synthetic fixtures only.
//
// # encoding/xml fidelity limits
//
// Round-trip uses encoding/xml with a generic element tree (xml:",any"). That
// preserves unknown elements and their attributes for normal vPilot configs,
// but does **not** preserve:
//   - XML comments (<!-- ... -->)
//   - processing instructions
//   - exact original whitespace, indentation, or attribute order
//   - document type declarations / entity expansions beyond the stdlib decoder
//
// Acceptable for Phase 0 (vPilot writes machine-generated field values). Operators
// who hand-edit vPilotConfig.xml with comments should expect comments to be
// dropped on Apply; re-add comments after inject if needed.
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

// Document is a parsed vPilotConfig.xml with decrypted known fields and a
// generic element tree so Format can preserve unknown elements.
type Document struct {
	Config Config
	tree   *genericXML
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

// genericXML is a DOM-ish tree that preserves unknown elements/attributes.
type genericXML struct {
	XMLName xml.Name
	Attrs   []xml.Attr   `xml:",any,attr"`
	Nodes   []genericXML `xml:",any"`
	Text    string       `xml:",chardata"`
}

// Parse decrypts obfuscated fields from vPilotConfig.xml bytes.
// Empty element text is treated as empty plaintext (not an error).
// Fields that fail to decrypt return an error. Unknown elements are ignored
// (use ParseDocument / Rewrite to preserve them on write).
func Parse(data []byte) (*Config, error) {
	doc, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	cfg := doc.Config
	return &cfg, nil
}

// ParseDocument parses full XML into a Document (config + tree for rewrite).
func ParseDocument(data []byte) (*Document, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyConfig
	}
	var root genericXML
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadXML, err)
	}
	if localName(root.XMLName) != "vPilotConfig" {
		return nil, fmt.Errorf("%w: root element %q (want vPilotConfig)", ErrBadXML, localName(root.XMLName))
	}
	cfg, err := configFromTree(&root)
	if err != nil {
		return nil, err
	}
	return &Document{Config: *cfg, tree: &root}, nil
}

// Format re-encrypts known fields and writes a **minimal** vPilotConfig.xml
// (four network fields only). Prefer Rewrite when updating a real install.
func Format(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, ErrEmptyConfig
	}
	enc, err := encryptConfigFields(cfg)
	if err != nil {
		return nil, err
	}
	root := genericXML{
		XMLName: xml.Name{Local: "vPilotConfig"},
		Nodes: []genericXML{
			textChild("NetworkStatusURL", enc.status),
			textChild("CachedServers", enc.servers),
			textChild("NetworkLogin", enc.login),
			textChild("NetworkPassword", enc.pass),
		},
	}
	return marshalXML(&root)
}

// Format on Document re-encrypts known fields into the preserved tree and
// writes the full document (unknown elements kept).
func (d *Document) Format() ([]byte, error) {
	if d == nil || d.tree == nil {
		return nil, ErrEmptyConfig
	}
	if err := applyConfigToTree(d.tree, &d.Config); err != nil {
		return nil, err
	}
	return marshalXML(d.tree)
}

// Rewrite parses original XML, applies cfg known fields, and returns XML that
// preserves unknown elements/attributes from original. Fails if original is
// not well-formed or known encrypted fields cannot be decrypted on parse.
func Rewrite(original []byte, cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, ErrEmptyConfig
	}
	doc, err := ParseDocument(original)
	if err != nil {
		return nil, err
	}
	doc.Config = *cfg
	return doc.Format()
}

type encryptedFields struct {
	status, servers, login, pass string
}

func encryptConfigFields(cfg *Config) (encryptedFields, error) {
	var e encryptedFields
	var err error
	if e.status, err = Encrypt(cfg.NetworkStatusURL); err != nil {
		return e, fmt.Errorf("NetworkStatusURL: %w", err)
	}
	if e.servers, err = Encrypt(joinServers(cfg.CachedServers)); err != nil {
		return e, fmt.Errorf("CachedServers: %w", err)
	}
	if e.login, err = Encrypt(cfg.NetworkLogin); err != nil {
		return e, fmt.Errorf("NetworkLogin: %w", err)
	}
	if e.pass, err = Encrypt(cfg.NetworkPassword); err != nil {
		return e, fmt.Errorf("NetworkPassword: %w", err)
	}
	return e, nil
}

func applyConfigToTree(root *genericXML, cfg *Config) error {
	enc, err := encryptConfigFields(cfg)
	if err != nil {
		return err
	}
	setOrAppendTextChild(root, "NetworkStatusURL", enc.status)
	setOrAppendTextChild(root, "CachedServers", enc.servers)
	setOrAppendTextChild(root, "NetworkLogin", enc.login)
	setOrAppendTextChild(root, "NetworkPassword", enc.pass)
	return nil
}

func configFromTree(root *genericXML) (*Config, error) {
	cfg := &Config{}
	var err error
	statusEnc := childText(root, "NetworkStatusURL")
	if cfg.NetworkStatusURL, err = decryptField(statusEnc); err != nil {
		return nil, fmt.Errorf("NetworkStatusURL: %w", err)
	}
	serversEnc := childText(root, "CachedServers")
	var serversPlain string
	if serversPlain, err = decryptField(serversEnc); err != nil {
		return nil, fmt.Errorf("CachedServers: %w", err)
	}
	cfg.CachedServers = splitServers(serversPlain)
	if cfg.NetworkLogin, err = decryptField(childText(root, "NetworkLogin")); err != nil {
		return nil, fmt.Errorf("NetworkLogin: %w", err)
	}
	if cfg.NetworkPassword, err = decryptField(childText(root, "NetworkPassword")); err != nil {
		return nil, fmt.Errorf("NetworkPassword: %w", err)
	}
	return cfg, nil
}

func localName(n xml.Name) string {
	if n.Local != "" {
		return n.Local
	}
	return n.Space
}

func childText(root *genericXML, local string) string {
	if root == nil {
		return ""
	}
	for i := range root.Nodes {
		if localName(root.Nodes[i].XMLName) == local {
			return strings.TrimSpace(collectText(&root.Nodes[i]))
		}
	}
	return ""
}

func collectText(n *genericXML) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(n.Text)
	for i := range n.Nodes {
		// Unexpected nested elements: still gather chardata.
		b.WriteString(collectText(&n.Nodes[i]))
	}
	return b.String()
}

func textChild(local, text string) genericXML {
	return genericXML{
		XMLName: xml.Name{Local: local},
		Text:    text,
	}
}

func setOrAppendTextChild(root *genericXML, local, text string) {
	if root == nil {
		return
	}
	for i := range root.Nodes {
		if localName(root.Nodes[i].XMLName) == local {
			root.Nodes[i].Text = text
			root.Nodes[i].Nodes = nil // drop nested junk inside known field
			return
		}
	}
	root.Nodes = append(root.Nodes, textChild(local, text))
}

func marshalXML(root *genericXML) ([]byte, error) {
	if root == nil {
		return nil, ErrEmptyConfig
	}
	// Ensure root local name.
	if root.XMLName.Local == "" && root.XMLName.Space == "" {
		root.XMLName = xml.Name{Local: "vPilotConfig"}
	}
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

func pkcs7Pad(b []byte, blockSize int) []byte {
	pad := blockSize - (len(b) % blockSize)
	out := make([]byte, len(b)+pad)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

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

func ecbEncrypt(block cipher.Block, dst, src []byte) {
	bs := block.BlockSize()
	for i := 0; i < len(src); i += bs {
		block.Encrypt(dst[i:i+bs], src[i:i+bs])
	}
}

func ecbDecrypt(block cipher.Block, dst, src []byte) {
	bs := block.BlockSize()
	for i := 0; i < len(src); i += bs {
		block.Decrypt(dst[i:i+bs], src[i:i+bs])
	}
}
