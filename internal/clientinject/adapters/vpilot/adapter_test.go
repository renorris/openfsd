package vpilot

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/cilus"
	"github.com/renorris/openfsd/internal/clientinject/vpilotconfig"
)

const (
	testJWTBodyOff = 0x100
	testAFVBodyOff = 0x200
	testJWTBudget  = 71
	testAFVBudget  = 51
)

func sha1hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

// buildSyntheticPE places stock JWT and AFV #US entries at known body offsets.
func buildSyntheticPE(t *testing.T) []byte {
	t.Helper()
	// File large enough for both slots + residual padding.
	size := testAFVBodyOff + int64(testAFVBudget) + 64
	data := make([]byte, size)
	writeStockUS(t, data, testJWTBodyOff, stockJWT, testJWTBudget)
	writeStockUS(t, data, testAFVBodyOff, stockAFV, testAFVBudget)
	// Pre-R3 residual extra stock JWT (resource-like) — should warn, not fail.
	extra := testAFVBodyOff + int64(testAFVBudget) + 8
	// Place raw UTF-16 only (no #US header) for residual scan.
	raw := encodeUTF16(stockJWT)
	if int(extra)+len(raw) <= len(data) {
		copy(data[extra:], raw)
	}
	return data
}

func writeStockUS(t *testing.T, data []byte, bodyOff int64, s string, budget int) {
	t.Helper()
	body := cilus.EncodeBody(s)
	if len(body) > budget {
		t.Fatalf("stock body %d > budget %d", len(body), budget)
	}
	prefix, err := cilus.LengthPrefixBytes(len(body))
	if err != nil {
		t.Fatal(err)
	}
	padded, err := cilus.EncodeBodyPadded(s, budget)
	if err != nil {
		t.Fatal(err)
	}
	prefixOff := bodyOff - int64(len(prefix))
	if prefixOff < 0 {
		t.Fatal("prefix off")
	}
	copy(data[prefixOff:], prefix)
	copy(data[bodyOff:], padded)
}

func encodeUTF16(s string) []byte {
	// UTF-16LE without null (for residual ScanUTF16String).
	u16 := utf16.Encode([]rune(s))
	out := make([]byte, len(u16)*2)
	for i, c := range u16 {
		binary.LittleEndian.PutUint16(out[i*2:], c)
	}
	return out
}

func testProfile(pe []byte) *clientinject.Profile {
	return &clientinject.Profile{
		SchemaVersion:          2,
		ProfileID:              "vpilot-test-synth",
		ClientID:               clientID,
		DisplayName:            "vPilot",
		SupportedClientVersion: "3.12.1-test",
		PrimaryBinary: clientinject.PrimaryBinarySpec{
			RelativePath: primaryName,
			SHA1:         sha1hex(pe),
			SizeBytes:    int64(len(pe)),
		},
		Strings: map[string]clientinject.StringSpec{
			"fsd_jwt": {
				Stock:              stockJWT,
				USHeapOffset:       clientinject.FlexibleInt64(testJWTBodyOff - 1),
				BodyFileOffsets:    []clientinject.FlexibleInt64{clientinject.FlexibleInt64(testJWTBodyOff)},
				PayloadBudgetBytes: testJWTBudget,
			},
			"afv_base": {
				Stock:              stockAFV,
				USHeapOffset:       clientinject.FlexibleInt64(testAFVBodyOff - 1),
				BodyFileOffsets:    []clientinject.FlexibleInt64{clientinject.FlexibleInt64(testAFVBodyOff)},
				PayloadBudgetBytes: testAFVBudget,
			},
		},
		Launch: clientinject.LaunchSpec{
			ServerAddressFlag: "-serveraddressoverride",
			NoVoiceFlags:      []string{"-novoice"},
		},
	}
}

func setupInstall(t *testing.T) (root string, pe []byte, install clientinject.Install, profile *clientinject.Profile, a *Adapter) {
	t.Helper()
	root = t.TempDir()
	pe = buildSyntheticPE(t)
	pePath := filepath.Join(root, primaryName)
	if err := os.WriteFile(pePath, pe, 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal stock-like config.
	cfg := &vpilotconfig.Config{
		NetworkStatusURL: "http://status.vatsim.net/",
		CachedServers:    []string{"AUTOMATIC|fsd.connect.vatsim.net"},
	}
	raw, err := vpilotconfig.Format(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, configName)
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	profile = testProfile(pe)
	store := clientinject.NewProfileStore()
	if err := store.Add(profile); err != nil {
		t.Fatal(err)
	}
	a = NewWithProfiles(store)
	install = clientinject.Install{
		ClientID:    clientID,
		RootDir:     root,
		PrimaryPE:   pePath,
		ConfigPaths: []string{cfgPath},
		HashSHA1:    profile.PrimaryBinary.SHA1,
		ProfileID:   profile.ProfileID,
	}
	return root, pe, install, profile, a
}

func TestPlan_ShortHostInPlace(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL:    "https://fsd.ex.co",
		FSDHost:       "fsd.ex.co",
		AFVBaseURL:    "https://v.ex.co",
		FSDServerName: "OPENFSD",
	}
	plan, err := a.Plan(install, profile, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers: %v", plan.Blockers)
	}
	var jwtMut, afvMut bool
	for _, m := range plan.Mutations {
		if m.Kind == clientinject.MutUSHeapString {
			d, _ := usDetail(m.Detail)
			if d.StringRef == "fsd_jwt" {
				jwtMut = true
				if d.NewString != "https://fsd.ex.co/api/v1/fsd-jwt" {
					t.Fatalf("jwt url=%q", d.NewString)
				}
			}
			if d.StringRef == "afv_base" {
				afvMut = true
			}
		}
	}
	if !jwtMut || !afvMut {
		t.Fatalf("expected in-place JWT and AFV mutations, jwt=%v afv=%v", jwtMut, afvMut)
	}
}

func TestPlan_LongHostBlocker(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL: "https://fsd.example.com",
		FSDHost:    "fsd.example.com",
	}
	plan, err := a.Plan(install, profile, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) == 0 {
		t.Fatal("expected JWT budget blocker for long host without --prefer-short-jwt")
	}
	found := false
	for _, b := range plan.Blockers {
		if strings.Contains(b, "exceeds #US budget") || strings.Contains(b, "free-slot") {
			found = true
		}
	}
	if !found {
		t.Fatalf("blockers=%v", plan.Blockers)
	}
}

func TestPlan_PreferShortJWTPath(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL:         "https://fsd.example.com",
		FSDHost:            "fsd.example.com",
		PreferShortJWTPath: true,
		AFVBaseURL:         "https://voice.example.com", // exactly 25
	}
	plan, err := a.Plan(install, profile, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers: %v", plan.Blockers)
	}
	var jwtURL string
	for _, m := range plan.Mutations {
		if m.Kind == clientinject.MutUSHeapString {
			d, _ := usDetail(m.Detail)
			if d.StringRef == "fsd_jwt" {
				jwtURL = d.NewString
			}
		}
	}
	if jwtURL != "https://fsd.example.com/j" {
		t.Fatalf("expected short JWT /j, got %q", jwtURL)
	}
	// PreferShortJWTPath honesty warning present.
	warnOK := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "PreferShortJWTPath") || strings.Contains(w, "without connectivity") {
			warnOK = true
		}
	}
	if !warnOK {
		t.Fatalf("expected PreferShortJWTPath warning, got %v", plan.Warnings)
	}
}

func TestPlan_AFVOverBudgetNoVoice(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL: "https://fsd.ex.co",
		FSDHost:    "fsd.ex.co",
		AFVBaseURL: "https://voice1.example.com", // 26 > 25
	}
	plan, err := a.Plan(install, profile, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("JWT should fit; blockers=%v", plan.Blockers)
	}
	hasNoVoice := false
	hasAFVPatch := false
	for _, m := range plan.Mutations {
		if m.Kind == clientinject.MutLaunchFlag {
			d := m.Detail.(clientinject.LaunchFlagDetail)
			for _, arg := range d.Args {
				if arg == "-novoice" {
					hasNoVoice = true
				}
			}
		}
		if m.Kind == clientinject.MutUSHeapString {
			d, _ := usDetail(m.Detail)
			if d.StringRef == "afv_base" {
				hasAFVPatch = true
			}
		}
	}
	if !hasNoVoice {
		t.Fatal("expected -novoice launch flag for over-budget AFV")
	}
	if hasAFVPatch {
		t.Fatal("should not in-place patch over-budget AFV")
	}
}

func TestApply_ConfigAndPE(t *testing.T) {
	root, _, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL:    "https://fsd.ex.co",
		FSDHost:       "fsd.ex.co",
		AFVBaseURL:    "https://v.ex.co",
		FSDServerName: "OPENFSD",
	}
	plan, err := a.Plan(install, profile, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers: %v", plan.Blockers)
	}
	w := clientinject.OSFileWriter{}
	if err := a.Apply(context.Background(), plan, w); err != nil {
		t.Fatal(err)
	}

	// Config round-trip.
	cfgPath := filepath.Join(root, configName)
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := vpilotconfig.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkStatusURL != "https://fsd.ex.co/api/v1/data/status.txt" {
		t.Fatalf("status=%q", cfg.NetworkStatusURL)
	}
	if len(cfg.CachedServers) != 1 || cfg.CachedServers[0] != "OPENFSD|fsd.ex.co" {
		t.Fatalf("servers=%v", cfg.CachedServers)
	}
	if cfg.NetworkLogin != "" || cfg.NetworkPassword != "" {
		t.Fatal("credentials not cleared")
	}

	// PE #US
	peData, err := os.ReadFile(install.PrimaryPE)
	if err != nil {
		t.Fatal(err)
	}
	gotJWT, err := decodeUSAtBody(peData, testJWTBodyOff, testJWTBudget)
	if err != nil {
		t.Fatal(err)
	}
	if gotJWT != "https://fsd.ex.co/api/v1/fsd-jwt" {
		t.Fatalf("jwt pe=%q", gotJWT)
	}
	gotAFV, err := decodeUSAtBody(peData, testAFVBodyOff, testAFVBudget)
	if err != nil {
		t.Fatal(err)
	}
	if gotAFV != "https://v.ex.co" {
		t.Fatalf("afv pe=%q", gotAFV)
	}

	if err := a.HealthCheck(install, ep); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestApply_ViaEngine(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	store := clientinject.NewProfileStore()
	if err := store.Add(profile); err != nil {
		t.Fatal(err)
	}
	a.Profiles = store
	eng := clientinject.NewEngine(store, a)
	ep := clientinject.Endpoints{
		WebBaseURL: "https://fsd.ex.co",
		FSDHost:    "fsd.ex.co",
		AFVBaseURL: "https://v.ex.co",
	}
	plan, err := eng.Plan(context.Background(), install, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers: %v", plan.Blockers)
	}
	res, err := eng.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if res.ManifestPath == "" {
		t.Fatal("empty manifest")
	}
	// Revert restores stock.
	if err := eng.Revert(context.Background(), install.RootDir); err != nil {
		t.Fatal(err)
	}
	peData, err := os.ReadFile(install.PrimaryPE)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeUSAtBody(peData, testJWTBodyOff, testJWTBudget)
	if err != nil {
		t.Fatal(err)
	}
	if got != stockJWT {
		t.Fatalf("after revert jwt=%q", got)
	}
}

func TestVerify_UnknownHash(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	profile.PrimaryBinary.SHA1 = "0000000000000000000000000000000000000000"
	// Size still matches; hash does not.
	err := a.Verify(install, profile)
	if err == nil {
		t.Fatal("expected hash mismatch")
	}
	if !strings.Contains(err.Error(), "sha1") {
		t.Fatalf("err=%v", err)
	}
}

func TestLaunchArgs(t *testing.T) {
	a := New()
	args := a.LaunchArgs(clientinject.Install{}, clientinject.Endpoints{
		FSDHost:         "fsd.ex.co",
		ForceDisableAFV: true,
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-serveraddressoverride") || !strings.Contains(joined, "fsd.ex.co") {
		t.Fatalf("args=%v", args)
	}
	if !strings.Contains(joined, "-novoice") {
		t.Fatalf("expected -novoice: %v", args)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	cfg := &vpilotconfig.Config{}
	vpilotconfig.ApplyEndpoints(cfg, "https://x.test/api/v1/data/status.txt", []string{"OPENFSD|h"}, true)
	raw, err := vpilotconfig.Format(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vpilotconfig.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.NetworkStatusURL != cfg.NetworkStatusURL {
		t.Fatalf("%q vs %q", got.NetworkStatusURL, cfg.NetworkStatusURL)
	}
	if len(got.CachedServers) != 1 || got.CachedServers[0] != "OPENFSD|h" {
		t.Fatalf("%v", got.CachedServers)
	}
}

func TestJWTURLCandidates_Order(t *testing.T) {
	ep := clientinject.Endpoints{
		WebBaseURL:         "https://fsd.example.com",
		PreferShortJWTPath: true,
	}
	c := jwtURLCandidates(ep)
	if len(c) < 2 || c[0] != "https://fsd.example.com/j" {
		t.Fatalf("%v", c)
	}
	// Last should include default.
	if c[len(c)-1] != "https://fsd.example.com/api/v1/fsd-jwt" {
		t.Fatalf("last=%q", c[len(c)-1])
	}
}

func TestEndpointConstraints(t *testing.T) {
	pe := buildSyntheticPE(t)
	p := testProfile(pe)
	a := New()
	cs := a.EndpointConstraints(p)
	if len(cs) < 2 {
		t.Fatalf("%v", cs)
	}
}

func TestDiscover_NoPanic(t *testing.T) {
	a := New()
	_, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallFromDir(t *testing.T) {
	root := t.TempDir()
	// Without PE, ConfigPaths still has install config path.
	inst := InstallFromDir(root)
	if inst.ClientID != clientID {
		t.Fatal(inst.ClientID)
	}
	if !strings.HasSuffix(inst.PrimaryPE, primaryName) {
		t.Fatal(inst.PrimaryPE)
	}
}

func TestClientIDDisplay(t *testing.T) {
	a := New()
	if a.ClientID() != "vpilot" || a.DisplayName() != "vPilot" {
		t.Fatal(a.ClientID(), a.DisplayName())
	}
	if len(a.SupportedProfiles()) == 0 {
		t.Fatal("profiles")
	}
}
