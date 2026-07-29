package clientinject

import (
	"context"
	"errors"
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
//  2. Temp dir via os.MkdirTemp("", "openfsd-client-shadow-*") only when
//     files must be shadowed.
//  3. Copy only files that will be PE-mutated (typically vPilot.exe), preserving
//     path relative to install root under the temp tree (Phase 1 still usually
//     has a single PE at the install root).
//  4. Apply PE mutations only to temp copies — never to install PE/DLL paths.
//     Missing PE shadow sources are hard errors (no install fall-through).
//  5. Config: durable install rewrite by default, with .openfsd-bak before
//     rewrite and restore on apply failure.
//  6. Launch: exec temp PE (or stock install PE if no PE muts) with Dir = installRoot.
//  7. On exit / cancel: best-effort delete temp copies.
//
// Limitations (Phase 1): nested DLL loaders that resolve via absolute install
// paths may still need durable Apply; flat multi-dir basename collisions are
// refused. Temp cleanup is best-effort if the wrapper process is SIGKILL'd.
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

	// SkipPreflight disables running-client / lock probes (tests only).
	SkipPreflight bool
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
	// Empty when nothing was shadowed (config-only / launch-flag-only plans).
	TempDir string
	// TempPE is the absolute path of the shadowed primary PE, or empty when
	// no primary PE was copied (launch uses stock install PE via Exe).
	TempPE string
	// Exe is the absolute path that will be / was executed (temp or install PE).
	Exe string
	// LaunchArgs are CLI flags for the client process.
	LaunchArgs []string
	// ShadowedFiles maps install absolute path → temp absolute path.
	ShadowedFiles map[string]string
	// InstallPEFingerprint is a cheap fingerprint of install PE bytes before
	// launch so tests can assert the install PE was left stock.
	InstallPEFingerprint string
	// ConfigBackups lists durable config .openfsd-bak siblings created for
	// this session (stock snapshot; kept after successful launch).
	ConfigBackups []ManifestFile

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
// Safe to call multiple times. No-op when KeepTemp was set or TempDir is empty.
// Does not remove durable config or config .openfsd-bak files.
func (s *ShadowSession) Cleanup() error {
	if s == nil || s.cleaned || s.keepTemp || s.TempDir == "" {
		if s != nil {
			s.cleaned = true
		}
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
		} else {
			add(AbsPrimaryPE(plan.Install))
		}
	}
	if hasPEMut {
		// Always include primary PE so AbsPrimaryPE-based adapters patch temp.
		add(AbsPrimaryPE(plan.Install))
	}

	if !durableConfig {
		for _, p := range collectConfigTargets(plan) {
			add(p)
		}
	}
	return out
}

// collectConfigTargets returns absolute paths that MutConfigRewrite will write.
func collectConfigTargets(plan *Plan) []string {
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
	for _, c := range plan.Install.ConfigPaths {
		// Only include install config paths when a config rewrite is planned.
		// Avoid backing up unrelated configs with no mutation.
		_ = c
	}
	return out
}

// resolvePEMutationSource returns the absolute install path a PE mutation writes.
func resolvePEMutationSource(install Install, m Mutation) string {
	src := m.TargetRel
	if src == "" {
		src = AbsPrimaryPE(install)
	} else if !filepath.IsAbs(src) {
		src = filepath.Join(install.RootDir, src)
	}
	return filepath.Clean(src)
}

// PrepareShadowLaunch creates the temp dir (if needed), copies shadow targets,
// and returns a session plus a plan whose PE mutations target only temp copies.
// Mutations are not applied yet.
//
// Every PE-kind mutation must map to a successful shadow copy; otherwise an
// error is returned (install PE/DLL paths are never left as write targets).
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

	session := &ShadowSession{
		InstallRoot:   install.RootDir,
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

	// L1: no temp dir when nothing to shadow.
	if len(targets) == 0 {
		session.Exe = pe
		shadowPlan, err := clonePlanForShadow(plan, install, session, durable)
		if err != nil {
			return nil, nil, err
		}
		return session, shadowPlan, nil
	}

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
	session.TempDir = tempDir

	// Copy each target under temp, preserving path relative to install root
	// when the source lives under RootDir (M3 / future DLL shadow).
	// Collision on the same relative dest from two install paths is refused.
	destSeen := make(map[string]string) // dest → install path
	for _, src := range targets {
		if _, err := w.Stat(src); err != nil {
			// H1: PE/DLL sources are required. Config (non-durable) may be
			// created by Apply — only skip when not required by PE muts.
			if isRequiredPEShadowSource(plan, install, src) {
				_ = session.Cleanup()
				return nil, nil, fmt.Errorf("clientinject: shadow PE source missing %s: %w", src, err)
			}
			// Non-durable missing config: still map to a temp path for create.
			if !durable {
				dst, derr := shadowDestPath(tempDir, install.RootDir, src)
				if derr != nil {
					_ = session.Cleanup()
					return nil, nil, derr
				}
				if err := w.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
					_ = session.Cleanup()
					return nil, nil, err
				}
				session.ShadowedFiles[src] = dst
			}
			continue
		}
		dst, err := shadowDestPath(tempDir, install.RootDir, src)
		if err != nil {
			_ = session.Cleanup()
			return nil, nil, err
		}
		if prev, ok := destSeen[dst]; ok && prev != src {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: shadow dest collision %q (%s vs %s)", dst, prev, src)
		}
		destSeen[dst] = src
		if err := w.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: mkdir shadow parent %s: %w", filepath.Dir(dst), err)
		}
		if err := w.CopyFile(src, dst); err != nil {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: shadow copy %s: %w", src, err)
		}
		session.ShadowedFiles[src] = dst
		if pe != "" && filepath.Clean(src) == filepath.Clean(pe) {
			session.TempPE = dst
		}
	}

	// H1: every PE mutation must be remapped to a shadow copy.
	for _, m := range plan.Mutations {
		if !IsPEMutationKind(m.Kind) {
			continue
		}
		src := resolvePEMutationSource(install, m)
		if src == "" {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: PE mutation %q has empty target", m.ID)
		}
		if _, ok := session.ShadowedFiles[src]; !ok {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: PE mutation %q target %s was not shadowed (refuse install write)", m.ID, src)
		}
	}
	// Primary PE must be shadowed whenever any PE mutation exists (adapters may
	// open AbsPrimaryPE only).
	if hasPEMutation(plan) {
		if pe == "" {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: PE mutations planned but PrimaryPE empty")
		}
		if _, ok := session.ShadowedFiles[filepath.Clean(pe)]; !ok {
			_ = session.Cleanup()
			return nil, nil, fmt.Errorf("clientinject: primary PE %s was not shadowed", pe)
		}
		if session.TempPE == "" {
			session.TempPE = session.ShadowedFiles[filepath.Clean(pe)]
		}
	}

	if session.TempPE != "" {
		session.Exe = session.TempPE
	} else {
		session.Exe = pe
	}

	shadowPlan, err := clonePlanForShadow(plan, install, session, durable)
	if err != nil {
		_ = session.Cleanup()
		return nil, nil, err
	}
	return session, shadowPlan, nil
}

func hasPEMutation(plan *Plan) bool {
	if plan == nil {
		return false
	}
	for _, m := range plan.Mutations {
		if IsPEMutationKind(m.Kind) {
			return true
		}
	}
	return false
}

func isRequiredPEShadowSource(plan *Plan, install Install, src string) bool {
	src = filepath.Clean(src)
	for _, m := range plan.Mutations {
		if !IsPEMutationKind(m.Kind) {
			continue
		}
		if resolvePEMutationSource(install, m) == src {
			return true
		}
	}
	if pe := AbsPrimaryPE(install); pe != "" && filepath.Clean(pe) == src && hasPEMutation(plan) {
		return true
	}
	return false
}

// shadowDestPath maps an install path into the temp tree.
// Prefer path relative to install root (preserves Plugins/Foo.dll layout);
// fall back to basename when the source is outside RootDir.
func shadowDestPath(tempDir, installRoot, src string) (string, error) {
	src = filepath.Clean(src)
	installRoot = filepath.Clean(installRoot)
	rel, err := filepath.Rel(installRoot, src)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		base := filepath.Base(src)
		if base == "" || base == "." || base == string(filepath.Separator) {
			return "", fmt.Errorf("clientinject: cannot derive shadow dest for %s", src)
		}
		return filepath.Join(tempDir, base), nil
	}
	if rel == "." {
		return "", fmt.Errorf("clientinject: shadow dest is install root itself")
	}
	return filepath.Join(tempDir, rel), nil
}

// clonePlanForShadow redirects PE mutation targets to temp copies.
// Returns an error if any PE mutation cannot be remapped (H1).
func clonePlanForShadow(plan *Plan, install Install, session *ShadowSession, durableConfig bool) (*Plan, error) {
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
			src := resolvePEMutationSource(install, m)
			dst, ok := session.ShadowedFiles[src]
			if !ok {
				return nil, fmt.Errorf("clientinject: PE mutation %q target %s not in shadow map", m.ID, src)
			}
			// Always absolute temp path so adapters never join RootDir→install.
			nm.TargetRel = dst
		case m.Kind == MutConfigRewrite && !durableConfig:
			switch d := m.Detail.(type) {
			case ConfigRewriteDetail:
				nd := d
				paths, err := remapPathsRequired(d.Paths, session.ShadowedFiles, install.RootDir)
				if err != nil {
					return nil, err
				}
				nd.Paths = paths
				nm.Detail = nd
				if m.TargetRel != "" {
					tr, err := remapOneRequired(m.TargetRel, session.ShadowedFiles, install.RootDir)
					if err != nil {
						return nil, err
					}
					nm.TargetRel = tr
				}
			case *ConfigRewriteDetail:
				if d != nil {
					nd := *d
					paths, err := remapPathsRequired(d.Paths, session.ShadowedFiles, install.RootDir)
					if err != nil {
						return nil, err
					}
					nd.Paths = paths
					nm.Detail = &nd
				}
				if m.TargetRel != "" {
					tr, err := remapOneRequired(m.TargetRel, session.ShadowedFiles, install.RootDir)
					if err != nil {
						return nil, err
					}
					nm.TargetRel = tr
				}
			}
		}
		out.Mutations = append(out.Mutations, nm)
	}

	// Final safety: no PE mutation TargetRel may resolve under install root.
	for _, m := range out.Mutations {
		if !IsPEMutationKind(m.Kind) {
			continue
		}
		p := m.TargetRel
		if p == "" {
			p = AbsPrimaryPE(out.Install)
		}
		if p == "" {
			return nil, fmt.Errorf("clientinject: PE mutation %q has no target after shadow clone", m.ID)
		}
		if pathUnderRoot(p, install.RootDir) {
			return nil, fmt.Errorf("clientinject: refuse PE mutation %q targeting install path %s", m.ID, p)
		}
	}
	if hasPEMutation(out) && pathUnderRoot(AbsPrimaryPE(out.Install), install.RootDir) {
		return nil, fmt.Errorf("clientinject: refuse shadow plan with PrimaryPE under install root %s", AbsPrimaryPE(out.Install))
	}

	return out, nil
}

func pathUnderRoot(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func remapPathsRequired(paths []string, shadowed map[string]string, root string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		r, err := remapOneRequired(p, shadowed, root)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

func remapOneRequired(p string, shadowed map[string]string, root string) (string, error) {
	if p == "" {
		return p, nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	abs = filepath.Clean(abs)
	dst, ok := shadowed[abs]
	if !ok {
		return "", fmt.Errorf("clientinject: config path %s not shadowed", abs)
	}
	return dst, nil
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

// LaunchShadow prepares a hybrid shadow PE, applies mutations (PE→temp only;
// config→install when durable, with bak/restore on apply failure), runs the
// process with Dir=installRoot, then best-effort cleans the temp dir.
//
// ctx cancellation (e.g. signal.NotifyContext) aborts the child via
// CommandContext and still runs temp cleanup.
//
// On success, durable config remains rewritten (intentional). Config
// .openfsd-bak siblings are left as a stock snapshot. Install PE is never
// written.
//
// launchArgs overrides plan launch_flag args when non-nil (empty slice clears).
// Process non-zero exit returns *ProcessExitError (errors.Is ErrProcessExit)
// after cleanup — not a prepare/apply failure.
func (e *Engine) LaunchShadow(ctx context.Context, plan *Plan, launchArgs []string, cfg ShadowLaunchConfig) (session *ShadowSession, err error) {
	if plan == nil {
		return nil, fmt.Errorf("clientinject: nil plan")
	}
	if len(plan.Blockers) > 0 {
		return nil, fmt.Errorf("clientinject: plan has blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a, ok := e.Adapters[plan.Install.ClientID]
	if !ok {
		return nil, fmt.Errorf("clientinject: no adapter for client %q", plan.Install.ClientID)
	}
	w := e.writer()
	install := normalizeInstall(plan.Install)
	plan.Install = install

	// M4: refuse when client appears running / PE locked before any install write.
	if !cfg.SkipPreflight {
		if pe := AbsPrimaryPE(install); pe != "" {
			if err := PreflightPrimaryPE(pe); err != nil {
				return nil, err
			}
		}
		if cfg.durableConfig() {
			for _, p := range collectConfigTargets(plan) {
				if _, stErr := w.Stat(p); stErr != nil {
					continue // Apply may create missing config
				}
				if err := PreflightPrimaryPE(p); err != nil {
					// Map lock errors to ErrClientRunning; other open errors fail closed.
					return nil, fmt.Errorf("clientinject: preflight config %s: %w", p, err)
				}
			}
		}
	}

	session, shadowPlan, err := PrepareShadowLaunch(plan, w, cfg)
	if err != nil {
		return nil, err
	}
	// Always cleanup temp on the way out (cancel, apply fail, process exit).
	defer func() {
		if session == nil {
			return
		}
		if cerr := session.Cleanup(); cerr != nil {
			slog.Warn("clientinject shadow temp cleanup failed", "temp", session.TempDir, "err", cerr)
			if err == nil {
				err = cerr
			}
		}
	}()

	if err := ctx.Err(); err != nil {
		return session, fmt.Errorf("clientinject: shadow launch canceled: %w", err)
	}

	// H2: bak durable config targets before rewrite; restore on apply failure.
	var configBaks []ManifestFile
	if cfg.durableConfig() {
		cfgTargets := collectConfigTargets(plan)
		baks, berr := CreateBackups(w, cfgTargets)
		if berr != nil {
			return session, fmt.Errorf("clientinject: shadow config backup: %w", berr)
		}
		configBaks = baks
		session.ConfigBackups = baks
	}

	if err := a.Apply(ctx, shadowPlan, w); err != nil {
		if len(configBaks) > 0 {
			if rerr := RestoreBackups(w, configBaks); rerr != nil {
				slog.Error("clientinject shadow config restore after apply failure also failed",
					"err", rerr)
				return session, fmt.Errorf("clientinject: shadow apply failed (%w) and config restore failed (%v)", err, rerr)
			}
		}
		return session, fmt.Errorf("clientinject: shadow apply: %w", err)
	}

	args := session.LaunchArgs
	if launchArgs != nil {
		args = append([]string(nil), launchArgs...)
		session.LaunchArgs = args
	}

	exe := session.TempPE
	if exe == "" {
		// No PE mutations — launch stock install PE (TempPE stays empty — L2).
		exe = AbsPrimaryPE(plan.Install)
	}
	if exe == "" {
		// Restore durable config if we rewrote it but cannot launch.
		if len(configBaks) > 0 {
			_ = RestoreBackups(w, configBaks)
		}
		return session, fmt.Errorf("clientinject: no PE to launch")
	}
	session.Exe = exe

	if err := ctx.Err(); err != nil {
		// Apply already succeeded; durable config stays (intentional).
		return session, fmt.Errorf("clientinject: shadow launch canceled before exec: %w", err)
	}

	runErr := cfg.runner().Run(ctx, exe, args, session.InstallRoot)
	if runErr != nil {
		// Do not restore durable config on process exit — config was intentional.
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

// Ensure ProcessExitError is distinct for CLI mapping.
var _ error = (*ProcessExitError)(nil)

// IsProcessExit reports whether err is a client process non-zero exit.
func IsProcessExit(err error) bool {
	return errors.Is(err, ErrProcessExit)
}
