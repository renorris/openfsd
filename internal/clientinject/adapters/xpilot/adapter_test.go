package xpilot

import (
	"bytes"
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
	"github.com/renorris/openfsd/internal/clientinject/pepatch"
)

func sha1hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

// Compact synthetic layout — not production offsets (those need ~30MB PE).
const (
	testStatusOff  = 0x100
	testJWTOff     = 0x200
	testStatusLEA  = 0x50
	testStatusLen  = 0x54
	testJWTLEA     = 0x58
	testJWTLen     = 0x5C
	testBreakOff   = 0x60
	testStatusSlot = 64
	testJWTSlot    = 80
)

func fo(v int64) *clientinject.FlexibleInt64 {
	f := clientinject.FlexibleInt64(v)
	return &f
}

func testProfile(pe []byte) *clientinject.Profile {
	return &clientinject.Profile{
		SchemaVersion:          2,
		ProfileID:              "xpilot-test-synth",
		ClientID:               clientID,
		DisplayName:            "xPilot",
		SupportedClientVersion: "3.0.1-test",
		PrimaryBinary: clientinject.PrimaryBinarySpec{
			RelativePath: primaryName,
			SHA1:         sha1hex(pe),
			SizeBytes:    int64(len(pe)),
		},
		Strings: map[string]clientinject.StringSpec{
			"status_json": {PayloadBudgetBytes: testStatusSlot, Template: "{{.StatusJSONURL}}"},
			"fsd_jwt":     {PayloadBudgetBytes: testJWTSlot, Template: "{{.JWTURL}}"},
		},
		Mutations: []clientinject.ProfileMutationSpec{
			{
				ID: "write_status_json", Kind: "padded_string", Description: "status",
				FileOffset: fo(testStatusOff), AvailableBytes: fo(testStatusSlot),
				Encoding: "utf8", EndpointKey: "status_json",
			},
			{
				ID: "write_fsd_jwt", Kind: "padded_string", Description: "jwt",
				FileOffset: fo(testJWTOff), AvailableBytes: fo(testJWTSlot),
				Encoding: "utf16le", EndpointKey: "fsd_jwt",
			},
			{
				ID: "patch_status_lea", Kind: "raw_overwrite", Description: "status lea",
				FileOffset: fo(testStatusLEA), NewBytes: []byte{0x9C, 0x68, 0xA8, 0x01},
			},
			{
				ID: "patch_status_len", Kind: "raw_overwrite", Description: "status len",
				FileOffset: fo(testStatusLen), LengthOf: "status_json",
			},
			{
				ID: "patch_jwt_lea", Kind: "raw_overwrite", Description: "jwt lea",
				FileOffset: fo(testJWTLEA), NewBytes: []byte{0x2A, 0x0A, 0xA6, 0x01},
			},
			{
				ID: "patch_jwt_len", Kind: "raw_overwrite", Description: "jwt len",
				FileOffset: fo(testJWTLen), LengthOf: "fsd_jwt",
			},
			{
				ID: "break_fsd", Kind: "raw_overwrite", Description: "break fsd",
				FileOffset: fo(testBreakOff), NewBytes: []byte{0x66, 0x6F, 0x6F},
			},
		},
	}
}

func buildSynthPE(t *testing.T) []byte {
	t.Helper()
	// Large enough for JWT slot end.
	size := testJWTOff + testJWTSlot + 16
	data := make([]byte, size)
	// Stock-like placeholders.
	copy(data[testStatusOff:], []byte("https://status.vatsim.net/old.json"))
	u := utf16.Encode([]rune("https://auth.vatsim.net/api/fsd-jwt"))
	for i, c := range u {
		binary.LittleEndian.PutUint16(data[testJWTOff+i*2:], c)
	}
	// break site stock-ish
	copy(data[testBreakOff:], []byte("fsd.vatsim.net"))
	return data
}

func setupInstall(t *testing.T) (root string, pe []byte, install clientinject.Install, profile *clientinject.Profile, a *Adapter) {
	t.Helper()
	root = t.TempDir()
	pe = buildSynthPE(t)
	pePath := filepath.Join(root, primaryName)
	if err := os.WriteFile(pePath, pe, 0o644); err != nil {
		t.Fatal(err)
	}
	profile = testProfile(pe)
	store := clientinject.NewProfileStore()
	if err := store.Add(profile); err != nil {
		t.Fatal(err)
	}
	a = NewWithProfiles(store)
	install = clientinject.Install{
		ClientID:  clientID,
		RootDir:   root,
		PrimaryPE: pePath,
		HashSHA1:  profile.PrimaryBinary.SHA1,
		ProfileID: profile.ProfileID,
	}
	return root, pe, install, profile, a
}

func TestClientIDDisplay(t *testing.T) {
	a := New()
	if a.ClientID() != "xpilot" || a.DisplayName() != "xPilot" {
		t.Fatal(a.ClientID(), a.DisplayName())
	}
	if len(a.SupportedProfiles()) != 1 || a.SupportedProfiles()[0] != profileID {
		t.Fatalf("profiles=%v", a.SupportedProfiles())
	}
}

func TestPlan_OK(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL: "https://fsd.ex.co",
		FSDHost:    "fsd.ex.co",
	}
	plan, err := a.Plan(install, profile, ep)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers: %v", plan.Blockers)
	}
	var padded, raw int
	for _, m := range plan.Mutations {
		switch m.Kind {
		case clientinject.MutPaddedString:
			padded++
		case clientinject.MutRawOverwrite:
			raw++
		}
	}
	if padded != 2 {
		t.Fatalf("padded=%d", padded)
	}
	if raw < 4 {
		t.Fatalf("raw=%d", raw)
	}
	// Dynamic length for status URL.
	statusURL := ep.StatusJSONURL()
	var sawLen bool
	for _, m := range plan.Mutations {
		if m.ID != "patch_status_len" {
			continue
		}
		d, ok := rawDetail(m.Detail)
		if !ok || len(d.NewBytes) != 1 {
			t.Fatalf("detail=%+v", m.Detail)
		}
		if d.NewBytes[0] != byte(len([]rune(statusURL))) {
			t.Fatalf("len byte=%d want %d", d.NewBytes[0], len([]rune(statusURL)))
		}
		sawLen = true
	}
	if !sawLen {
		t.Fatal("missing status len mutation")
	}
}

func TestPlan_MissingWebBaseBlocker(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	plan, err := a.Plan(install, profile, clientinject.Endpoints{FSDHost: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) == 0 {
		t.Fatal("expected blockers")
	}
}

func TestPlan_RefuseUnknownMutationKind(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	profile.Mutations = append(profile.Mutations, clientinject.ProfileMutationSpec{
		ID: "bad", Kind: "cil_us_string", FileOffset: fo(1),
	})
	plan, err := a.Plan(install, profile, clientinject.Endpoints{
		WebBaseURL: "https://fsd.ex.co",
		FSDHost:    "fsd.ex.co",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range plan.Blockers {
		if strings.Contains(b, "unsupported kind") || strings.Contains(b, "cil_us_string") {
			found = true
		}
	}
	if !found {
		t.Fatalf("blockers=%v", plan.Blockers)
	}
}

func TestVerify_RefuseUnknownHash(t *testing.T) {
	_, _, install, profile, a := setupInstall(t)
	// Corrupt PE.
	if err := os.WriteFile(install.PrimaryPE, []byte("not-the-pe"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := a.Verify(install, profile)
	if err == nil || !strings.Contains(err.Error(), "refuse unknown hash") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyHealth_RoundTrip(t *testing.T) {
	_, stock, install, profile, a := setupInstall(t)
	ep := clientinject.Endpoints{
		WebBaseURL: "https://fsd.ex.co",
		FSDHost:    "fsd.ex.co",
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
	if err := a.HealthCheck(install, ep); err != nil {
		t.Fatal(err)
	}
	// Ensure stock was actually changed.
	got, err := os.ReadFile(install.PrimaryPE)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, stock) {
		t.Fatal("PE unchanged after apply")
	}
	// Status URL present as UTF-8.
	status := ep.StatusJSONURL()
	if !bytes.Contains(got[testStatusOff:testStatusOff+testStatusSlot], []byte(status)) {
		t.Fatalf("status URL missing in slot")
	}
	// JWT as UTF-16LE.
	u := utf16.Encode([]rune(ep.JWTURL()))
	pat := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(pat[i*2:], c)
	}
	if !bytes.Contains(got[testJWTOff:testJWTOff+testJWTSlot], pat) {
		t.Fatal("jwt utf16 missing")
	}
	// Break site.
	if !bytes.Equal(got[testBreakOff:testBreakOff+3], []byte("foo")) {
		t.Fatalf("break=%q", got[testBreakOff:testBreakOff+3])
	}
}

func TestEngine_ApplyRevert_WithXPilot(t *testing.T) {
	root, stock, install, profile, a := setupInstall(t)
	store := clientinject.NewProfileStore()
	if err := store.Add(profile); err != nil {
		t.Fatal(err)
	}
	eng := clientinject.NewEngine(store, a)
	ep := clientinject.Endpoints{
		WebBaseURL: "https://fsd.ex.co",
		FSDHost:    "fsd.ex.co",
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
	// Health via engine already ran inside Apply.
	got, _ := os.ReadFile(install.PrimaryPE)
	if bytes.Equal(got, stock) {
		t.Fatal("not patched")
	}
	if err := eng.Revert(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(install.PrimaryPE)
	if !bytes.Equal(restored, stock) {
		t.Fatal("revert did not restore stock PE")
	}
}

func TestInstallFromDir(t *testing.T) {
	inst := InstallFromDir("/tmp/xpilot-install")
	if inst.ClientID != clientID {
		t.Fatal(inst.ClientID)
	}
	if !strings.HasSuffix(inst.PrimaryPE, primaryName) {
		t.Fatal(inst.PrimaryPE)
	}
}

func TestDiscover_EmptyOnUnixWithoutWine(t *testing.T) {
	// Should not error; may return empty.
	cands, err := New().Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = cands
}

func TestPaddedString_pepatchEncodingParity(t *testing.T) {
	// Sanity: pepatch accepts encodings used by profile.
	var buf struct {
		data []byte
		pos  int64
	}
	buf.data = make([]byte, 32)
	// Use a small WriteSeeker via temp file.
	dir := t.TempDir()
	path := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(path, make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pepatch.PaddedStringOverwrite(f, 0, "https://x.test/j", 32, "utf8"); err != nil {
		t.Fatal(err)
	}
	// UTF-16LE needs (runes+1)*2 bytes; use a short string that fits remaining 32.
	if err := pepatch.PaddedStringOverwrite(f, 32, "https://x.t/j", 32, "utf16le"); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointConstraints(t *testing.T) {
	_, _, _, profile, a := setupInstall(t)
	cs := a.EndpointConstraints(profile)
	if len(cs) < 2 {
		t.Fatalf("constraints=%v", cs)
	}
}

func TestLaunchArgsEmpty(t *testing.T) {
	a := New()
	if args := a.LaunchArgs(clientinject.Install{}, clientinject.Endpoints{}); len(args) != 0 {
		t.Fatalf("args=%v", args)
	}
}
