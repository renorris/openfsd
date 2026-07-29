// Package vpilot implements the clientinject.Adapter for vPilot 3.12.1.
//
// Phase 0 honesty: short-host lab JWT/AFV in-place #US works; long hostnames
// block unless PreferShortJWTPath + budget fits (or free-slot remap after R1).
// AFV PE ret disable is out of scope until research gate R2.
package vpilot

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
)

const (
	clientID    = "vpilot"
	displayName = "vPilot"

	// Stock strings (also in profile YAML).
	stockJWT    = "https://auth.vatsim.net/api/fsd-jwt"
	stockAFV    = "https://voice1.vatsim.net"
	configName  = "vPilotConfig.xml"
	primaryName = "vPilot.exe"
)

// Adapter is the vPilot client strategy.
type Adapter struct {
	// Writer is used by HealthCheck / Discover when non-nil; default OSFileWriter.
	Writer clientinject.FileWriter
	// Profiles overrides the embedded profile store (tests / --profiles-dir).
	// When nil, HealthCheck loads embedded profiles.
	Profiles *clientinject.ProfileStore
}

// New returns a vPilot adapter.
func New() *Adapter {
	return &Adapter{}
}

// NewWithProfiles returns an adapter that resolves profiles from store.
func NewWithProfiles(store *clientinject.ProfileStore) *Adapter {
	return &Adapter{Profiles: store}
}

// ClientID implements clientinject.Adapter.
func (a *Adapter) ClientID() string { return clientID }

// DisplayName implements clientinject.Adapter.
func (a *Adapter) DisplayName() string { return displayName }

// SupportedProfiles implements clientinject.Adapter.
func (a *Adapter) SupportedProfiles() []string {
	return []string{"vpilot-3.12.1"}
}

func (a *Adapter) writer() clientinject.FileWriter {
	if a != nil && a.Writer != nil {
		return a.Writer
	}
	return clientinject.OSFileWriter{}
}

// Discover looks for vPilot installs.
// Windows: %LOCALAPPDATA%\vPilot\ and common Program Files paths.
// macOS/Linux: best-effort Wine prefixes; may return empty (use --install).
func (a *Adapter) Discover(ctx context.Context) ([]clientinject.InstallCandidate, error) {
	_ = ctx
	var cands []clientinject.InstallCandidate
	seen := make(map[string]struct{})

	add := func(root, hint string) {
		root = filepath.Clean(root)
		if root == "" || root == "." {
			return
		}
		if _, ok := seen[root]; ok {
			return
		}
		pe := filepath.Join(root, primaryName)
		w := a.writer()
		if _, err := w.Stat(pe); err != nil {
			return
		}
		seen[root] = struct{}{}
		cands = append(cands, clientinject.InstallCandidate{
			ClientID:    clientID,
			RootDir:     root,
			PrimaryPE:   pe,
			ConfigPaths: configCandidates(root),
			DisplayHint: hint,
		})
	}

	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			add(filepath.Join(local, "vPilot"), "default LocalAppData install")
		}
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(env); base != "" {
				add(filepath.Join(base, "vPilot"), env+" install")
			}
		}
	default:
		// Wine: common prefixes under $HOME.
		home, _ := os.UserHomeDir()
		if home != "" {
			wineRoots := []string{
				filepath.Join(home, ".wine", "drive_c", "users", os.Getenv("USER"), "AppData", "Local", "vPilot"),
				filepath.Join(home, ".wine", "drive_c", "Program Files", "vPilot"),
				filepath.Join(home, ".wine", "drive_c", "Program Files (x86)", "vPilot"),
			}
			for _, r := range wineRoots {
				add(r, "Wine install")
			}
		}
	}
	return cands, nil
}

// configCandidates returns ordered config paths that exist (R4).
func configCandidates(installRoot string) []string {
	var out []string
	seen := make(map[string]struct{})
	addIfExists := func(p string) {
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		if _, err := os.Stat(p); err != nil {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	addIfExists(filepath.Join(installRoot, configName))
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			alt := filepath.Join(local, "vPilot", configName)
			if filepath.Clean(filepath.Dir(alt)) != filepath.Clean(installRoot) {
				addIfExists(alt)
			}
		}
	}
	return out
}

// Verify checks primary PE SHA-1 (and size when profile specifies it).
func (a *Adapter) Verify(install clientinject.Install, profile *clientinject.Profile) error {
	if profile == nil {
		return fmt.Errorf("vpilot: nil profile")
	}
	if profile.ClientID != clientID {
		return fmt.Errorf("vpilot: profile client_id %q is not vpilot", profile.ClientID)
	}
	pe := clientinject.AbsPrimaryPE(install)
	if pe == "" {
		return fmt.Errorf("vpilot: empty primary PE path")
	}
	w := a.writer()
	data, err := w.ReadFile(pe)
	if err != nil {
		return fmt.Errorf("vpilot: read PE: %w", err)
	}
	sum := sha1.Sum(data)
	got := hex.EncodeToString(sum[:])
	want := strings.ToLower(strings.TrimSpace(profile.PrimaryBinary.SHA1))
	if want == "" {
		return fmt.Errorf("vpilot: profile has no primary_binary.sha1")
	}
	if !strings.EqualFold(got, want) {
		// Allow verify when live PE already patched (stock bak exists) and
		// install.HashSHA1 matches profile stock.
		bak := clientinject.BackupPath(pe)
		if _, bakErr := w.Stat(bak); bakErr == nil {
			if install.HashSHA1 != "" && strings.EqualFold(install.HashSHA1, want) {
				return nil
			}
			// Re-hash not possible from bak here without reading; if HashSHA1 empty, try bak.
			if bakData, rerr := w.ReadFile(bak); rerr == nil {
				bakSum := sha1.Sum(bakData)
				if strings.EqualFold(hex.EncodeToString(bakSum[:]), want) {
					return nil
				}
			}
		}
		return fmt.Errorf("vpilot: PE sha1 %s does not match profile %s (stock %s); refuse unknown hash",
			got, profile.ProfileID, want)
	}
	if wantSize := profile.PrimaryBinary.SizeBytes; wantSize > 0 && int64(len(data)) != wantSize {
		return fmt.Errorf("vpilot: PE size %d does not match profile %d", len(data), wantSize)
	}
	return nil
}

// EndpointConstraints returns static budget hints from the profile.
func (a *Adapter) EndpointConstraints(profile *clientinject.Profile) []clientinject.Constraint {
	if profile == nil {
		return nil
	}
	var out []clientinject.Constraint
	if s, ok := profile.Strings["fsd_jwt"]; ok && s.PayloadBudgetBytes > 0 {
		maxRunes := (s.PayloadBudgetBytes - 1) / 2
		out = append(out, clientinject.Constraint{
			Field:       "JWTURL",
			MaxRunes:    maxRunes,
			Strategy:    "in_place",
			Description: fmt.Sprintf("in-place #US budget %d bytes (~%d runes); default path max host 12 chars; --prefer-short-jwt (/j) max host 25 if server fixed", s.PayloadBudgetBytes, maxRunes),
		})
	}
	if s, ok := profile.Strings["afv_base"]; ok && s.PayloadBudgetBytes > 0 {
		maxRunes := (s.PayloadBudgetBytes - 1) / 2
		out = append(out, clientinject.Constraint{
			Field:       "AFVBaseURL",
			MaxRunes:    maxRunes,
			Strategy:    "in_place",
			Description: fmt.Sprintf("in-place #US budget %d bytes (~%d runes); over-budget falls back to -novoice", s.PayloadBudgetBytes, maxRunes),
		})
	}
	return out
}

// LaunchArgs returns CLI flags for launching the patched client.
func (a *Adapter) LaunchArgs(install clientinject.Install, ep clientinject.Endpoints) []string {
	_ = install
	ep = ep.Normalize()
	var args []string
	if addr := ep.FSDAddress(); addr != "" {
		args = append(args, "-serveraddressoverride", addr)
	}
	if ep.ForceDisableAFV || ep.AFVBaseURL == "" {
		args = append(args, "-novoice")
	} else if afvSpecBudget(ep) {
		// Over-budget AFV also needs -novoice; recompute via budget helper.
		// Use a light check: BodyBudgetBytes without full profile — AFV max 25 runes.
		if !fitsAFVURL(ep.AFVBaseURL) {
			args = append(args, "-novoice")
		}
	}
	return args
}

func fitsAFVURL(url string) bool {
	// Stock AFV budget is 51 body bytes → 25 runes. Use rune count as approximation;
	// exact body check is in Plan.
	if url == "" {
		return true
	}
	// ASCII URLs: body = 2*len + 1 ≤ 51 → len ≤ 25.
	return len(url) <= 25
}

func afvSpecBudget(ep clientinject.Endpoints) bool {
	return ep.AFVBaseURL != "" && !ep.ForceDisableAFV
}

// InstallFromDir builds an Install for an explicit --install directory.
func InstallFromDir(root string) clientinject.Install {
	root = filepath.Clean(root)
	pe := filepath.Join(root, primaryName)
	cfgs := configCandidates(root)
	if len(cfgs) == 0 {
		// Prefer install-dir path for plan detail even if missing (plan may block).
		cfgs = []string{filepath.Join(root, configName)}
	}
	return clientinject.Install{
		ClientID:    clientID,
		RootDir:     root,
		PrimaryPE:   pe,
		ConfigPaths: cfgs,
	}
}

// resolveConfigWritePaths returns paths that Apply will rewrite.
// Existing candidates only; if none exist under install, returns install config
// path for creation (tests / first-run after blocker is lifted).
func resolveConfigWritePaths(install clientinject.Install) []string {
	var existing []string
	for _, p := range install.ConfigPaths {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(install.RootDir, p)
		}
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) > 0 {
		return existing
	}
	// Fallback: install-dir config (may not exist yet).
	return []string{filepath.Join(install.RootDir, configName)}
}
