package clientinject

import "context"

// InstallCandidate is a discovered client install location.
type InstallCandidate struct {
	ClientID    string
	RootDir     string
	PrimaryPE   string
	ConfigPaths []string
	DisplayHint string
}

// Install is a resolved install + fingerprint binding.
type Install struct {
	ClientID    string
	RootDir     string
	PrimaryPE   string
	ConfigPaths []string
	HashSHA1    string
	ProfileID   string
}

// Constraint is a client-agnostic UI hint (GUI must not special-case clients).
type Constraint struct {
	Field       string // "JWTURL", "AFVBaseURL", "WebBaseURL", ...
	MaxRunes    int    // 0 = unknown / N/A
	Strategy    string // "in_place", "remap_required", "server_alias", "n/a"
	Description string
}

// Mutation is one planned change to disk or launch.
type Mutation struct {
	ID          string
	Kind        MutationKind
	Description string
	TargetRel   string // relative to install root (optional for launch_flag)
	Detail      any    // typed payload; see mutation detail structs below
}

// Plan is a dry-run result; non-empty Blockers means Apply must refuse.
type Plan struct {
	Install     Install
	Endpoints   Endpoints
	Mutations   []Mutation
	Constraints []Constraint
	Warnings    []string
	Blockers    []string
}

// ApplyResult summarizes a successful transactional Apply.
type ApplyResult struct {
	ManifestPath string
	BackupRoot   string // install root where .openfsd-bak siblings live
	Applied      []string
}

// USStringDetail is Detail for MutUSHeapString.
type USStringDetail struct {
	StringRef string
	NewString string
	HeapOff   int64
	BodyOffs  []int64
}

// LdstrRemapDetail is Detail for MutLdstrRemap.
type LdstrRemapDetail struct {
	LdstrFileOff int64
	NewToken     []byte
	SlotHeapOff  int64
	SlotBodyOff  int64
	SlotBudget   int
}

// RawOverwriteDetail is Detail for MutRawOverwrite / MutAFVDisablePE.
type RawOverwriteDetail struct {
	FileOffset int64
	NewBytes   []byte
}

// PaddedStringDetail is Detail for MutPaddedString.
type PaddedStringDetail struct {
	FileOffset int64
	NewString  string
	SlotLen    int
	Encoding   string // "utf16le" or "ascii"
}

// ConfigRewriteDetail is Detail for MutConfigRewrite.
type ConfigRewriteDetail struct {
	Paths            []string
	NetworkStatusURL string
	CachedServers    []string
	ClearCredentials bool
}

// LaunchFlagDetail is Detail for MutLaunchFlag.
type LaunchFlagDetail struct {
	Args []string
}

// WriteFileDetail is a test/simple Detail: replace entire file contents.
// Not produced by production profiles; used by FakeAdapter and unit tests.
type WriteFileDetail struct {
	Contents []byte
}

// Adapter is the per-client strategy; GUI/CLI stay generic.
type Adapter interface {
	ClientID() string
	DisplayName() string
	SupportedProfiles() []string

	Discover(ctx context.Context) ([]InstallCandidate, error)
	Verify(install Install, profile *Profile) error

	// EndpointConstraints returns static/profile hints before full plan
	// (optional; may return nil and rely solely on Plan.Constraints).
	EndpointConstraints(profile *Profile) []Constraint

	// Plan must not write disk. Must populate Constraints + Blockers.
	Plan(install Install, profile *Profile, ep Endpoints) (*Plan, error)

	Apply(ctx context.Context, plan *Plan, w FileWriter) error
	HealthCheck(install Install, ep Endpoints) error
	LaunchArgs(install Install, ep Endpoints) []string
}
