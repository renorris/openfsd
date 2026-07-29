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

func shadowCfg(r ProcessRunner) ShadowLaunchConfig {
	return ShadowLaunchConfig{Runner: r, SkipPreflight: true}
}

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
			if name == pePath {
				t.Errorf("launched install PE directly; want temp shadow")
			}
			if filepath.Dir(name) == root {
				t.Errorf("temp PE still under install root: %s", name)
			}
			// Relpath-preserving layout: payload.bin next to temp PE under temp root.
			tempPayload := filepath.Join(filepath.Dir(name), "payload.bin")
			b, err := os.ReadFile(tempPayload)
			if err != nil {
				t.Errorf("read temp payload: %v", err)
				return
			}
			sawTempPayload = append([]byte(nil), b...)
			livePE, _ := os.ReadFile(pePath)
			if !bytes.Equal(livePE, stockPE) {
				t.Errorf("install PE mutated during launch")
			}
			livePayload, _ := os.ReadFile(payloadPath)
			if !bytes.Equal(livePayload, stockPayload) {
				t.Errorf("install payload mutated during launch: %q", livePayload)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(name), "plugin.dll")); err == nil {
				t.Errorf("plugin.dll was shadow-copied; hybrid must not full-tree copy")
			}
			if len(args) < 2 || args[0] != "-serveraddressoverride" {
				t.Errorf("args=%v", args)
			}
		},
	}

	var createdTemp string
	cfg := shadowCfg(runner)
	cfg.MkdirTemp = func(dir, pattern string) (string, error) {
		d, err := os.MkdirTemp(dir, pattern)
		createdTemp = d
		return d, err
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
	if session.TempPE == "" || session.TempPE == pePath {
		t.Fatalf("TempPE should be shadow only, got %q", session.TempPE)
	}
	if session.Exe != session.TempPE {
		t.Fatalf("Exe=%q TempPE=%q", session.Exe, session.TempPE)
	}
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
	if createdTemp != "" {
		if _, err := os.Stat(createdTemp); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temp dir not cleaned: %s err=%v", createdTemp, err)
		}
	}
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
				Detail:    ConfigRewriteDetail{Paths: []string{cfgPath}},
			},
		},
	}

	runner := &fakeRunner{}
	cfg := shadowCfg(runner)
	cfg.DurableConfig = boolPtr(true)
	session, err := eng.LaunchShadow(context.Background(), plan, []string{"-novoice"}, cfg)
	if err != nil {
		t.Fatalf("LaunchShadow: %v", err)
	}
	if session.TempPE == pePath {
		t.Fatal("TempPE must not be install PE")
	}

	gotPE, _ := os.ReadFile(pePath)
	if !bytes.Equal(gotPE, stockPE) {
		t.Fatalf("install PE mutated: %q", gotPE)
	}
	gotCfg, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(gotCfg, []byte("NEW-CONFIG")) {
		t.Fatalf("config not durably rewritten: %q", gotCfg)
	}
	// Stock bak should exist after successful durable rewrite.
	if _, err := os.Stat(BackupPath(cfgPath)); err != nil {
		t.Fatalf("expected config bak: %v", err)
	}
	if runner.dir != root {
		t.Fatalf("cwd=%q", runner.dir)
	}
	if runner.name == pePath {
		t.Fatal("should launch temp PE, not install PE")
	}
}

func TestLaunchShadow_DurableConfigRestoredOnApplyFailure(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	cfgPath := filepath.Join(root, "app.config")
	_ = os.WriteFile(pePath, []byte("MZ"), 0o644)
	_ = os.WriteFile(cfgPath, []byte("OLD-CONFIG"), 0o644)

	fa := &configAndPEAdapter{
		peRel: "app.exe", cfgPath: cfgPath,
		pePatch: []byte("SHADOW-PE"), cfgPatch: []byte("NEW-CONFIG"),
		failAfterConfig: true,
	}
	eng := NewEngine(nil, fa)
	plan := &Plan{
		Install: Install{ClientID: "cfgpe", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{
			{
				ID: "cfg", Kind: MutConfigRewrite, TargetRel: "app.config",
				Detail: ConfigRewriteDetail{Paths: []string{cfgPath}},
			},
			{
				ID: "pe", Kind: MutRawOverwrite, TargetRel: "app.exe",
				Detail: WriteFileDetail{Contents: []byte("SHADOW-PE")},
			},
		},
	}
	var created string
	cfg := shadowCfg(&fakeRunner{})
	cfg.MkdirTemp = func(dir, pattern string) (string, error) {
		d, err := os.MkdirTemp(dir, pattern)
		created = d
		return d, err
	}
	_, err := eng.LaunchShadow(context.Background(), plan, nil, cfg)
	if err == nil {
		t.Fatal("expected apply failure")
	}
	got, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(got, []byte("OLD-CONFIG")) {
		t.Fatalf("config not restored after apply fail: %q", got)
	}
	if created != "" {
		if _, st := os.Stat(created); !errors.Is(st, os.ErrNotExist) {
			t.Fatalf("temp not cleaned: %v", st)
		}
	}
}

// H1: missing PE shadow source must refuse; install PE stays stock.
func TestPrepareShadowLaunch_MissingPETargetRefuses(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	_ = os.WriteFile(pePath, []byte("MZ-STOCK"), 0o644)
	// missing.dll is planned but not on disk.
	plan := &Plan{
		Install: Install{ClientID: "fake", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{{
			ID: "dll", Kind: MutRawOverwrite, TargetRel: "missing.dll",
			Detail: WriteFileDetail{Contents: []byte("X")},
		}},
	}
	_, _, err := PrepareShadowLaunch(plan, OSFileWriter{}, ShadowLaunchConfig{})
	if err == nil {
		t.Fatal("expected missing PE source error")
	}
	if !strings.Contains(err.Error(), "missing") && !strings.Contains(err.Error(), "not shadowed") {
		t.Fatalf("unexpected err: %v", err)
	}
	// Install PE untouched.
	got, _ := os.ReadFile(pePath)
	if !bytes.Equal(got, []byte("MZ-STOCK")) {
		t.Fatal("install PE mutated")
	}
}

func TestLaunchShadow_MissingPETargetDoesNotWriteInstall(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	_ = os.WriteFile(pePath, []byte("MZ-STOCK"), 0o644)
	// Adapter that would happily write TargetRel on install if not remapped.
	fa := &FakeAdapter{ID: "fake", Name: "F", TargetRel: "ghost.bin", NewData: []byte("BAD")}
	eng := NewEngine(nil, fa)
	plan := &Plan{
		Install: Install{ClientID: "fake", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{{
			ID: "ghost", Kind: MutRawOverwrite, TargetRel: "ghost.bin",
			Detail: WriteFileDetail{Contents: []byte("BAD")},
		}},
	}
	_, err := eng.LaunchShadow(context.Background(), plan, nil, shadowCfg(&fakeRunner{}))
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(root, "ghost.bin")); err == nil {
		t.Fatal("install ghost.bin must not be created")
	}
	got, _ := os.ReadFile(pePath)
	if !bytes.Equal(got, []byte("MZ-STOCK")) {
		t.Fatal("install PE mutated")
	}
}

func TestLaunchShadow_NonDurableConfigUsesTemp(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	cfgPath := filepath.Join(root, "app.config")
	_ = os.WriteFile(pePath, []byte("MZ"), 0o644)
	_ = os.WriteFile(cfgPath, []byte("OLD"), 0o644)

	fa := &configAndPEAdapter{peRel: "app.exe", cfgPath: cfgPath, writeConfigToMutationPath: true}
	eng := NewEngine(nil, fa)
	plan := &Plan{
		Install: Install{ClientID: "cfgpe", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{
			{
				ID: "pe", Kind: MutRawOverwrite, TargetRel: "app.exe",
				Detail: WriteFileDetail{Contents: []byte("SHADOW-PE")},
			},
			{
				ID: "cfg", Kind: MutConfigRewrite, TargetRel: "app.config",
				Detail: ConfigRewriteDetail{Paths: []string{cfgPath}},
			},
		},
	}
	var tempCfgDuringRun string
	runner := &fakeRunner{onRun: func(name string, _ []string, _ string) {
		// Config should have been written under temp, not install.
		// Adapter writes to mutation Paths[0] which is remapped.
		_ = name
	}}
	cfg := shadowCfg(runner)
	cfg.DurableConfig = boolPtr(false)
	// Capture apply via custom adapter path.
	session, err := eng.LaunchShadow(context.Background(), plan, nil, cfg)
	if err != nil {
		t.Fatalf("%v", err)
	}
	_ = session
	// Install config stock.
	got, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(got, []byte("OLD")) {
		t.Fatalf("install config changed in non-durable mode: %q", got)
	}
	_ = tempCfgDuringRun
}

func TestLaunchShadow_RunnerFailureStillCleansTemp(t *testing.T) {
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
	runner := &fakeRunner{runErr: &ProcessExitError{Name: "app.exe", ExitCode: 42}}
	cfg := shadowCfg(runner)
	cfg.MkdirTemp = func(dir, pattern string) (string, error) {
		d, err := os.MkdirTemp(dir, pattern)
		created = d
		return d, err
	}
	_, err := eng.LaunchShadow(context.Background(), plan, nil, cfg)
	if !IsProcessExit(err) {
		t.Fatalf("want process exit, got %v", err)
	}
	if created != "" {
		if _, st := os.Stat(created); !errors.Is(st, os.ErrNotExist) {
			t.Fatalf("temp not cleaned after runner fail: %v", st)
		}
	}
}

func TestLaunchShadow_ContextCancelBeforeRun(t *testing.T) {
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var created string
	cfg := shadowCfg(&fakeRunner{})
	cfg.MkdirTemp = func(dir, pattern string) (string, error) {
		d, err := os.MkdirTemp(dir, pattern)
		created = d
		return d, err
	}
	_, err := eng.LaunchShadow(ctx, plan, nil, cfg)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if created != "" {
		if _, st := os.Stat(created); !errors.Is(st, os.ErrNotExist) {
			t.Fatalf("temp not cleaned on cancel: %v", st)
		}
	}
}

func TestLaunchShadow_NoPEMutationLeavesTempPEEmpty(t *testing.T) {
	root := t.TempDir()
	pePath := filepath.Join(root, "app.exe")
	_ = os.WriteFile(pePath, []byte("MZ"), 0o644)
	fa := &FakeAdapter{ID: "fake", Name: "F"}
	eng := NewEngine(nil, fa)
	plan := &Plan{
		Install: Install{ClientID: "fake", RootDir: root, PrimaryPE: pePath},
		Mutations: []Mutation{{
			ID: "lf", Kind: MutLaunchFlag, Detail: LaunchFlagDetail{Args: []string{"-novoice"}},
		}},
	}
	runner := &fakeRunner{}
	session, err := eng.LaunchShadow(context.Background(), plan, nil, shadowCfg(runner))
	if err != nil {
		t.Fatal(err)
	}
	if session.TempPE != "" {
		t.Fatalf("TempPE should be empty when no PE shadow, got %q", session.TempPE)
	}
	if session.Exe != pePath {
		t.Fatalf("Exe=%q want install PE", session.Exe)
	}
	if session.TempDir != "" {
		t.Fatalf("no temp dir expected, got %q", session.TempDir)
	}
	if runner.name != pePath {
		t.Fatalf("launched %q", runner.name)
	}
}

func TestShadowDestPath_PreservesRelpath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "inst")
	src := filepath.Join(root, "Plugins", "Foo.dll")
	dst, err := shadowDestPath("/tmp/shadow", root, src)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/shadow", "Plugins", "Foo.dll")
	if dst != want {
		t.Fatalf("got %q want %q", dst, want)
	}
}

// configAndPEAdapter applies WriteFileDetail for PE kinds to TargetRel and for
// MutConfigRewrite to the absolute cfg path (durable) or mutation path.
type configAndPEAdapter struct {
	peRel, cfgPath            string
	pePatch, cfgPatch         []byte
	failAfterConfig           bool
	writeConfigToMutationPath bool
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
	// Apply config first so failAfterConfig exercises restore.
	for _, m := range plan.Mutations {
		if m.Kind != MutConfigRewrite {
			continue
		}
		path := a.cfgPath
		if a.writeConfigToMutationPath {
			if d, ok := m.Detail.(ConfigRewriteDetail); ok && len(d.Paths) > 0 {
				path = d.Paths[0]
			} else if m.TargetRel != "" {
				path = m.TargetRel
				if !filepath.IsAbs(path) {
					path = filepath.Join(plan.Install.RootDir, path)
				}
			}
		}
		contents := a.cfgPatch
		if len(contents) == 0 {
			contents = []byte("NEW-CONFIG")
		}
		if err := w.WriteFile(path, contents); err != nil {
			return err
		}
		if a.failAfterConfig {
			return errors.New("fake fail after config")
		}
	}
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
		case MutConfigRewrite, MutLaunchFlag:
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
	cfg := shadowCfg(&fakeRunner{})
	cfg.MkdirTemp = func(dir, pattern string) (string, error) {
		d, err := os.MkdirTemp(dir, pattern)
		created = d
		return d, err
	}
	_, err := eng.LaunchShadow(context.Background(), plan, nil, cfg)
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
	cfg := shadowCfg(&fakeRunner{})
	cfg.KeepTemp = true
	cfg.MkdirTemp = func(dir, pattern string) (string, error) {
		d, err := os.MkdirTemp(dir, pattern)
		created = d
		return d, err
	}
	session, err := eng.LaunchShadow(context.Background(), plan, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("KeepTemp should leave dir: %v", err)
	}
	_ = session
	_ = os.RemoveAll(created)
}

func TestShadowLaunch_NilEngine(t *testing.T) {
	_, err := ShadowLaunch(context.Background(), nil, &Plan{}, nil, ShadowLaunchConfig{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessExitError_Is(t *testing.T) {
	err := &ProcessExitError{Name: "x", ExitCode: 2}
	if !errors.Is(err, ErrProcessExit) {
		t.Fatal("expected Is ErrProcessExit")
	}
	if !IsProcessExit(err) {
		t.Fatal("IsProcessExit")
	}
}
