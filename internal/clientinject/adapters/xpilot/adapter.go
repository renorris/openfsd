// Package xpilot implements the clientinject.Adapter for xPilot 3.0.1.
//
// Strategy (prior art openfsd-client-patch-utility xpilot-3.0.1.yaml):
// padded_string slots for status.json (UTF-8) and fsd-jwt (UTF-16LE),
// raw_overwrite for LEA RIP fixups, dynamic length immediates, and a
// break of residual fsd.vatsim.net. AFV retarget is out of scope until
// research inventories voice base sites.
//
// Offsets are version-pinned by primary PE SHA-1; unknown hashes are refused.
package xpilot

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
	clientID    = "xpilot"
	displayName = "xPilot"
	primaryName = "xPilot.exe"
	profileID   = "xpilot-3.0.1"

	// Single-byte length immediates in the 3.0.1 prior-art path.
	maxLengthImm = 255
)

// Adapter is the xPilot client strategy.
type Adapter struct {
	// Writer is used by HealthCheck / Discover when non-nil; default OSFileWriter.
	Writer clientinject.FileWriter
	// Profiles overrides the embedded profile store (tests / --profiles-dir).
	Profiles *clientinject.ProfileStore
}

// New returns an xPilot adapter.
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
	return []string{profileID}
}

func (a *Adapter) writer() clientinject.FileWriter {
	if a != nil && a.Writer != nil {
		return a.Writer
	}
	return clientinject.OSFileWriter{}
}

// Discover looks for xPilot installs (Program Files + Wine prefixes).
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
		if _, err := a.writer().Stat(pe); err != nil {
			return
		}
		seen[root] = struct{}{}
		cands = append(cands, clientinject.InstallCandidate{
			ClientID:    clientID,
			RootDir:     root,
			PrimaryPE:   pe,
			ConfigPaths: nil,
			DisplayHint: hint,
		})
	}

	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if base := os.Getenv(env); base != "" {
				add(filepath.Join(base, "xPilot"), env+" install")
			}
		}
		// Some installs may live under LocalAppData (defensive).
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			add(filepath.Join(local, "xPilot"), "LocalAppData install")
		}
	default:
		home, _ := os.UserHomeDir()
		if home != "" {
			user := os.Getenv("USER")
			if user == "" {
				user = os.Getenv("USERNAME")
			}
			wineRoots := []string{
				filepath.Join(home, ".wine", "drive_c", "Program Files", "xPilot"),
				filepath.Join(home, ".wine", "drive_c", "Program Files (x86)", "xPilot"),
			}
			if user != "" {
				wineRoots = append(wineRoots,
					filepath.Join(home, ".wine", "drive_c", "users", user, "AppData", "Local", "xPilot"),
				)
			}
			for _, r := range wineRoots {
				add(r, "Wine install")
			}
		}
	}
	return cands, nil
}

// Verify checks primary PE SHA-1 against the profile (stock bak allowed post-Apply).
func (a *Adapter) Verify(install clientinject.Install, profile *clientinject.Profile) error {
	if profile == nil {
		return fmt.Errorf("xpilot: nil profile")
	}
	if profile.ClientID != clientID {
		return fmt.Errorf("xpilot: profile client_id %q is not xpilot", profile.ClientID)
	}
	pe := clientinject.AbsPrimaryPE(install)
	if pe == "" {
		return fmt.Errorf("xpilot: empty primary PE path")
	}
	w := a.writer()
	data, err := w.ReadFile(pe)
	if err != nil {
		return fmt.Errorf("xpilot: read PE: %w", err)
	}
	sum := sha1.Sum(data)
	got := hex.EncodeToString(sum[:])
	want := strings.ToLower(strings.TrimSpace(profile.PrimaryBinary.SHA1))
	if want == "" {
		return fmt.Errorf("xpilot: profile has no primary_binary.sha1")
	}
	if !strings.EqualFold(got, want) {
		bak := clientinject.BackupPath(pe)
		if _, bakErr := w.Stat(bak); bakErr == nil {
			if install.HashSHA1 != "" && strings.EqualFold(install.HashSHA1, want) {
				return nil
			}
			if bakData, rerr := w.ReadFile(bak); rerr == nil {
				bakSum := sha1.Sum(bakData)
				if strings.EqualFold(hex.EncodeToString(bakSum[:]), want) {
					return nil
				}
			}
		}
		return fmt.Errorf("xpilot: PE sha1 %s does not match profile %s (stock %s); refuse unknown hash",
			got, profile.ProfileID, want)
	}
	if wantSize := profile.PrimaryBinary.SizeBytes; wantSize > 0 && int64(len(data)) != wantSize {
		return fmt.Errorf("xpilot: PE size %d does not match profile %d", len(data), wantSize)
	}
	return nil
}

// EndpointConstraints returns slot budget hints from the profile.
func (a *Adapter) EndpointConstraints(profile *clientinject.Profile) []clientinject.Constraint {
	if profile == nil {
		return nil
	}
	var out []clientinject.Constraint
	if s, ok := profile.Strings["status_json"]; ok && s.PayloadBudgetBytes > 0 {
		// UTF-8: budget is byte slot; max URL runes ≈ budget-1 (NUL).
		maxRunes := s.PayloadBudgetBytes - 1
		if maxRunes > maxLengthImm {
			maxRunes = maxLengthImm
		}
		out = append(out, clientinject.Constraint{
			Field:       "StatusJSONURL",
			MaxRunes:    maxRunes,
			Strategy:    "padded_string",
			Description: fmt.Sprintf("UTF-8 padded slot %d bytes; length imm is 1 byte (max %d)", s.PayloadBudgetBytes, maxLengthImm),
		})
	}
	if s, ok := profile.Strings["fsd_jwt"]; ok && s.PayloadBudgetBytes > 0 {
		// UTF-16LE: encoded (runes+NUL)*2 must fit slot; length imm is char count ≤255.
		maxRunes := (s.PayloadBudgetBytes / 2) - 1
		if maxRunes > maxLengthImm {
			maxRunes = maxLengthImm
		}
		out = append(out, clientinject.Constraint{
			Field:       "JWTURL",
			MaxRunes:    maxRunes,
			Strategy:    "padded_string",
			Description: fmt.Sprintf("UTF-16LE padded slot %d bytes (~%d runes); length imm max %d", s.PayloadBudgetBytes, maxRunes, maxLengthImm),
		})
	}
	out = append(out, clientinject.Constraint{
		Field:       "AFVBaseURL",
		MaxRunes:    0,
		Strategy:    "n/a",
		Description: "xPilot 3.0.1 prior art does not retarget AFV; configure voice separately or await a future profile",
	})
	return out
}

// LaunchArgs returns CLI flags for launching the client (none known for 3.0.1).
func (a *Adapter) LaunchArgs(install clientinject.Install, ep clientinject.Endpoints) []string {
	_ = install
	_ = ep
	return nil
}

// InstallFromDir builds an Install for an explicit --install directory.
func InstallFromDir(root string) clientinject.Install {
	return New().InstallFromDir(root)
}

// InstallFromDir builds an Install using this adapter's FileWriter.
func (a *Adapter) InstallFromDir(root string) clientinject.Install {
	root = filepath.Clean(root)
	return clientinject.Install{
		ClientID:  clientID,
		RootDir:   root,
		PrimaryPE: filepath.Join(root, primaryName),
	}
}

func (a *Adapter) loadProfileForInstall(install clientinject.Install) (*clientinject.Profile, error) {
	var store *clientinject.ProfileStore
	if a != nil && a.Profiles != nil {
		store = a.Profiles
	} else {
		var err error
		store, err = clientinject.LoadEmbedded()
		if err != nil {
			return nil, err
		}
	}
	if install.ProfileID != "" {
		if p, ok := store.Get(install.ProfileID); ok {
			return p, nil
		}
	}
	if install.HashSHA1 != "" {
		if p, ok := store.LookupByHash(install.ClientID, install.HashSHA1); ok {
			return p, nil
		}
	}
	list := store.ForClient(clientID)
	if len(list) == 0 {
		return nil, fmt.Errorf("xpilot: no xpilot profiles loaded")
	}
	for _, p := range list {
		if strings.HasPrefix(p.ProfileID, "xpilot") {
			return p, nil
		}
	}
	return list[0], nil
}
