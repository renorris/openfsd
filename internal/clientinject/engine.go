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

// Plan runs adapter.Plan after Verify. Does not write disk.
func (e *Engine) Plan(ctx context.Context, install Install, ep Endpoints) (*Plan, error) {
	_ = ctx
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
func (e *Engine) Apply(ctx context.Context, plan *Plan) (*ApplyResult, error) {
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

	// Preflight: exclusive open on primary PE.
	preflightOK := true
	if plan.Install.PrimaryPE != "" {
		if err := PreflightPrimaryPE(plan.Install.PrimaryPE); err != nil {
			return nil, err
		}
	}

	targets := CollectPlanTargets(plan)
	files, err := CreateBackups(w, targets)
	if err != nil {
		return nil, err
	}
	manifest := NewManifest(plan, files, preflightOK)
	if err := WriteManifest(w, manifest); err != nil {
		// Best-effort restore if we already wrote bak.
		_ = RestoreBackups(w, files)
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
			manifest.Error = fmt.Sprintf("apply: %v; restore: %v", applyErr, rerr)
			_ = WriteManifest(w, manifest)
			return nil, fmt.Errorf("clientinject: apply failed (%w) and restore failed (%v)", applyErr, rerr)
		}
		manifest.Status = ManifestStatusFailed
		manifest.Error = applyErr.Error()
		_ = WriteManifest(w, manifest)
		return nil, fmt.Errorf("clientinject: apply failed (restored from bak): %w", applyErr)
	}

	// Record applied mutation IDs.
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
		manifest.Status = ManifestStatusFailed
		manifest.Error = "healthcheck: " + err.Error()
		_ = WriteManifest(w, manifest)
		return nil, fmt.Errorf("clientinject: healthcheck failed (restored from bak): %w", err)
	}

	manifest.Status = ManifestStatusApplied
	manifest.Error = ""
	if err := WriteManifest(w, manifest); err != nil {
		return nil, fmt.Errorf("clientinject: finalize manifest: %w", err)
	}

	return &ApplyResult{
		ManifestPath: ManifestPath(plan.Install.RootDir),
		BackupRoot:   plan.Install.RootDir,
		Applied:      applied,
	}, nil
}

// Revert restores bak files from the install root manifest.
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
	m.Status = ManifestStatusReverted
	m.Error = ""
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
