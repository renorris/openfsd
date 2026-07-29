package vpilot

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/cilus"
	"github.com/renorris/openfsd/internal/clientinject/pepatch"
	"github.com/renorris/openfsd/internal/clientinject/vpilotconfig"
)

// HealthCheck validates PE #US slots and written configs after Apply.
//
// Pre-R3 residual policy (KD-20): extra stock JWT UTF-16 hits outside
// profile-listed body offsets are **warn-only**. Fail only if profile-listed
// JWT slots remain stock after a JWT mutation was expected.
func (a *Adapter) HealthCheck(install clientinject.Install, ep clientinject.Endpoints) error {
	ep = ep.Normalize()
	w := a.writer()
	pe := clientinject.AbsPrimaryPE(install)
	if pe == "" {
		return fmt.Errorf("vpilot: healthcheck: empty PrimaryPE")
	}
	data, err := w.ReadFile(pe)
	if err != nil {
		return fmt.Errorf("vpilot: healthcheck read PE: %w", err)
	}

	// Resolve profile for body offsets.
	profile, err := a.loadProfileForInstall(install)
	if err != nil {
		return fmt.Errorf("vpilot: healthcheck profile: %w", err)
	}

	jwtURL, _, _ := resolveJWTURL(ep, profile)
	if js, ok := profile.Strings["fsd_jwt"]; ok && jwtURL != "" {
		for _, fo := range js.BodyFileOffsets {
			off := fo.Int64()
			got, err := decodeUSAtBody(data, off, js.PayloadBudgetBytes)
			if err != nil {
				return fmt.Errorf("vpilot: healthcheck JWT body@%#x: %w", off, err)
			}
			if got == js.Stock || got == stockJWT {
				return fmt.Errorf("vpilot: healthcheck: profile-listed JWT slot @%#x still stock %q", off, got)
			}
			if got != jwtURL {
				return fmt.Errorf("vpilot: healthcheck: JWT slot @%#x = %q, want %q", off, got, jwtURL)
			}
		}
	}

	// AFV: only assert when we would have in-place patched (fits + non-empty + not force disable).
	if as, ok := profile.Strings["afv_base"]; ok && ep.AFVBaseURL != "" && !ep.ForceDisableAFV {
		if cilus.FitsBudget(ep.AFVBaseURL, as.PayloadBudgetBytes) {
			for _, fo := range as.BodyFileOffsets {
				off := fo.Int64()
				got, err := decodeUSAtBody(data, off, as.PayloadBudgetBytes)
				if err != nil {
					return fmt.Errorf("vpilot: healthcheck AFV body@%#x: %w", off, err)
				}
				if got == as.Stock || got == stockAFV {
					return fmt.Errorf("vpilot: healthcheck: AFV slot @%#x still stock", off)
				}
				if got != ep.AFVBaseURL {
					return fmt.Errorf("vpilot: healthcheck: AFV slot @%#x = %q, want %q", off, got, ep.AFVBaseURL)
				}
			}
		}
	}

	// Residual full-PE stock JWT scan — warn pre-R3, do not fail.
	hits := pepatch.ScanUTF16String(data, stockJWT)
	if len(hits) > 0 {
		// Filter out any that are still at profile body offsets (already failed above if stock).
		var extra []int64
		listed := map[int64]struct{}{}
		if js, ok := profile.Strings["fsd_jwt"]; ok {
			for _, fo := range js.BodyFileOffsets {
				listed[fo.Int64()] = struct{}{}
			}
		}
		for _, h := range hits {
			if _, ok := listed[h]; !ok {
				extra = append(extra, h)
			}
		}
		if len(extra) > 0 {
			slog.Warn("vpilot healthcheck: residual stock JWT UTF-16 outside profile-listed slots (pre-R3 warn only)",
				"count", len(extra), "offsets", fmt.Sprintf("%x", extra))
		}
	}

	// Configs: decrypt and assert status URL.
	statusURL := ep.StatusURL()
	cfgPaths := install.ConfigPaths
	if len(cfgPaths) == 0 {
		cfgPaths = []string{filepath.Join(install.RootDir, configName)}
	}
	checked := 0
	for _, p := range cfgPaths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(install.RootDir, p)
		}
		raw, err := w.ReadFile(p)
		if err != nil {
			continue
		}
		cfg, err := vpilotconfig.Parse(raw)
		if err != nil {
			return fmt.Errorf("vpilot: healthcheck parse config %s: %w", p, err)
		}
		if statusURL != "" && cfg.NetworkStatusURL != statusURL {
			return fmt.Errorf("vpilot: healthcheck config %s status URL %q != %q", p, cfg.NetworkStatusURL, statusURL)
		}
		checked++
	}
	if checked == 0 && statusURL != "" {
		return fmt.Errorf("vpilot: healthcheck: no readable config files under install")
	}
	return nil
}

// decodeUSAtBody reads a #US body at bodyOff using the length prefix before it.
func decodeUSAtBody(data []byte, bodyOff int64, budgetHint int) (string, error) {
	if bodyOff < 0 || int(bodyOff) >= len(data) {
		return "", fmt.Errorf("body offset %#x out of range", bodyOff)
	}
	for _, plen := range []int{1, 2, 4} {
		if bodyOff < int64(plen) {
			continue
		}
		start := int(bodyOff) - plen
		s, err := cilus.DecodeUserString(data[start:])
		if err != nil {
			continue
		}
		body := cilus.EncodeBody(s)
		full, err := cilus.EncodeUserString(s)
		if err != nil {
			continue
		}
		if len(full)-len(body) != plen {
			continue
		}
		return s, nil
	}
	// Fallback: try DecodeBody on budgetHint bytes (works when length prefix lost but body exact).
	if budgetHint > 0 && int(bodyOff)+budgetHint <= len(data) {
		// Try shrinking from budgetHint to find valid body.
		for n := budgetHint; n >= 1; n -= 2 {
			// Body length is odd (UTF-16 + terminal).
			if n%2 == 0 {
				continue
			}
			s, err := cilus.DecodeBody(data[bodyOff : int(bodyOff)+n])
			if err == nil {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("could not decode #US at body %#x", bodyOff)
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
	// Fall back to first vpilot profile.
	list := store.ForClient(clientID)
	if len(list) == 0 {
		return nil, fmt.Errorf("no vpilot profiles loaded")
	}
	// Prefer exact profile id match already failed; use first.
	for _, p := range list {
		if strings.HasPrefix(p.ProfileID, "vpilot") {
			return p, nil
		}
	}
	return list[0], nil
}
