package clientinject

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml
var embeddedProfiles embed.FS

// RequiredSchemaVersion is the only schema the loader accepts.
const RequiredSchemaVersion = 2

// Profile is a loaded schema_version 2 document (fingerprint + mutations).
type Profile struct {
	SchemaVersion          int               `yaml:"schema_version"`
	ClientID               string            `yaml:"client_id"`
	DisplayName            string            `yaml:"display_name"`
	Vendor                 string            `yaml:"vendor"`
	LicenseNote            string            `yaml:"license_note"`
	ProfileVersion         string            `yaml:"profile_version"`
	SupportedClientVersion string            `yaml:"supported_client_version"`
	Released               string            `yaml:"released"`
	DownloadPage           string            `yaml:"download_page"`
	Installer              *InstallerSpec    `yaml:"installer"`
	PrimaryBinary          PrimaryBinarySpec `yaml:"primary_binary"`
	ConfigFiles            []ConfigFileSpec  `yaml:"config_files"`
	RelatedBinaries        []string          `yaml:"related_binaries"`
	// PESections documents native PE section maps (VA→file conversion for padded_string adapters).
	PESections  []PESectionSpec       `yaml:"pe_sections"`
	CLR         *CLRSpec              `yaml:"clr"`
	Strings     map[string]StringSpec `yaml:"strings"`
	USFreeSlots []USFreeSlot          `yaml:"us_free_slots"`
	Mutations   []ProfileMutationSpec `yaml:"mutations"`
	Launch      LaunchSpec            `yaml:"launch"`
	// ProfileID is the stem of the YAML file (e.g. "vpilot-3.12.1"), set by loader.
	ProfileID string `yaml:"-"`
}

// PESectionSpec is a PE section raw/virtual base for VA→file offset conversion.
type PESectionSpec struct {
	Name         string        `yaml:"name"`
	RawOffset    FlexibleInt64 `yaml:"raw_offset"`
	VirtualStart FlexibleInt64 `yaml:"virtual_start"`
}

// InstallerSpec describes the upstream installer (metadata only).
type InstallerSpec struct {
	Filename  string `yaml:"filename"`
	URL       string `yaml:"url"`
	SizeBytes int64  `yaml:"size_bytes"`
	SHA256    string `yaml:"sha256"`
	SHA1      string `yaml:"sha1"`
	Format    string `yaml:"format"`
}

// PrimaryBinarySpec fingerprints the primary PE.
type PrimaryBinarySpec struct {
	RelativePath       string              `yaml:"relative_path"`
	DefaultInstallGlob map[string][]string `yaml:"default_install_globs"`
	SHA1               string              `yaml:"sha1"`
	SHA256             string              `yaml:"sha256"`
	SizeBytes          int64               `yaml:"size_bytes"`
	PE                 *PEMeta             `yaml:"pe"`
}

// PEMeta is optional PE/CLR metadata.
type PEMeta struct {
	Framework    string `yaml:"framework"`
	CLR          bool   `yaml:"clr"`
	Architecture string `yaml:"architecture"`
}

// ConfigFileSpec lists a config file relative to install root.
type ConfigFileSpec struct {
	RelativePath string `yaml:"relative_path"`
	Format       string `yaml:"format"`
}

// CLRSpec holds CLR #US heap location.
type CLRSpec struct {
	USHeap *USHeapSpec `yaml:"us_heap"`
}

// USHeapSpec is the #US stream file range.
type USHeapSpec struct {
	FileOffset FlexibleInt64 `yaml:"file_offset"`
	Size       FlexibleInt64 `yaml:"size"`
}

// StringSpec describes one stock string and its patch sites.
type StringSpec struct {
	Stock              string          `yaml:"stock"`
	USHeapOffset       FlexibleInt64   `yaml:"us_heap_offset"`
	BodyFileOffsets    []FlexibleInt64 `yaml:"body_file_offsets"`
	LdstrFileOffsets   []FlexibleInt64 `yaml:"ldstr_file_offsets"`
	PayloadBudgetBytes int             `yaml:"payload_budget_bytes"`
	Template           string          `yaml:"template"`
}

// USFreeSlot is a free #US slot for ldstr remap (populated after research).
type USFreeSlot struct {
	ID          string        `yaml:"id"`
	HeapOffset  FlexibleInt64 `yaml:"heap_offset"`
	BodyOffset  FlexibleInt64 `yaml:"body_offset"`
	BudgetBytes int           `yaml:"budget_bytes"`
	Description string        `yaml:"description"`
}

// ProfileMutationSpec is a declarative mutation from YAML.
type ProfileMutationSpec struct {
	ID          string         `yaml:"id"`
	Kind        string         `yaml:"kind"`
	Description string         `yaml:"description"`
	StringRef   string         `yaml:"string_ref"`
	OnTooLong   string         `yaml:"on_too_long"`
	FileOffset  *FlexibleInt64 `yaml:"file_offset"`
	NewBytes    []byte         `yaml:"new_bytes"`
	OnlyIf      string         `yaml:"only_if"`
	// AvailableBytes is the padded-string slot size (padded_string kind).
	AvailableBytes *FlexibleInt64 `yaml:"available_bytes"`
	// Encoding is "utf8" / "ascii" / "utf16le" for padded_string.
	Encoding string `yaml:"encoding"`
	// EndpointKey selects a planned URL: "status_json", "fsd_jwt", "status", "afv_base", …
	EndpointKey string `yaml:"endpoint_key"`
	// LengthOf, when set on raw_overwrite, writes a single-byte length of that endpoint URL.
	LengthOf string `yaml:"length_of"`
	// Fields is free-form for config_rewrite / vpilot_config.
	Fields map[string]any `yaml:"fields"`
}

// FileOffsetOf returns the VA→file conversion for a section-relative virtual address,
// or (-1, false) if the section is unknown.
func (p *Profile) FileOffsetOf(sectionName string, virtualAddr int64) (int64, bool) {
	if p == nil {
		return -1, false
	}
	for _, sec := range p.PESections {
		if sec.Name != sectionName {
			continue
		}
		return sec.RawOffset.Int64() + (virtualAddr - sec.VirtualStart.Int64()), true
	}
	return -1, false
}

// LaunchSpec holds default launch flag templates.
type LaunchSpec struct {
	ServerAddressFlag string   `yaml:"server_address_flag"`
	NoVoiceFlags      []string `yaml:"no_voice_flags"`
}

// FlexibleInt64 accepts decimal ints, hex ints (0x…), or numeric strings in YAML.
type FlexibleInt64 int64

// Int64 returns the underlying value.
func (f FlexibleInt64) Int64() int64 { return int64(f) }

// UnmarshalYAML implements yaml.Unmarshaler.
func (f *FlexibleInt64) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		s := strings.TrimSpace(value.Value)
		if s == "" || s == "null" || s == "~" {
			*f = 0
			return nil
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			v, err := strconv.ParseInt(s[2:], 16, 64)
			if err != nil {
				return fmt.Errorf("clientinject: parse hex %q: %w", s, err)
			}
			*f = FlexibleInt64(v)
			return nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("clientinject: parse int %q: %w", s, err)
		}
		*f = FlexibleInt64(v)
		return nil
	default:
		return fmt.Errorf("clientinject: FlexibleInt64: unexpected yaml kind %v", value.Kind)
	}
}

// ProfileStore holds loaded profiles indexed by profile id and by client+sha1.
type ProfileStore struct {
	byID        map[string]*Profile
	byClientSHA map[string]*Profile // key: clientID + "\x00" + sha1(lower)
}

// NewProfileStore returns an empty store.
func NewProfileStore() *ProfileStore {
	return &ProfileStore{
		byID:        make(map[string]*Profile),
		byClientSHA: make(map[string]*Profile),
	}
}

// LoadEmbedded loads all schema v2 profiles from the go:embed tree.
func LoadEmbedded() (*ProfileStore, error) {
	return loadFromFS(embeddedProfiles, "profiles")
}

// LoadFromDir loads all *.yaml profiles from dir (for tests / --profiles-dir).
func LoadFromDir(dir string) (*ProfileStore, error) {
	return loadFromFS(os.DirFS(dir), ".")
}

func loadFromFS(fsys fs.FS, root string) (*ProfileStore, error) {
	store := NewProfileStore()
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("clientinject: read profiles dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") && !strings.HasSuffix(strings.ToLower(name), ".yml") {
			continue
		}
		path := name
		if root != "." {
			path = root + "/" + name
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("clientinject: read %s: %w", path, err)
		}
		p, err := ParseProfile(data, strings.TrimSuffix(name, filepath.Ext(name)))
		if err != nil {
			return nil, fmt.Errorf("clientinject: profile %s: %w", name, err)
		}
		if err := store.Add(p); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// ParseProfile unmarshals YAML and validates schema_version == 2.
func ParseProfile(data []byte, profileID string) (*Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if p.SchemaVersion != RequiredSchemaVersion {
		return nil, fmt.Errorf("schema_version %d not supported (require %d)", p.SchemaVersion, RequiredSchemaVersion)
	}
	if p.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if p.PrimaryBinary.RelativePath == "" {
		return nil, fmt.Errorf("primary_binary.relative_path is required")
	}
	p.ProfileID = profileID
	if p.Strings == nil {
		p.Strings = make(map[string]StringSpec)
	}
	return &p, nil
}

// Add indexes a profile. Duplicate ProfileID is an error.
func (s *ProfileStore) Add(p *Profile) error {
	if p == nil {
		return fmt.Errorf("clientinject: nil profile")
	}
	if p.ProfileID == "" {
		return fmt.Errorf("clientinject: empty profile id")
	}
	if _, ok := s.byID[p.ProfileID]; ok {
		return fmt.Errorf("clientinject: duplicate profile id %q", p.ProfileID)
	}
	s.byID[p.ProfileID] = p
	if sha := strings.ToLower(strings.TrimSpace(p.PrimaryBinary.SHA1)); sha != "" {
		s.byClientSHA[p.ClientID+"\x00"+sha] = p
	}
	return nil
}

// Get returns a profile by id.
func (s *ProfileStore) Get(id string) (*Profile, bool) {
	if s == nil {
		return nil, false
	}
	p, ok := s.byID[id]
	return p, ok
}

// LookupByHash finds a profile for clientID + primary PE SHA-1 (hex, case-insensitive).
func (s *ProfileStore) LookupByHash(clientID, sha1hex string) (*Profile, bool) {
	if s == nil {
		return nil, false
	}
	p, ok := s.byClientSHA[clientID+"\x00"+strings.ToLower(strings.TrimSpace(sha1hex))]
	return p, ok
}

// List returns all profiles in unspecified order.
func (s *ProfileStore) List() []*Profile {
	if s == nil {
		return nil
	}
	out := make([]*Profile, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, p)
	}
	return out
}

// ForClient returns profiles for a client_id.
func (s *ProfileStore) ForClient(clientID string) []*Profile {
	var out []*Profile
	for _, p := range s.List() {
		if p.ClientID == clientID {
			out = append(out, p)
		}
	}
	return out
}
