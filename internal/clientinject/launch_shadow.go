package clientinject

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ShadowLaunchConfig controls hybrid Phase 1 shadow PE launch (Appendix B).
//
// Default hybrid algorithm:
//  1. Install root stays intact (DLLs/plugins stay put).
//  2. Temp dir via os.MkdirTemp("", "openfsd-client-shadow-*").
//  3. Copy only files that will be PE-mutated (typically vPilot.exe).
//  4. Apply PE mutations to temp copies.
//  5. Config: durable install rewrite by default (not temp).
//  6. Launch: exec temp PE with Dir = installRoot.
//  7. On exit: best-effort delete temp copies.
type ShadowLaunchConfig struct {
	// DurableConfig applies config rewrites to the install root (default true).
	// When false, config mutation targets are also copied into the temp tree
	// (research escape hatch; not the Phase 1 default).
	DurableConfig *bool

	// KeepTemp skips temp-dir deletion after the process exits (debug/tests).
	KeepTemp bool

	// TempDir, when non-empty, is used instead of MkdirTemp (tests).
	TempDir string

	// Runner defaults to DefaultProcessRunner.
	Runner ProcessRunner

	// MkdirTemp overrides os.MkdirTemp (tests).
	MkdirTemp func(dir, pattern string) (string, error)

	// RemoveAll overrides os.RemoveAll (tests).
	RemoveAll func(path string) error
}

func (c ShadowLaunchConfig) durableConfig() bool {
	if c.DurableConfig == nil {
		return true
	}
	return *c.DurableConfig
}

func (c ShadowLaunchConfig) runner() ProcessRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return DefaultProcessRunner{}
}

func (c ShadowLaunchConfig) mkdirTemp(dir, pattern string) (string, error) {
	if c.MkdirTemp != nil {
		return c.MkdirTemp(dir, pattern)
	}
	return os.MkdirTemp(dir, pattern)
}

func (c ShadowLaunchConfig) removeAll(path string) error {
	if c.RemoveAll != nil {
		return c.RemoveAll(path)
	}
	return os.RemoveAll(path)
}

// ShadowSession describes a prepared hybrid shadow environment.
type ShadowSession struct {
	// InstallRoot is the user install tree (CWD for the launched PE).
	InstallRoot string
	// TempDir holds shadowed PE copies (and optional non-durable config).
	TempDir string
	// TempPE is the absolute path of the shadowed primary PE.
	TempPE string
	// LaunchArgs are CLI flags for the client process.
	LaunchArgs []string
	// ShadowedFiles maps install absolute path → temp absolute path.
	ShadowedFiles map[string]string
	// StockPEChecksum is a cheap fingerprint of install PE bytes before launch
	// (len + first 32 bytes hex) so tests can assert the install PE was left stock.
	// Empty when no primary PE was shadowed.
	InstallPEFingerprint string

	keepTemp  bool
	removeAll func(path string) error
	cleaned   bool
}

// KeptTemp reports whether Cleanup intentionally left the temp directory
// (ShadowLaunchConfig.KeepTemp).
func (s *ShadowSession) KeptTemp() bool {
	return s != nil && s.keepTemp
}

// Cleanup best-effort removes the temp shadow directory.
// Safe to call multiple times. No-op when KeepTemp was set.
func (s *ShadowSession) Cleanup() error {
	if s == nil || s.cleaned || s.keepTemp || s.TempDir == "" {
		return nil
	}
	s.cleaned = true
	rm := s.removeAll
	if rm == nil {
		rm = os.RemoveAll
	}
	if err := rm(s.TempDir); err != nil {
		return fmt.Errorf("clientinject: shadow cleanup %s: %w", s.TempDir, err)
	}
	return nil
}

// CollectShadowTargets returns unique absolute install paths that must be
// copied into the shadow temp dir (PE mutation targets only when durableConfig).
// Config paths are excluded when durableConfig is true.
func CollectShadowTargets(plan *Plan, durableConfig bool) []string {
	if plan == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(plan.Install.RootDir, p)
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	hasPEMut := false
	for _, m := range plan.Mutations {
		if !IsPEMutationKind(m.Kind) {
			continue
		}
		hasPEMut = true
		if m.TargetRel != "" {
			add(m.TargetRel)
		}
	}
	if hasPEMut {
		add(AbsPrimaryPE(plan.Install))
	}

	if !durableConfig {
		// Escape hatch: also shadow config rewrite targets.
		for _, m := range plan.Mutations {
			if m.Kind != MutConfigRewrite {
				continue
			}
			if m.TargetRel != "" {
				add(m.TargetRel)
			}
			switch d := m.Detail.(type) {
			case ConfigRewriteDetail:
				for _, p := range d.Paths {
					add(p)
				}
			case *ConfigRewriteDetail:
				if d != nil {
					for _, p := range d.Paths {
						add(p)
					}
				}
			}
		}
	}
	return out
}

// PrepareShadowLaunch creates the temp dir, copies shadow targets, and returns
// a session plus a plan whose PE mutations target the temp copies.
// Mutations are not applied yet.
//
// When durableConfig is true (default), config mutations still point at the
// install tree; only PE/DLL mutation targets are redirected.
func PrepareShadowLaunch(plan *Plan, w FileWriter, cfg ShadowLaunchConfig) (*ShadowSession, *Plan, error) {
	if plan == nil {
		return nil, nil, fmt.Errorf("clientinject: nil plan")
	}
	if len(plan.Blockers) > 0 {
		return nil, nil, fmt.Errorf("clientinject: plan has blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	if w == nil {
		w = OSFileWriter{}
	}
	install := normalizeInstall(plan.Install)
	if install.RootDir == "" {
		return nil, nil, fmt.Errorf("clientinject: empty install root")
	}

	durable := cfg.durableConfig()
	targets := CollectShadowTargets(plan, durable)

	tempDir := cfg.TempDir
	if tempDir == "" {
		d, err := cfg.mkdirTemp("", "openfsd-client-shadow-*")
		if err != nil {
			return nil, nil, fmt.Errorf("clientinject: mkdir temp shadow: %w", err)
		}
		tempDir = d
	} else {
		if err := w.MkdirAll(tempDir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("clientinject: mkdir shadow dir %s: %w", tempDir, err)
		}
	}

	session := &ShadowSession{
		InstallRoot:   install.RootDir,
		TempDir:       tempDir,
		LaunchArgs:    LaunchArgsFromPlan(plan),
		ShadowedFiles: make(map[string]string),
		keepTemp:      cfg.KeepTemp,
		removeAll:     cfg.removeAll,
	}

	// Fingerprint install primary PE before any copy (stock proof).
	pe := AbsPrimaryPE(install)
	if pe != "" {
		if data, err := w.ReadFile(pe); err == nil {
			session.InstallPEFingerprint = peFingerprint(data)
		}
	}

	// Copy each target into temp by basename (flat; no full-tree copy — KD-21).
	// Collision on basename is refused.
	baseSeen := make(map[string]string) // basename → install path
	for _, src := range targets {
		if _, err := w.Stat(src); err != nil {
			// Missing targets skipped (Apply may create config when non-durable).
			continue
		}
		base := filepath.Base(src)
		if prev, ok := baseSeen[base]; ok && prev != src {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: shadow basename collision %q (%s vs %s)", base, prev, src)
		}
		baseSeen[base] = src
		dst := filepath.Join(tempDir, base)
		if err := w.CopyFile(src, dst); err != nil {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: shadow copy %s: %w", src, err)
		}
		session.ShadowedFiles[src] = dst
		if pe != "" && filepath.Clean(src) == filepath.Clean(pe) {
			session.TempPE = dst
		}
	}

	// If no PE mutation targets were copied but we have a primary PE, still
	// shadow the PE when the plan intends to launch a stock binary from temp
	// (launch_flag-only plans). Prefer launching install PE with Dir=root in
	// that case — TempPE stays empty and LaunchShadow uses install PE.
	// (Design: copy only files that will be mutated.)

	shadowPlan := clonePlanForShadow(plan, install, session, durable)
	return session, shadowPlan, nil
}

// clonePlanForShadow redirects PE mutation targets to temp copies.
func clonePlanForShadow(plan *Plan, install Install, session *ShadowSession, durableConfig bool) *Plan {
	out := &Plan{
		Install:     install,
		Endpoints:   plan.Endpoints,
		Mutations:   make([]Mutation, 0, len(plan.Mutations)),
		Constraints: append([]Constraint(nil), plan.Constraints...),
		Warnings:    append([]string(nil), plan.Warnings...),
		Blockers:    append([]string(nil), plan.Blockers...),
	}
	if session.TempPE != "" {
		out.Install.PrimaryPE = session.TempPE
	}

	for _, m := range plan.Mutations {
		nm := m
		switch {
		case IsPEMutationKind(m.Kind):
			// Redirect TargetRel to the temp absolute path when shadowed.
			src := m.TargetRel
			if src == "" {
				src = AbsPrimaryPE(install)
			} else if !filepath.IsAbs(src) {
				src = filepath.Join(install.RootDir, src)
			}
			src = filepath.Clean(src)
			if dst, ok := session.ShadowedFiles[src]; ok {
				nm.TargetRel = dst
			}
		case m.Kind == MutConfigRewrite && !durableConfig:
			// Rewrite config paths into the temp tree.
			switch d := m.Detail.(type) {
			case ConfigRewriteDetail:
				nd := d
				nd.Paths = remapPaths(d.Paths, session.ShadowedFiles, install.RootDir)
				nm.Detail = nd
				if m.TargetRel != "" {
					nm.TargetRel = remapOne(m.TargetRel, session.ShadowedFiles, install.RootDir)
				}
			case *ConfigRewriteDetail:
				if d != nil {
					nd := *d
					nd.Paths = remapPaths(d.Paths, session.ShadowedFiles, install.RootDir)
					nm.Detail = &nd
				}
				if m.TargetRel != "" {
					nm.TargetRel = remapOne(m.TargetRel, session.ShadowedFiles, install.RootDir)
				}
			}
		}
		out.Mutations = append(out.Mutations, nm)
	}
	return out
}

func remapPaths(paths []string, shadowed map[string]string, root string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = remapOne(p, shadowed, root)
	}
	return out
}

func remapOne(p string, shadowed map[string]string, root string) string {
	if p == "" {
		return p
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	if dst, ok := shadowed[abs]; ok {
		return dst
	}
	// Not copied (missing source): place under temp by basename when possible.
	// Callers that need creation should have copied or accept install path.
	return p
}

func peFingerprint(data []byte) string {
	n := len(data)
	const head = 32
	h := data
	if len(h) > head {
		h = h[:head]
	}
	return fmt.Sprintf("%d:%x", n, h)
}

// LaunchShadow prepares a hybrid shadow PE, applies mutations (PE→temp,
// config→install when durable), runs the process with Dir=installRoot, then
// best-effort cleans the temp dir.
//
// The install primary PE is left stock when only PE mutations were shadowed.
// Config is rewritten durably by default so CachedServers/status persist.
//
// launchArgs overrides plan launch_flag args when non-nil (empty slice clears).
func (e *Engine) LaunchShadow(ctx context.Context, plan *Plan, launchArgs []string, cfg ShadowLaunchConfig) (*ShadowSession, error) {
	if plan == nil {
		return nil, fmt.Errorf("clientinject: nil plan")
	}
	if len(plan.Blockers) > 0 {
		return nil, fmt.Errorf("clientinject: plan has blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	a, ok := e.Adapters[plan.Install.ClientID]
	if !ok {
		return nil, fmt.Errorf("clientinject: no adapter for client %q", plan.Install.ClientID)
	}
	w := e.writer()

	session, shadowPlan, err := PrepareShadowLaunch(plan, w, cfg)
	if err != nil {
		return nil, err
	}

	// Apply mutations: PE to temp (via redirected PrimaryPE), config durable.
	if err := a.Apply(ctx, shadowPlan, w); err != nil {
		_ = session.Cleanup()
		return nil, fmt.Errorf("clientinject: shadow apply: %w", err)
	}

	args := session.LaunchArgs
	if launchArgs != nil {
		args = append([]string(nil), launchArgs...)
		session.LaunchArgs = args
	}

	exe := session.TempPE
	if exe == "" {
		// No PE mutations — launch stock install PE from install root.
		exe = AbsPrimaryPE(plan.Install)
	}
	if exe == "" {
		_ = session.Cleanup()
		return nil, fmt.Errorf("clientinject: no PE to launch")
	}
	session.TempPE = exe // may be install PE when nothing was shadowed

	runner := cfg.runner()
	runErr := runner.Run(ctx, exe, args, session.InstallRoot)

	// Always try cleanup after process exit (or run failure).
	if cerr := session.Cleanup(); cerr != nil {
		slog.Warn("clientinject shadow temp cleanup failed", "temp", session.TempDir, "err", cerr)
		if runErr == nil {
			runErr = cerr
		}
	}
	if runErr != nil {
		return session, runErr
	}
	return session, nil
}

// ShadowLaunch is a package-level convenience using the engine's writer/adapters.
// See Engine.LaunchShadow.
func ShadowLaunch(ctx context.Context, e *Engine, plan *Plan, launchArgs []string, cfg ShadowLaunchConfig) (*ShadowSession, error) {
	if e == nil {
		return nil, fmt.Errorf("clientinject: nil engine")
	}
	return e.LaunchShadow(ctx, plan, launchArgs, cfg)
}
