package clientinject

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// ManifestFileName is written at the install root.
const ManifestFileName = ".openfsd-inject-manifest.json"

// BackupSuffix is appended to each backed-up target path.
const BackupSuffix = ".openfsd-bak"

// Manifest status values.
const (
	ManifestStatusInProgress = "in_progress"
	ManifestStatusApplied    = "applied"
	ManifestStatusFailed     = "failed"
	ManifestStatusReverted   = "reverted"
)

// ErrPriorInjectActive is returned when Apply finds an unfinished or applied
// inject that must be Reverted first (or when bak would be clobbered).
// For status=applied, re-Apply is allowed if stock bak is preserved; this
// error is used for status=in_progress (incomplete prior Apply).
var ErrPriorInjectActive = errors.New("clientinject: prior inject in progress; Revert first, then Apply again")

// Manifest records a transactional Apply for Revert and recovery.
type Manifest struct {
	ClientID            string         `json:"client_id"`
	ProfileID           string         `json:"profile_id"`
	PESHA1              string         `json:"pe_sha1"`
	ConfigPaths         []string       `json:"config_paths"`
	Files               []ManifestFile `json:"files"`
	MutationsApplied    []string       `json:"mutations_applied"`
	Endpoints           Endpoints      `json:"endpoints"`
	PreflightOK         bool           `json:"preflight_ok"`
	ConstraintsSnapshot []Constraint   `json:"constraints_snapshot,omitempty"`
	Status              string         `json:"status"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	Error               string         `json:"error,omitempty"`
	// InstallRoot is absolute path of the install (not always needed to restore).
	InstallRoot string `json:"install_root"`
}

// ManifestFile pairs original path with its .openfsd-bak sibling.
type ManifestFile struct {
	Original string `json:"original"`
	Backup   string `json:"backup"`
}

// BackupPath returns the sibling bak path for original.
func BackupPath(original string) string {
	return original + BackupSuffix
}

// ManifestPath returns the absolute manifest path under installRoot.
func ManifestPath(installRoot string) string {
	return filepath.Join(installRoot, ManifestFileName)
}

// CollectPlanTargets returns unique absolute paths that Apply will mutate.
// Includes primary PE, config paths, and mutation TargetRel under RootDir.
func CollectPlanTargets(plan *Plan) []string {
	if plan == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		// Prefer absolute; if relative, join RootDir.
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
	add(plan.Install.PrimaryPE)
	for _, c := range plan.Install.ConfigPaths {
		add(c)
	}
	for _, m := range plan.Mutations {
		if m.TargetRel != "" {
			add(m.TargetRel)
		}
		// Config rewrite may list extra paths in detail.
		if d, ok := m.Detail.(ConfigRewriteDetail); ok {
			for _, p := range d.Paths {
				add(p)
			}
		}
		if d, ok := m.Detail.(*ConfigRewriteDetail); ok && d != nil {
			for _, p := range d.Paths {
				add(p)
			}
		}
	}
	return out
}

// CreateBackups ensures each existing target has a .openfsd-bak sibling.
// Existing bak files are never overwritten — they hold stock content from the
// first Apply and must remain restorable across re-Apply.
// Missing targets are skipped (they may be created by Apply).
func CreateBackups(w FileWriter, targets []string) ([]ManifestFile, error) {
	var files []ManifestFile
	for _, orig := range targets {
		if _, err := w.Stat(orig); err != nil {
			// Skip missing — Apply may create config.
			continue
		}
		bak := BackupPath(orig)
		if _, err := w.Stat(bak); err == nil {
			// Preserve existing stock bak.
			files = append(files, ManifestFile{Original: orig, Backup: bak})
			continue
		}
		if err := w.CopyFile(orig, bak); err != nil {
			return files, fmt.Errorf("clientinject: backup %s: %w", orig, err)
		}
		files = append(files, ManifestFile{Original: orig, Backup: bak})
	}
	return files, nil
}

// RestoreBackups copies each bak over its original.
func RestoreBackups(w FileWriter, files []ManifestFile) error {
	var first error
	for _, f := range files {
		if _, err := w.Stat(f.Backup); err != nil {
			if first == nil {
				first = fmt.Errorf("clientinject: missing bak %s: %w", f.Backup, err)
			}
			continue
		}
		if err := w.CopyFile(f.Backup, f.Original); err != nil {
			if first == nil {
				first = fmt.Errorf("clientinject: restore %s: %w", f.Original, err)
			}
		}
	}
	return first
}

// RemoveBackups deletes bak siblings (best-effort after successful revert).
func RemoveBackups(w FileWriter, files []ManifestFile) error {
	var first error
	for _, f := range files {
		if err := w.Remove(f.Backup); err != nil {
			if first == nil {
				first = err
			}
		}
	}
	return first
}

// WriteManifest marshals m to install root.
func WriteManifest(w FileWriter, m *Manifest) error {
	if m == nil {
		return fmt.Errorf("clientinject: nil manifest")
	}
	m.UpdatedAt = time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = m.UpdatedAt
	}
	path := ManifestPath(m.InstallRoot)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("clientinject: marshal manifest: %w", err)
	}
	if err := w.WriteFile(path, data); err != nil {
		return fmt.Errorf("clientinject: write manifest: %w", err)
	}
	return nil
}

// ReadManifest loads the manifest from installRoot.
func ReadManifest(w FileWriter, installRoot string) (*Manifest, error) {
	path := ManifestPath(installRoot)
	data, err := w.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("clientinject: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("clientinject: parse manifest: %w", err)
	}
	if m.InstallRoot == "" {
		m.InstallRoot = installRoot
	}
	return &m, nil
}

// NewManifest builds an in_progress manifest for a plan.
func NewManifest(plan *Plan, files []ManifestFile, preflightOK bool) *Manifest {
	now := time.Now().UTC()
	return &Manifest{
		ClientID:            plan.Install.ClientID,
		ProfileID:           plan.Install.ProfileID,
		PESHA1:              plan.Install.HashSHA1,
		ConfigPaths:         append([]string(nil), plan.Install.ConfigPaths...),
		Files:               files,
		MutationsApplied:    nil,
		Endpoints:           plan.Endpoints,
		PreflightOK:         preflightOK,
		ConstraintsSnapshot: append([]Constraint(nil), plan.Constraints...),
		Status:              ManifestStatusInProgress,
		CreatedAt:           now,
		UpdatedAt:           now,
		InstallRoot:         plan.Install.RootDir,
	}
}

// CheckPriorInject refuses Apply when a prior inject is incomplete (in_progress).
// status=applied / failed / reverted is OK: CreateBackups preserves stock bak.
// Recovery: Revert uses bak even if status is still in_progress.
func CheckPriorInject(w FileWriter, installRoot string) error {
	path := ManifestPath(installRoot)
	if _, err := w.Stat(path); err != nil {
		return nil // no manifest
	}
	m, err := ReadManifest(w, installRoot)
	if err != nil {
		return fmt.Errorf("clientinject: read prior manifest: %w", err)
	}
	if m.Status == ManifestStatusInProgress {
		return fmt.Errorf("%w (status=%s); Revert restores .openfsd-bak even if status is in_progress",
			ErrPriorInjectActive, m.Status)
	}
	return nil
}
