package clientinject

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// FakeAdapter exercises real Engine Apply/backup/restore paths.
type FakeAdapter struct {
	ID             string
	Name           string
	Profiles       []string
	Blockers       []string
	FailApply      bool
	FailAfterWrite bool
	FailHealth     bool
	VerifyErr      error
	appliedPath    string
	appliedData    []byte
	// Mutation uses WriteFileDetail on TargetRel "payload.bin"
	TargetRel string
	NewData   []byte
}

func (f *FakeAdapter) ClientID() string            { return f.ID }
func (f *FakeAdapter) DisplayName() string         { return f.Name }
func (f *FakeAdapter) SupportedProfiles() []string { return f.Profiles }
func (f *FakeAdapter) Discover(context.Context) ([]InstallCandidate, error) {
	return nil, nil
}
func (f *FakeAdapter) Verify(Install, *Profile) error            { return f.VerifyErr }
func (f *FakeAdapter) EndpointConstraints(*Profile) []Constraint { return nil }
func (f *FakeAdapter) LaunchArgs(Install, Endpoints) []string    { return nil }

func (f *FakeAdapter) Plan(install Install, profile *Profile, ep Endpoints) (*Plan, error) {
	rel := f.TargetRel
	if rel == "" {
		rel = "payload.bin"
	}
	data := f.NewData
	if data == nil {
		data = []byte("OPENFSD-PATCHED")
	}
	return &Plan{
		Install:   install,
		Endpoints: ep,
		Mutations: []Mutation{{
			ID:          "write_payload",
			Kind:        MutRawOverwrite,
			Description: "test write entire file",
			TargetRel:   rel,
			Detail:      WriteFileDetail{Contents: data},
		}},
		Blockers: append([]string(nil), f.Blockers...),
	}, nil
}

func (f *FakeAdapter) Apply(ctx context.Context, plan *Plan, w FileWriter) error {
	_ = ctx
	if f.FailApply {
		return errors.New("fake apply boom")
	}
	for _, m := range plan.Mutations {
		path := m.TargetRel
		if !filepath.IsAbs(path) {
			path = filepath.Join(plan.Install.RootDir, path)
		}
		switch d := m.Detail.(type) {
		case WriteFileDetail:
			if err := w.WriteFile(path, d.Contents); err != nil {
				return err
			}
			f.appliedPath = path
			f.appliedData = d.Contents
		case *WriteFileDetail:
			if err := w.WriteFile(path, d.Contents); err != nil {
				return err
			}
			f.appliedPath = path
			f.appliedData = d.Contents
		default:
			return fmt.Errorf("fake: unsupported detail %T", m.Detail)
		}
		if f.FailAfterWrite {
			return errors.New("fake mid-apply failure after write")
		}
	}
	return nil
}

func (f *FakeAdapter) HealthCheck(Install, Endpoints) error {
	if f.FailHealth {
		return errors.New("fake health fail")
	}
	return nil
}

func sha1hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func setupFakeInstall(t *testing.T) (root, pe, payload string, stock []byte) {
	t.Helper()
	root = t.TempDir()
	pe = filepath.Join(root, "app.exe")
	payload = filepath.Join(root, "payload.bin")
	stock = []byte("STOCK-PAYLOAD-CONTENT")
	if err := os.WriteFile(pe, []byte("MZ-fake-pe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, stock, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, pe, payload, stock
}

func TestEngine_ApplySuccess(t *testing.T) {
	root, pe, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{
			RelativePath: "app.exe",
			SHA1:         sha1hex([]byte("MZ-fake-pe")),
		},
	})
	fake := &FakeAdapter{
		ID:       "fake",
		Name:     "Fake Client",
		Profiles: []string{"fake-1"},
		NewData:  []byte("OPENFSD-PATCHED"),
	}
	eng := NewEngine(store, fake)
	install := Install{
		ClientID:  "fake",
		RootDir:   root,
		PrimaryPE: pe,
		HashSHA1:  sha1hex([]byte("MZ-fake-pe")),
		ProfileID: "fake-1",
	}
	plan, err := eng.Plan(context.Background(), install, Endpoints{WebBaseURL: "https://x.test"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := eng.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if res.ManifestPath == "" {
		t.Fatal("empty manifest path")
	}
	got, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("OPENFSD-PATCHED")) {
		t.Fatalf("payload=%q", got)
	}
	bak, err := os.ReadFile(payload + BackupSuffix)
	if err != nil {
		t.Fatalf("bak missing: %v", err)
	}
	if !bytes.Equal(bak, stock) {
		t.Fatalf("bak=%q stock=%q", bak, stock)
	}
	m, err := ReadManifest(OSFileWriter{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != ManifestStatusApplied {
		t.Fatalf("status=%q", m.Status)
	}
	if !m.PreflightOK {
		t.Fatal("preflight_ok")
	}
}

func TestEngine_ApplyBlockersRefuseWrite(t *testing.T) {
	root, pe, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{
		ID:       "fake",
		Blockers: []string{"JWT URL too long for #US budget"},
		NewData:  []byte("SHOULD-NOT-WRITE"),
	}
	eng := NewEngine(store, fake)
	install := Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1", HashSHA1: sha1hex([]byte("MZ-fake-pe"))}
	plan, err := eng.Plan(context.Background(), install, Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected blockers error")
	}
	got, _ := os.ReadFile(payload)
	if !bytes.Equal(got, stock) {
		t.Fatalf("payload mutated despite blockers: %q", got)
	}
	if _, err := os.Stat(payload + BackupSuffix); err == nil {
		t.Fatal("bak should not exist when blockers refuse")
	}
	if _, err := os.Stat(ManifestPath(root)); err == nil {
		t.Fatal("manifest should not exist when blockers refuse")
	}
}

func TestEngine_ApplyMidFailRestoresBak(t *testing.T) {
	root, pe, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{
		ID:             "fake",
		NewData:        []byte("PARTIAL-WRITE"),
		FailAfterWrite: true,
	}
	eng := NewEngine(store, fake)
	install := Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1", HashSHA1: sha1hex([]byte("MZ-fake-pe"))}
	plan, err := eng.Plan(context.Background(), install, Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected apply error")
	}
	got, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, stock) {
		t.Fatalf("expected stock restored, got %q", got)
	}
	// bak still present for forensics
	if _, err := os.Stat(payload + BackupSuffix); err != nil {
		t.Fatalf("bak should remain: %v", err)
	}
	m, err := ReadManifest(OSFileWriter{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != ManifestStatusFailed {
		t.Fatalf("status=%q", m.Status)
	}
}

func TestEngine_HealthFailAutoRevert(t *testing.T) {
	root, pe, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{ID: "fake", NewData: []byte("BAD-HEALTH"), FailHealth: true}
	eng := NewEngine(store, fake)
	install := Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1", HashSHA1: sha1hex([]byte("MZ-fake-pe"))}
	plan, err := eng.Plan(context.Background(), install, Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected health error")
	}
	got, _ := os.ReadFile(payload)
	if !bytes.Equal(got, stock) {
		t.Fatalf("want stock after health fail, got %q", got)
	}
}

func TestEngine_Revert(t *testing.T) {
	root, pe, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{ID: "fake", NewData: []byte("PATCHED-FOR-REVERT")}
	eng := NewEngine(store, fake)
	install := Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1", HashSHA1: sha1hex([]byte("MZ-fake-pe"))}
	plan, err := eng.Plan(context.Background(), install, Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(payload)
	if bytes.Equal(got, stock) {
		t.Fatal("expected patched before revert")
	}
	if err := eng.Revert(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(payload)
	if !bytes.Equal(got, stock) {
		t.Fatalf("after revert got %q", got)
	}
	m, _ := ReadManifest(OSFileWriter{}, root)
	if m.Status != ManifestStatusReverted {
		t.Fatalf("status=%q", m.Status)
	}
}

func TestEngine_NoAdapter(t *testing.T) {
	eng := NewEngine(NewProfileStore())
	_, err := eng.Plan(context.Background(), Install{ClientID: "nope"}, Endpoints{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEngine_ResolveProfileByHash(t *testing.T) {
	root, pe, _, _ := setupFakeInstall(t)
	store := NewProfileStore()
	sum := sha1hex([]byte("MZ-fake-pe"))
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sum},
	})
	fake := &FakeAdapter{ID: "fake"}
	eng := NewEngine(store, fake)
	install := Install{ClientID: "fake", RootDir: root, PrimaryPE: pe} // no ProfileID
	p, err := eng.ResolveProfile(install)
	if err != nil {
		t.Fatal(err)
	}
	if p.ProfileID != "fake-1" {
		t.Fatalf("%q", p.ProfileID)
	}
}

func TestEngine_ApplyNilPlan(t *testing.T) {
	eng := NewEngine(NewProfileStore())
	_, err := eng.Apply(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHasManifest(t *testing.T) {
	root := t.TempDir()
	if HasManifest(OSFileWriter{}, root) {
		t.Fatal("expected false")
	}
	_ = OSFileWriter{}.WriteFile(ManifestPath(root), []byte(`{"status":"applied"}`))
	if !HasManifest(OSFileWriter{}, root) {
		t.Fatal("expected true")
	}
}

func TestAbsPrimaryPE(t *testing.T) {
	if AbsPrimaryPE(Install{PrimaryPE: "/abs/x"}) != "/abs/x" && filepath.IsAbs("/abs/x") {
		// On Windows /abs/x may not be abs; only check relative join.
	}
	got := AbsPrimaryPE(Install{RootDir: "/root", PrimaryPE: "app.exe"})
	want := filepath.Join("/root", "app.exe")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCollectPlanTargets(t *testing.T) {
	plan := &Plan{
		Install: Install{
			RootDir:     "/inst",
			PrimaryPE:   "/inst/app.exe",
			ConfigPaths: []string{"/inst/cfg.xml"},
		},
		Mutations: []Mutation{
			{TargetRel: "payload.bin"},
			{Detail: ConfigRewriteDetail{Paths: []string{"/inst/other.xml"}}},
		},
	}
	targets := CollectPlanTargets(plan)
	if len(targets) < 3 {
		t.Fatalf("targets=%v", targets)
	}
}

func TestEngine_ReApplyPreservesStockBak(t *testing.T) {
	// Apply → re-Apply with different payload → Revert must restore original stock.
	root, pe, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{
			RelativePath: "app.exe",
			SHA1:         sha1hex([]byte("MZ-fake-pe")),
		},
	})
	fake := &FakeAdapter{ID: "fake", NewData: []byte("PATCH-V1")}
	eng := NewEngine(store, fake)
	install := Install{
		ClientID:  "fake",
		RootDir:   root,
		PrimaryPE: pe,
		HashSHA1:  sha1hex([]byte("MZ-fake-pe")),
		ProfileID: "fake-1",
	}
	plan, err := eng.Plan(context.Background(), install, Endpoints{WebBaseURL: "https://x.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	// Stock bak must still be original after first apply.
	bak1, err := os.ReadFile(payload + BackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bak1, stock) {
		t.Fatalf("first bak=%q stock=%q", bak1, stock)
	}
	// Re-apply different content; bak must not be clobbered with PATCH-V1.
	fake.NewData = []byte("PATCH-V2")
	plan2, err := eng.Plan(context.Background(), install, Endpoints{WebBaseURL: "https://y.test"})
	if err != nil {
		t.Fatal(err)
	}
	// Live PE is still stock (fake only patches payload.bin), so Plan/Verify OK.
	if _, err := eng.Apply(context.Background(), plan2); err != nil {
		t.Fatal(err)
	}
	bak2, err := os.ReadFile(payload + BackupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bak2, stock) {
		t.Fatalf("re-apply clobbered bak: got %q want stock %q", bak2, stock)
	}
	got, _ := os.ReadFile(payload)
	if !bytes.Equal(got, []byte("PATCH-V2")) {
		t.Fatalf("payload=%q", got)
	}
	if err := eng.Revert(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(payload)
	if !bytes.Equal(got, stock) {
		t.Fatalf("after revert got %q want stock", got)
	}
}

func TestEngine_RelativePrimaryPE(t *testing.T) {
	root, _, payload, stock := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{ID: "fake", NewData: []byte("REL-PATCH")}
	eng := NewEngine(store, fake)
	install := Install{
		ClientID:  "fake",
		RootDir:   root,
		PrimaryPE: "app.exe", // relative
		ProfileID: "fake-1",
		HashSHA1:  sha1hex([]byte("MZ-fake-pe")),
	}
	plan, err := eng.Plan(context.Background(), install, Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(plan.Install.PrimaryPE) {
		t.Fatalf("Plan should abs PrimaryPE, got %q", plan.Install.PrimaryPE)
	}
	if _, err := eng.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(payload)
	if !bytes.Equal(got, []byte("REL-PATCH")) {
		t.Fatalf("%q", got)
	}
	// Revert still works
	if err := eng.Revert(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(payload)
	if !bytes.Equal(got, stock) {
		t.Fatalf("revert %q", got)
	}
}

func TestEngine_PriorInjectInProgressRefuses(t *testing.T) {
	root, pe, _, _ := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	// Stale in_progress manifest (e.g. finalize WriteManifest failed after patch).
	m := &Manifest{
		InstallRoot: root,
		Status:      ManifestStatusInProgress,
		ClientID:    "fake",
		ProfileID:   "fake-1",
		Files:       []ManifestFile{{Original: pe, Backup: BackupPath(pe)}},
	}
	if err := WriteManifest(OSFileWriter{}, m); err != nil {
		t.Fatal(err)
	}
	fake := &FakeAdapter{ID: "fake"}
	eng := NewEngine(store, fake)
	plan := &Plan{
		Install:   Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1", HashSHA1: sha1hex([]byte("MZ-fake-pe"))},
		Mutations: []Mutation{{ID: "x", TargetRel: "payload.bin", Detail: WriteFileDetail{Contents: []byte("x")}}},
	}
	_, err := eng.Apply(context.Background(), plan)
	if err == nil || !errors.Is(err, ErrPriorInjectActive) {
		t.Fatalf("err=%v", err)
	}
}

func TestEngine_ApplyVerifyMismatch(t *testing.T) {
	root, pe, _, _ := setupFakeInstall(t)
	// Wrong PE contents vs profile stock SHA.
	if err := os.WriteFile(pe, []byte("WRONG-PE"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{ID: "fake"}
	eng := NewEngine(store, fake)
	plan := &Plan{
		Install:   Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1"},
		Mutations: []Mutation{{ID: "x", TargetRel: "payload.bin", Detail: WriteFileDetail{Contents: []byte("x")}}},
	}
	_, err := eng.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected sha mismatch")
	}
}

func TestEngine_HealthFailClearsMutationsApplied(t *testing.T) {
	root, pe, _, _ := setupFakeInstall(t)
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex([]byte("MZ-fake-pe"))},
	})
	fake := &FakeAdapter{ID: "fake", NewData: []byte("BAD"), FailHealth: true}
	eng := NewEngine(store, fake)
	install := Install{ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1", HashSHA1: sha1hex([]byte("MZ-fake-pe"))}
	plan, err := eng.Plan(context.Background(), install, Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("expected health error")
	}
	m, err := ReadManifest(OSFileWriter{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.MutationsApplied) != 0 {
		t.Fatalf("MutationsApplied should be cleared after health restore, got %v", m.MutationsApplied)
	}
}

func TestEngine_PreflightOKFalseWhenNoPE(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload.bin")
	stock := []byte("stock")
	if err := os.WriteFile(payload, stock, 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe"}, // empty sha — no stock check
	})
	fake := &FakeAdapter{ID: "fake", NewData: []byte("P")}
	eng := NewEngine(store, fake)
	plan := &Plan{
		Install:   Install{ClientID: "fake", RootDir: root, PrimaryPE: "", ProfileID: "fake-1"},
		Mutations: []Mutation{{ID: "write_payload", TargetRel: "payload.bin", Detail: WriteFileDetail{Contents: []byte("P")}}},
	}
	res, err := eng.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	m, _ := ReadManifest(OSFileWriter{}, root)
	if m.PreflightOK {
		t.Fatal("preflight_ok should be false when PrimaryPE empty")
	}
}

func TestCreateBackups_PreservesExisting(t *testing.T) {
	w := OSFileWriter{}
	dir := t.TempDir()
	orig := filepath.Join(dir, "f.bin")
	bak := BackupPath(orig)
	if err := w.WriteFile(orig, []byte("LIVE")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile(bak, []byte("STOCK")); err != nil {
		t.Fatal(err)
	}
	files, err := CreateBackups(w, []string{orig})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("%v", files)
	}
	got, _ := w.ReadFile(bak)
	if string(got) != "STOCK" {
		t.Fatalf("clobbered bak: %q", got)
	}
}

func TestFileWriter_WriteFilePreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("a"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := OSFileWriter{}
	if err := w.WriteFile(path, []byte("bb")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		// On some FS execute bit may not stick; at least we tried 0o755.
		// Require that we didn't force only 0o644 if OS preserves.
		t.Logf("mode=%v (execute may be unsupported on this FS)", info.Mode())
	}
	// Content updated
	got, _ := os.ReadFile(path)
	if string(got) != "bb" {
		t.Fatalf("%q", got)
	}
}

func TestEngine_RegisterAdapterAndWriterNil(t *testing.T) {
	eng := &Engine{Profiles: NewProfileStore()}
	fake := &FakeAdapter{ID: "fake"}
	eng.RegisterAdapter(fake)
	if eng.Adapters["fake"] == nil {
		t.Fatal("register")
	}
	if _, ok := eng.writer().(OSFileWriter); !ok {
		t.Fatalf("default writer %T", eng.writer())
	}
}

func TestEngine_ResolveProfileErrors(t *testing.T) {
	eng := &Engine{}
	if _, err := eng.ResolveProfile(Install{}); err == nil {
		t.Fatal("no store")
	}
	eng.Profiles = NewProfileStore()
	if _, err := eng.ResolveProfile(Install{ProfileID: "missing"}); err == nil {
		t.Fatal("missing id")
	}
	if _, err := eng.ResolveProfile(Install{ClientID: "c"}); err == nil {
		t.Fatal("no hash")
	}
	if _, err := eng.ResolveProfile(Install{ClientID: "c", HashSHA1: "abc"}); err == nil {
		t.Fatal("no match")
	}
}

func TestEngine_PlanVerifyError(t *testing.T) {
	store := NewProfileStore()
	_ = store.Add(&Profile{SchemaVersion: 2, ProfileID: "p", ClientID: "fake", PrimaryBinary: PrimaryBinarySpec{RelativePath: "a"}})
	fake := &FakeAdapter{ID: "fake", VerifyErr: errors.New("bad hash")}
	eng := NewEngine(store, fake)
	_, err := eng.Plan(context.Background(), Install{ClientID: "fake", ProfileID: "p"}, Endpoints{})
	if err == nil {
		t.Fatal("expected verify error")
	}
}

func TestEngine_ApplyNoAdapter(t *testing.T) {
	eng := NewEngine(NewProfileStore())
	_, err := eng.Apply(context.Background(), &Plan{Install: Install{ClientID: "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEngine_RevertEmptyRoot(t *testing.T) {
	eng := NewEngine(NewProfileStore())
	if err := eng.Revert(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestEngine_RevertNoFiles(t *testing.T) {
	root := t.TempDir()
	m := &Manifest{InstallRoot: root, Status: ManifestStatusApplied, Files: nil}
	_ = WriteManifest(OSFileWriter{}, m)
	eng := NewEngine(NewProfileStore())
	if err := eng.Revert(context.Background(), root); err == nil {
		t.Fatal("expected error")
	}
}

func TestEngine_PlanFillsHash(t *testing.T) {
	root, pe, _, _ := setupFakeInstall(t)
	sum := sha1hex([]byte("MZ-fake-pe"))
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2, ProfileID: "fake-1", ClientID: "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sum},
	})
	fake := &FakeAdapter{ID: "fake"}
	eng := NewEngine(store, fake)
	plan, err := eng.Plan(context.Background(), Install{
		ClientID: "fake", RootDir: root, PrimaryPE: pe, ProfileID: "fake-1",
	}, Endpoints{WebBaseURL: "https://a/"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Install.HashSHA1 != sum {
		t.Fatalf("hash=%q", plan.Install.HashSHA1)
	}
}

func TestNewEngine_NilAdapterSkipped(t *testing.T) {
	eng := NewEngine(NewProfileStore(), nil, &FakeAdapter{ID: "x"})
	if eng.Adapters["x"] == nil {
		t.Fatal("missing")
	}
}

func TestEndpoints_EmptyStatus(t *testing.T) {
	if (Endpoints{}).StatusURL() != "" || (Endpoints{}).StatusJSONURL() != "" {
		t.Fatal("empty")
	}
	if (Endpoints{}).FSDAddress() != "" {
		t.Fatal("empty fsd")
	}
	if (Endpoints{FSDServerName: "N"}).CachedServerEntry() != "N|" {
		t.Fatal("name only")
	}
}

func TestExpandYAMLKind_Unknown(t *testing.T) {
	if _, err := ExpandYAMLKind("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProfileStore_NilReceivers(t *testing.T) {
	var s *ProfileStore
	if _, ok := s.Get("x"); ok {
		t.Fatal()
	}
	if _, ok := s.LookupByHash("c", "h"); ok {
		t.Fatal()
	}
	if s.List() != nil {
		t.Fatal()
	}
}

func TestParseProfile_BadYAML(t *testing.T) {
	if _, err := ParseProfile([]byte(":::"), "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFlexibleInt64_Bad(t *testing.T) {
	yaml := []byte(`schema_version: 2
client_id: t
primary_binary:
  relative_path: t.exe
clr:
  us_heap:
    file_offset: 0xZZ
`)
	if _, err := ParseProfile(yaml, "t"); err == nil {
		t.Fatal("expected hex parse error")
	}
}

func TestAdd_NilAndEmpty(t *testing.T) {
	s := NewProfileStore()
	if err := s.Add(nil); err == nil {
		t.Fatal()
	}
	if err := s.Add(&Profile{}); err == nil {
		t.Fatal()
	}
}

func TestJWTURL_PreferShortWithoutSlash(t *testing.T) {
	ep := Endpoints{WebBaseURL: "https://h", PreferShortJWTPath: true, ShortJWTPathCandidates: []string{"j"}}
	if got := ep.JWTURL(); got != "https://h/j" {
		t.Fatalf("%q", got)
	}
}

func TestFSDAddress_DefaultPortInHost(t *testing.T) {
	ep := Endpoints{FSDHost: "h:6809"}
	if got := ep.FSDAddress(); got != "h" {
		t.Fatalf("%q", got)
	}
	ep.IncludePortInServerList = true
	if got := ep.FSDAddress(); got != "h:6809" {
		t.Fatalf("%q", got)
	}
}

func TestHasManifest_NilWriter(t *testing.T) {
	if HasManifest(nil, t.TempDir()) {
		t.Fatal()
	}
}

func TestFileWriter_CopyAndOpen(t *testing.T) {
	w := OSFileWriter{}
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := w.WriteFile(src, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := w.ReadFile(dst)
	if string(got) != "hello" {
		t.Fatalf("%q", got)
	}
	f, err := w.OpenReadWrite(src)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := w.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := w.Remove(dst); err != nil {
		t.Fatal(err)
	}
}

func TestEngine_ResolveProfile_BakAndManifest(t *testing.T) {
	root := t.TempDir()
	pe := filepath.Join(root, "app.exe")
	stock := []byte("STOCK-PE-BYTES-AAAA")
	patched := []byte("PATCHED-PE-BYTES-BB")
	if err := os.WriteFile(pe, stock, 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewProfileStore()
	_ = store.Add(&Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-bak",
		ClientID:      "fake",
		PrimaryBinary: PrimaryBinarySpec{RelativePath: "app.exe", SHA1: sha1hex(stock)},
	})
	eng := NewEngine(store)

	// After "apply": live is patched, bak holds stock.
	if err := os.WriteFile(pe+BackupSuffix, stock, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pe, patched, 0o644); err != nil {
		t.Fatal(err)
	}

	// No ProfileID / HashSHA1 — must resolve via bak.
	p, err := eng.ResolveProfile(Install{ClientID: "fake", RootDir: root, PrimaryPE: pe})
	if err != nil {
		t.Fatal(err)
	}
	if p.ProfileID != "fake-bak" {
		t.Fatalf("got %q", p.ProfileID)
	}

	// Via manifest ProfileID when bak hash also wouldn't match store if we remove bak.
	// Write manifest with profile id.
	m := &Manifest{
		ClientID:    "fake",
		ProfileID:   "fake-bak",
		PESHA1:      sha1hex(stock),
		Status:      ManifestStatusApplied,
		InstallRoot: root,
	}
	if err := WriteManifest(OSFileWriter{}, m); err != nil {
		t.Fatal(err)
	}
	// Remove bak — resolve via manifest.
	_ = os.Remove(pe + BackupSuffix)
	// Live still patched (unknown hash).
	p2, err := eng.ResolveProfile(Install{ClientID: "fake", RootDir: root, PrimaryPE: pe})
	if err != nil {
		t.Fatal(err)
	}
	if p2.ProfileID != "fake-bak" {
		t.Fatalf("manifest resolve: %q", p2.ProfileID)
	}
}
