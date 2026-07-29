package clientinject

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeRunner records the last process invocation without executing a PE.
type fakeRunner struct {
	mu     sync.Mutex
	calls  int
	name   string
	args   []string
	dir    string
	runErr error
	onRun  func(name string, args []string, dir string)
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.name = name
	f.args = append([]string(nil), args...)
	f.dir = dir
	if f.onRun != nil {
		f.onRun(name, args, dir)
	}
	return f.runErr
}

func boolPtr(v bool) *bool { return &v }

func TestIsPEMutationKind(t *testing.T) {
	if !IsPEMutationKind(MutUSHeapString) || !IsPEMutationKind(MutRawOverwrite) {
		t.Fatal("expected PE kinds")
	}
	if IsPEMutationKind(MutConfigRewrite) || IsPEMutationKind(MutLaunchFlag) {
		t.Fatal("config/launch must not be PE kinds")
	}
}

func TestLaunchArgsFromPlan(t *testing.T) {
	if LaunchArgsFromPlan(nil) != nil {
		t.Fatal("nil plan")
	}
	plan := &Plan{Mutations: []Mutation{
		{Kind: MutConfigRewrite},
		{Kind: MutLaunchFlag, Detail: LaunchFlagDetail{Args: []string{"-novoice", "-x"}}},
	}}
	got := LaunchArgsFromPlan(plan)
	if len(got) != 2 || got[0] != "-novoice" || got[1] != "-x" {
		t.Fatalf("got %v", got)
	}
}

func TestCollectShadowTargets_PEOnlyWhenDurable(t *testing.T) {
	root := t.TempDir()
	pe := filepath.Join(root, "app.exe")
	cfg := filepath.Join(root, "config.xml")
	plan := &Plan{
		Install: Install{RootDir: root, PrimaryPE: pe, ConfigPaths: []string{cfg}},
		Mutations: []Mutation{
			{Kind: MutUSHeapString, TargetRel: "app.exe"},
			{Kind: MutConfigRewrite, TargetRel: "config.xml", Detail: ConfigRewriteDetail{Paths: []string{cfg}}},
			{Kind: MutLaunchFlag, Detail: LaunchFlagDetail{Args: []string{"-novoice"}}},
		},
	}
	got := CollectShadowTargets(plan, true)
	if len(got) != 1 || got[0] != pe {
		t.Fatalf("durable shadow targets=%v want only PE", got)
	}
	gotAll := CollectShadowTargets(plan, false)
	if len(gotAll) != 2 {
		t.Fatalf("non-durable want PE+config, got %v", gotAll)
	}
}

func TestLaunchShadow_PatchesTempLeavesInstallStock_CleansTemp(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	payloadPath := filepath.Join(root, "payload.bin")
	stockPE := []byte("MZ-STOCK-PE-BYTES-AAAAAAAA")
	stockPayload := []byte("STOCK-PAYLOAD-CONTENT-XXXX")
	if err := os.WriteFile(pePath, stockPE, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payloadPath, stockPayload, 0o644); err != nil {
		t.Fatal(err)
	}

	// Side file that must never be copied (DLL / plugin stand-in).
	dllPath := filepath.Join(root, "plugin.dll")
	if err := os.WriteFile(dllPath, []byte("DLL-STOCK"), 0o644); err != nil {
		t.Fatal(err)
	}

	patched := []byte("OPENFSD-PATCHED-PAYLOAD")
	fa := &FakeAdapter{
		ID:        "fake",
		Name:      "Fake",
		Profiles:  []string{"fake-1"},
		TargetRel: "payload.bin",
		NewData:   patched,
	}
	// Profile store not required for LaunchShadow (no Plan/Resolve).
	eng := NewEngine(nil, fa)

	plan := &Plan{
		Install: Install{
			ClientID:  "fake",
			RootDir:   root,
			PrimaryPE: pePath,
		},
		Mutations: []Mutation{
			{
				ID:        "write_payload",
				Kind:      MutRawOverwrite,
				TargetRel: "payload.bin",
				Detail:    WriteFileDetail{Contents: patched},
			},
			{
				ID:     "launch_flags",
				Kind:   MutLaunchFlag,
				Detail: LaunchFlagDetail{Args: []string{"-serveraddressoverride", "h:6809", "-novoice"}},
			},
		},
	}

	var sawTempPayload []byte
	runner := &fakeRunner{
		onRun: func(name string, args []string, dir string) {
			if dir != root {
				t.Errorf("Dir=%q want install root %q", dir, root)
			}
			// name should be shadowed PE under temp, not install PE.
			if name == pePath {
				t.Errorf("launched install PE directly; want temp shadow")
			}
			if !strings.Contains(name, "openfsd-client-shadow-") && !strings.Contains(filepath.Dir(name), "shadow") {
				// TempDir may be custom; at least must not be install root for PE.
				if filepath.Dir(name) == root {
					t.Errorf("temp PE still under install root: %s", name)
				}
			}
			// Patched payload should live next to temp PE.
			tempPayload := filepath.Join(filepath.Dir(name), "payload.bin")
			b, err := os.ReadFile(tempPayload)
			if err != nil {
				t.Errorf("read temp payload: %v", err)
				return
			}
			sawTempPayload = append([]byte(nil), b...)
			// Install tree must still be stock while process "runs".
			livePE, _ := os.ReadFile(pePath)
			if !bytes.Equal(livePE, stockPE) {
				t.Errorf("install PE mutated during launch")
			}
			livePayload, _ := os.ReadFile(payloadPath)
			if !bytes.Equal(livePayload, stockPayload) {
				t.Errorf("install payload mutated during launch: %q", livePayload)
			}
			// DLL must never have been copied into temp.
			if _, err := os.Stat(filepath.Join(filepath.Dir(name), "plugin.dll")); err == nil {
				t.Errorf("plugin.dll was shadow-copied; hybrid must not full-tree copy")
			}
			if len(args) < 2 || args[0] != "-serveraddressoverride" {
				t.Errorf("args=%v", args)
			}
		},
	}

	// Track temp dir creation for cleanup assertion.
	var createdTemp string
	cfg := ShadowLaunchConfig{
		Runner: runner,
		MkdirTemp: func(dir, pattern string) (string, error) {
			d, err := os.MkdirTemp(dir, pattern)
			createdTemp = d
			return d, err
		},
	}

	session, err := eng.LaunchShadow(context.Background(), plan, nil, cfg)
	if err != nil {
		t.Fatalf("LaunchShadow: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls=%d", runner.calls)
	}
	if session == nil {
		t.Fatal("nil session")
	}
	if session.InstallPEFingerprint == "" {
		t.Fatal("expected install PE fingerprint")
	}
	// Install PE still stock after exit.
	livePE, err := os.ReadFile(pePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(livePE, stockPE) {
		t.Fatalf("install PE not stock after shadow launch: %q", livePE)
	}
	livePayload, _ := os.ReadFile(payloadPath)
	if !bytes.Equal(livePayload, stockPayload) {
		t.Fatalf("install payload not stock: %q", livePayload)
	}
	if !bytes.Equal(sawTempPayload, patched) {
		t.Fatalf("temp payload not patched: %q", sawTempPayload)
	}
	// Temp dir best-effort deleted.
	if createdTemp != "" {
		if _, err := os.Stat(createdTemp); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temp dir not cleaned: %s err=%v", createdTemp, err)
		}
	}
	// DLL untouched.
	dll, _ := os.ReadFile(dllPath)
	if !bytes.Equal(dll, []byte("DLL-STOCK")) {
		t.Fatal("dll mutated")
	}
}

func TestLaunchShadow_DurableConfigRewritesInstall(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	cfgPath := filepath.Join(root, "app.config")
	stockPE := []byte("MZ-STOCK")
	if err := os.WriteFile(pePath, stockPE, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("OLD-CONFIG"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Adapter that writes config via WriteFileDetail and PE-ish file via TargetRel.
	fa := &configAndPEAdapter{peRel: "app.exe", cfgPath: cfgPath, pePatch: []byte("SHADOW-PE"), cfgPatch: []byte("NEW-CONFIG")}
	eng := NewEngine(nil, fa)

	plan := &Plan{
		Install: Install{ClientID: "cfgpe", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{
			{
				ID:        "pe",
				Kind:      MutRawOverwrite,
				TargetRel: "app.exe",
				Detail:    WriteFileDetail{Contents: []byte("SHADOW-PE")},
			},
			{
				ID:        "cfg",
				Kind:      MutConfigRewrite,
				TargetRel: "app.config",
				Detail: WriteFileDetail{ // Fake path: adapter below handles MutConfigRewrite
					Contents: []byte("NEW-CONFIG"),
				},
			},
		},
	}

	runner := &fakeRunner{}
	session, err := eng.LaunchShadow(context.Background(), plan, []string{"-novoice"}, ShadowLaunchConfig{
		Runner:        runner,
		DurableConfig: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("LaunchShadow: %v", err)
	}
	_ = session

	// Install PE stock.
	gotPE, _ := os.ReadFile(pePath)
	if !bytes.Equal(gotPE, stockPE) {
		t.Fatalf("install PE mutated: %q", gotPE)
	}
	// Durable config applied to install.
	gotCfg, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(gotCfg, []byte("NEW-CONFIG")) {
		t.Fatalf("config not durably rewritten: %q", gotCfg)
	}
	if runner.dir != root {
		t.Fatalf("cwd=%q", runner.dir)
	}
	if runner.name == pePath {
		t.Fatal("should launch temp PE, not install PE")
	}
}

// configAndPEAdapter applies WriteFileDetail for PE kinds to TargetRel and for
// MutConfigRewrite to the absolute cfg path (durable).
type configAndPEAdapter struct {
	peRel, cfgPath    string
	pePatch, cfgPatch []byte
}

func (a *configAndPEAdapter) ClientID() string            { return "cfgpe" }
func (a *configAndPEAdapter) DisplayName() string         { return "cfgpe" }
func (a *configAndPEAdapter) SupportedProfiles() []string { return nil }
func (a *configAndPEAdapter) Discover(context.Context) ([]InstallCandidate, error) {
	return nil, nil
}
func (a *configAndPEAdapter) Verify(Install, *Profile) error            { return nil }
func (a *configAndPEAdapter) EndpointConstraints(*Profile) []Constraint { return nil }
func (a *configAndPEAdapter) Plan(Install, *Profile, Endpoints) (*Plan, error) {
	return nil, errors.New("unused")
}
func (a *configAndPEAdapter) HealthCheck(Install, Endpoints) error { return nil }
func (a *configAndPEAdapter) LaunchArgs(Install, Endpoints) []string {
	return []string{"-novoice"}
}

func (a *configAndPEAdapter) Apply(_ context.Context, plan *Plan, w FileWriter) error {
	for _, m := range plan.Mutations {
		switch m.Kind {
		case MutRawOverwrite, MutUSHeapString, MutPaddedString, MutAFVDisablePE, MutLdstrRemap:
			path := m.TargetRel
			if path == "" {
				path = AbsPrimaryPE(plan.Install)
			} else if !filepath.IsAbs(path) {
				path = filepath.Join(plan.Install.RootDir, path)
			}
			d, ok := m.Detail.(WriteFileDetail)
			if !ok {
				if p, ok2 := m.Detail.(*WriteFileDetail); ok2 && p != nil {
					d = *p
				} else {
					return errors.New("bad PE detail")
				}
			}
			if err := w.WriteFile(path, d.Contents); err != nil {
				return err
			}
		case MutConfigRewrite:
			// Always write durable path a.cfgPath (simulates absolute config paths).
			d, ok := m.Detail.(WriteFileDetail)
			if !ok {
				if p, ok2 := m.Detail.(*WriteFileDetail); ok2 && p != nil {
					d = *p
				} else {
					d = WriteFileDetail{Contents: a.cfgPatch}
				}
			}
			if err := w.WriteFile(a.cfgPath, d.Contents); err != nil {
				return err
			}
		case MutLaunchFlag:
			continue
		default:
			return errors.New("unsupported")
		}
	}
	return nil
}

func TestPrepareShadowLaunch_Blockers(t *testing.T) {
	_, _, err := PrepareShadowLaunch(&Plan{Blockers: []string{"x"}}, OSFileWriter{}, ShadowLaunchConfig{})
	if err == nil {
		t.Fatal("expected blockers error")
	}
}

func TestLaunchShadow_CleanupOnApplyFailure(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	if err := os.WriteFile(pePath, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	fa := &FakeAdapter{ID: "fake", Name: "F", FailApply: true, TargetRel: "payload.bin"}
	// Need payload for shadow copy? Fake apply fails before write — still prepare copies PE.
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := NewEngine(nil, fa)
	plan := &Plan{
		Install: Install{ClientID: "fake", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{{
			ID: "w", Kind: MutRawOverwrite, TargetRel: "payload.bin",
			Detail: WriteFileDetail{Contents: []byte("y")},
		}},
	}
	var created string
	_, err := eng.LaunchShadow(context.Background(), plan, nil, ShadowLaunchConfig{
		Runner: &fakeRunner{},
		MkdirTemp: func(dir, pattern string) (string, error) {
			d, err := os.MkdirTemp(dir, pattern)
			created = d
			return d, err
		},
	})
	if err == nil {
		t.Fatal("expected apply failure")
	}
	if created != "" {
		if _, st := os.Stat(created); !errors.Is(st, os.ErrNotExist) {
			t.Fatalf("temp not cleaned after apply fail: %v", st)
		}
	}
}

func TestLaunchShadow_KeepTemp(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	payload := filepath.Join(root, "payload.bin")
	_ = os.WriteFile(pePath, []byte("MZ"), 0o644)
	_ = os.WriteFile(payload, []byte("stock"), 0o644)
	fa := &FakeAdapter{ID: "fake", Name: "F", TargetRel: "payload.bin", NewData: []byte("new")}
	eng := NewEngine(nil, fa)
	plan := &Plan{
		Install: Install{ClientID: "fake", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{{
			ID: "w", Kind: MutRawOverwrite, TargetRel: "payload.bin",
			Detail: WriteFileDetail{Contents: []byte("new")},
		}},
	}
	var created string
	session, err := eng.LaunchShadow(context.Background(), plan, nil, ShadowLaunchConfig{
		KeepTemp: true,
		Runner:   &fakeRunner{},
		MkdirTemp: func(dir, pattern string) (string, error) {
			d, err := os.MkdirTemp(dir, pattern)
			created = d
			return d, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("KeepTemp should leave dir: %v", err)
	}
	// Manual cleanup for test hygiene.
	if err := session.Cleanup(); err != nil {
		// Cleanup is no-op when keepTemp — remove manually.
		_ = os.RemoveAll(created)
	} else {
		// keepTemp means Cleanup is no-op
		_ = os.RemoveAll(created)
	}
}

func TestShadowLaunch_NilEngine(t *testing.T) {
	_, err := ShadowLaunch(context.Background(), nil, &Plan{}, nil, ShadowLaunchConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
}
