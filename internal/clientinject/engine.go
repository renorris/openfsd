package clientinject

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// Engine orchestrates plan/apply/revert across adapters and profiles.
type Engine struct {
	Adapters map[string]Adapter
	Profiles *ProfileStore
	Writer   FileWriter
}

// NewEngine constructs an Engine with OS file writer.
func NewEngine(profiles *ProfileStore, adapters ...Adapter) *Engine {
	e := &Engine{
		Adapters: make(map[string]Adapter),
		Profiles: profiles,
		Writer:   OSFileWriter{},
	}
	for _, a := range adapters {
		if a == nil {
			continue
		}
		e.Adapters[a.ClientID()] = a
	}
	return e
}

// RegisterAdapter adds or replaces an adapter by ClientID.
func (e *Engine) RegisterAdapter(a Adapter) {
	if e.Adapters == nil {
		e.Adapters = make(map[string]Adapter)
	}
	e.Adapters[a.ClientID()] = a
}

func (e *Engine) writer() FileWriter {
	if e.Writer != nil {
		return e.Writer
	}
	return OSFileWriter{}
}

// ResolveProfile picks a profile for install (by ProfileID or PE hash).
func (e *Engine) ResolveProfile(install Install) (*Profile, error) {
	if e.Profiles == nil {
		return nil, fmt.Errorf("clientinject: no profile store")
	}
	if install.ProfileID != "" {
		if p, ok := e.Profiles.Get(install.ProfileID); ok {
			return p, nil
		}
		return nil, fmt.Errorf("clientinject: profile %q not found", install.ProfileID)
	}
	sha := install.HashSHA1
	if sha == "" && install.PrimaryPE != "" {
		sum, err := fileSHA1(e.writer(), install.PrimaryPE)
		if err != nil {
			return nil, fmt.Errorf("clientinject: hash PE: %w", err)
		}
		sha = sum
	}
	if sha == "" {
		return nil, fmt.Errorf("clientinject: cannot resolve profile without ProfileID or PE hash")
	}
	p, ok := e.Profiles.LookupByHash(install.ClientID, sha)
	if !ok {
		return nil, fmt.Errorf("clientinject: no profile for client %q sha1=%s", install.ClientID, sha)
	}
	return p, nil
}

func fileSHA1(w FileWriter, path string) (string, error) {
	data, err := w.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:]), nil
}

// normalizeInstall makes PrimaryPE absolute under RootDir when relative.
func normalizeInstall(install Install) Install {
	if pe := AbsPrimaryPE(install); pe != "" {
		install.PrimaryPE = pe
	}
	return install
}

// Plan runs adapter.Plan after Verify. Does not write disk.
func (e *Engine) Plan(ctx context.Context, install Install, ep Endpoints) (*Plan, error) {
	_ = ctx
	install = normalizeInstall(install)
	a, ok := e.Adapters[install.ClientID]
	if !ok {
		return nil, fmt.Errorf("clientinject: no adapter for client %q", install.ClientID)
	}
	profile, err := e.ResolveProfile(install)
	if err != nil {
		return nil, err
	}
	// Fill install metadata when empty.
	if install.ProfileID == "" {
		install.ProfileID = profile.ProfileID
	}
	if install.HashSHA1 == "" && install.PrimaryPE != "" {
		if sum, err := fileSHA1(e.writer(), install.PrimaryPE); err == nil {
			install.HashSHA1 = sum
		}
	}
	if err := a.Verify(install, profile); err != nil {
		return nil, fmt.Errorf("clientinject: verify: %w", err)
	}
	plan, err := a.Plan(install, profile, ep.Normalize())
	if err != nil {
		return nil, fmt.Errorf("clientinject: plan: %w", err)
	}
	if plan == nil {
		return nil, fmt.Errorf("clientinject: adapter returned nil plan")
	}
	plan.Install = install
	plan.Endpoints = ep.Normalize()
	return plan, nil
}

// Apply runs preflight, plan blockers check, backup, adapter.Apply, healthcheck
// with automatic restore on failure.
//
// Preflight is a probe only (lock released before mutations). PrimaryPE is
// normalized via AbsPrimaryPE. Existing .openfsd-bak files are never clobbered
// so re-Apply keeps stock for Revert. Incomplete prior inject (manifest
// status=in_progress) must be Reverted first — Revert uses bak even if status
// is still in_progress (e.g. finalize WriteManifest failed after a successful
// patch).
func (e *Engine) Apply(ctx context.Context, plan *Plan) (*ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("clientinject: nil plan")
	}
	if len(plan.Blockers) > 0 {
		return nil, fmt.Errorf("clientinject: plan has blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	// Normalize PrimaryPE to absolute under RootDir for preflight + bak paths.
	plan.Install = normalizeInstall(plan.Install)

	a, ok := e.Adapters[plan.Install.ClientID]
	if !ok {
		return nil, fmt.Errorf("clientinject: no adapter for client %q", plan.Install.ClientID)
	}
	w := e.writer()

	// Refuse incomplete prior inject (status=in_progress). Applied/failed/reverted OK.
	if err := CheckPriorInject(w, plan.Install.RootDir); err != nil {
		return nil, err
	}

	// Re-resolve profile + Verify at Apply entry (Plan may be stale / caller-built).
	profile, err := e.ResolveProfile(plan.Install)
	if err != nil {
		return nil, err
	}
	if plan.Install.ProfileID == "" {
		plan.Install.ProfileID = profile.ProfileID
	}
	// On first apply (no stock bak for PE yet), PE must match profile stock SHA.
	// Re-apply after a prior successful inject keeps stock in .openfsd-bak while
	// the live PE may already be patched — skip live-hash stock check then.
	if plan.Install.PrimaryPE != "" {
		want := strings.ToLower(strings.TrimSpace(profile.PrimaryBinary.SHA1))
		bak := BackupPath(plan.Install.PrimaryPE)
		if _, bakErr := w.Stat(bak); bakErr != nil && want != "" {
			sum, err := fileSHA1(w, plan.Install.PrimaryPE)
			if err != nil {
				return nil, fmt.Errorf("clientinject: hash PE for verify: %w", err)
			}
			if !strings.EqualFold(sum, want) {
				return nil, fmt.Errorf("clientinject: PE sha1 %s does not match profile %s stock %s",
					sum, profile.ProfileID, want)
			}
			plan.Install.HashSHA1 = sum
		} else if plan.Install.HashSHA1 == "" {
			if sum, err := fileSHA1(w, plan.Install.PrimaryPE); err == nil {
				plan.Install.HashSHA1 = sum
			}
		}
	}
	if err := a.Verify(plan.Install, profile); err != nil {
		return nil, fmt.Errorf("clientinject: verify: %w", err)
	}

	// Preflight: exclusive open probe on absolute primary PE (lock not held across Apply).
	preflightOK := false
	if plan.Install.PrimaryPE != "" {
		if err := PreflightPrimaryPE(plan.Install.PrimaryPE); err != nil {
			return nil, err
		}
		preflightOK = true
	}

	targets := CollectPlanTargets(plan)
	files, err := CreateBackups(w, targets)
	if err != nil {
		return nil, err
	}
	manifest := NewManifest(plan, files, preflightOK)
	if err := WriteManifest(w, manifest); err != nil {
		// Best-effort restore only for newly created baks is complex; leave baks.
		// Disk originals unchanged at this point (adapter not yet called).
		return nil, err
	}

	applyErr := a.Apply(ctx, plan, w)
	if applyErr != nil {
		slog.Warn("clientinject apply failed; restoring backups",
			"client_id", plan.Install.ClientID,
			"err", applyErr)
		if rerr := RestoreBackups(w, files); rerr != nil {
			slog.Error("clientinject restore after apply failure also failed", "err", rerr)
			manifest.Status = ManifestStatusFailed
			manifest.MutationsApplied = nil
			manifest.Error = fmt.Sprintf("apply: %v; restore: %v", applyErr, rerr)
			_ = WriteManifest(w, manifest)
			return nil, fmt.Errorf("clientinject: apply failed (%w) and restore failed (%v)", applyErr, rerr)
		}
		manifest.Status = ManifestStatusFailed
		manifest.MutationsApplied = nil
		manifest.Error = applyErr.Error()
		_ = WriteManifest(w, manifest)
		return nil, fmt.Errorf("clientinject: apply failed (restored from bak): %w", applyErr)
	}

	// Record applied mutation IDs (cleared again if healthcheck reverts).
	applied := make([]string, 0, len(plan.Mutations))
	for _, m := range plan.Mutations {
		if m.ID != "" {
			applied = append(applied, m.ID)
		}
	}
	manifest.MutationsApplied = applied

	if err := a.HealthCheck(plan.Install, plan.Endpoints); err != nil {
		slog.Warn("clientinject healthcheck failed; auto-reverting",
			"client_id", plan.Install.ClientID,
			"err", err)
		if rerr := RestoreBackups(w, files); rerr != nil {
			manifest.Status = ManifestStatusFailed
			manifest.Error = fmt.Sprintf("healthcheck: %v; restore: %v", err, rerr)
			_ = WriteManifest(w, manifest)
			return nil, fmt.Errorf("clientinject: healthcheck failed (%w) and restore failed (%v)", err, rerr)
		}
		// Disk is stock again — do not claim mutations applied.
		manifest.Status = ManifestStatusFailed
		manifest.MutationsApplied = nil
		manifest.Error = "healthcheck: " + err.Error()
		_ = WriteManifest(w, manifest)
		return nil, fmt.Errorf("clientinject: healthcheck failed (restored from bak): %w", err)
	}

	manifest.Status = ManifestStatusApplied
	manifest.Error = ""
	if err := WriteManifest(w, manifest); err != nil {
		// PE/config remain patched; bak still holds stock. Manifest may stay
		// in_progress — next Apply is refused until Revert (uses bak).
		slog.Error("clientinject finalize manifest failed; disk patched, bak stock intact — Revert to recover",
			"err", err, "install_root", plan.Install.RootDir)
		return nil, fmt.Errorf("clientinject: finalize manifest: %w (disk may be patched; Revert restores from .openfsd-bak)", err)
	}

	return &ApplyResult{
		ManifestPath: ManifestPath(plan.Install.RootDir),
		BackupRoot:   plan.Install.RootDir,
		Applied:      applied,
	}, nil
}

// Revert restores bak files from the install root manifest.
// After restore, if the primary PE path is known and a profile SHA-1 is on the
// manifest, a soft mismatch is logged (does not fail Revert).
func (e *Engine) Revert(ctx context.Context, installRoot string) error {
	_ = ctx
	if installRoot == "" {
		return fmt.Errorf("clientinject: empty install root")
	}
	w := e.writer()
	m, err := ReadManifest(w, installRoot)
	if err != nil {
		return err
	}
	if len(m.Files) == 0 {
		return fmt.Errorf("clientinject: manifest has no backup files")
	}
	if err := RestoreBackups(w, m.Files); err != nil {
		return err
	}

	// Soft stock-SHA check when profile is available.
	if e.Profiles != nil && m.ProfileID != "" {
		if p, ok := e.Profiles.Get(m.ProfileID); ok {
			want := strings.ToLower(strings.TrimSpace(p.PrimaryBinary.SHA1))
			// Find primary PE among restored files (first matching relative name) or Absolute.
			pePath := ""
			if p.PrimaryBinary.RelativePath != "" {
				cand := filepath.Join(installRoot, p.PrimaryBinary.RelativePath)
				for _, f := range m.Files {
					if filepath.Clean(f.Original) == filepath.Clean(cand) {
						pePath = f.Original
						break
					}
				}
				if pePath == "" {
					// Fallback: any original ending with relative path.
					for _, f := range m.Files {
						if strings.HasSuffix(filepath.Clean(f.Original), filepath.Clean(p.PrimaryBinary.RelativePath)) {
							pePath = f.Original
							break
						}
					}
				}
			}
			if pePath != "" && want != "" {
				if sum, err := fileSHA1(w, pePath); err == nil {
					if !strings.EqualFold(sum, want) {
						slog.Warn("clientinject revert: primary PE sha1 != profile stock after restore",
							"path", pePath, "got", sum, "want", want, "profile_id", m.ProfileID)
					}
				}
			}
		}
	}

	m.Status = ManifestStatusReverted
	m.Error = ""
	m.MutationsApplied = nil
	if err := WriteManifest(w, m); err != nil {
		return err
	}
	// Keep bak files for forensics; optional cleanup is caller's choice.
	return nil
}

// ErrNoManifest is returned when Revert finds no manifest (optional helper).
var ErrNoManifest = errors.New("clientinject: no inject manifest")

// HasManifest reports whether an inject manifest exists under installRoot.
func HasManifest(w FileWriter, installRoot string) bool {
	if w == nil {
		w = OSFileWriter{}
	}
	_, err := w.Stat(ManifestPath(installRoot))
	return err == nil
}

// AbsPrimaryPE joins RootDir and relative PrimaryPE when needed.
func AbsPrimaryPE(install Install) string {
	if install.PrimaryPE == "" {
		return ""
	}
	if filepath.IsAbs(install.PrimaryPE) {
		return install.PrimaryPE
	}
	return filepath.Join(install.RootDir, install.PrimaryPE)
}
