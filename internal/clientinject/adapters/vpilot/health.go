package vpilot

import (
	"bytes"
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
// JWT slots remain stock after a JWT mutation was expected (in-place path).
// Free-slot remap leaves the stock JWT body intact and is checked via the
// free slot + ldstr token instead.
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

	usedSlots := make(map[string]bool)

	jwtURL, _, _ := resolveJWTURL(ep, profile)
	if js, ok := profile.Strings["fsd_jwt"]; ok && jwtURL != "" {
		if cilus.FitsBudget(jwtURL, js.PayloadBudgetBytes) {
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
		} else if freeSlotRemapReady {
			if err := healthCheckFreeSlot(data, profile.USFreeSlots, usedSlots, jwtURL, js, "JWT"); err != nil {
				return err
			}
		}
	}

	// AFV: assert in-place or free-slot remap when voice is enabled and URL set.
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
		} else if freeSlotRemapReady {
			// Only require free-slot health when a slot was available (same as Plan).
			need := cilus.BodyBudgetBytes(ep.AFVBaseURL)
			if slot, _ := pickFreeSlot(profile.USFreeSlots, need, usedSlots); slot != nil && len(as.LdstrFileOffsets) > 0 {
				if err := healthCheckFreeSlot(data, profile.USFreeSlots, usedSlots, ep.AFVBaseURL, as, "AFV"); err != nil {
					return err
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

// healthCheckFreeSlot verifies a free-slot body holds want and ldstr tokens
// point at the slot heap offset. Marks the chosen slot in used.
func healthCheckFreeSlot(
	data []byte,
	slots []clientinject.USFreeSlot,
	used map[string]bool,
	want string,
	spec clientinject.StringSpec,
	label string,
) error {
	need := cilus.BodyBudgetBytes(want)
	slot, slotID := pickFreeSlot(slots, need, used)
	if slot == nil {
		return fmt.Errorf("vpilot: healthcheck: %s URL over stock budget and no free slot fits", label)
	}
	if len(spec.LdstrFileOffsets) == 0 {
		return fmt.Errorf("vpilot: healthcheck: %s missing ldstr_file_offsets for free-slot check", label)
	}
	used[slotID] = true

	entryOff, err := usEntryFileOff(slot.BodyOffset.Int64(), slot.BudgetBytes)
	if err != nil {
		return fmt.Errorf("vpilot: healthcheck %s free slot: %w", label, err)
	}
	got, err := decodeUSAtEntry(data, entryOff)
	if err != nil {
		return fmt.Errorf("vpilot: healthcheck %s free-slot entry@%#x: %w", label, entryOff, err)
	}
	if got != want {
		return fmt.Errorf("vpilot: healthcheck: %s free slot %s = %q, want %q", label, slotID, got, want)
	}

	wantTok := makeLdstrToken(slot.HeapOffset.Int64())
	for _, lo := range spec.LdstrFileOffsets {
		off := lo.Int64()
		if off <= 0 {
			continue
		}
		if int(off)+len(wantTok) > len(data) {
			return fmt.Errorf("vpilot: healthcheck: %s ldstr @%#x OOB", label, off)
		}
		gotTok := data[off : int(off)+len(wantTok)]
		if !bytes.Equal(gotTok, wantTok) {
			return fmt.Errorf("vpilot: healthcheck: %s ldstr @%#x = %x, want %x (heap 0x%X)",
				label, off, gotTok, wantTok, slot.HeapOffset.Int64())
		}
	}
	return nil
}

// usEntryFileOff returns the file offset of the #US entry start (compressed
// length prefix) for a stock body at bodyOff with stock body length budget.
func usEntryFileOff(bodyOff int64, budget int) (int64, error) {
	stockPrefix, err := cilus.LengthPrefixBytes(budget)
	if err != nil {
		return 0, err
	}
	entry := bodyOff - int64(len(stockPrefix))
	if entry < 0 {
		return 0, fmt.Errorf("negative entry for body %#x", bodyOff)
	}
	return entry, nil
}

// decodeUSAtEntry decodes a #US entry starting at entryOff (length prefix).
func decodeUSAtEntry(data []byte, entryOff int64) (string, error) {
	if entryOff < 0 || int(entryOff) >= len(data) {
		return "", fmt.Errorf("entry offset %#x out of range", entryOff)
	}
	return cilus.DecodeUserString(data[entryOff:])
}

// decodeUSAtBody reads a #US body at bodyOff using the length prefix before it.
// Also tries decoding from the stock entry start when the new body used a
// narrower compressed-length prefix (free-slot short URL case).
func decodeUSAtBody(data []byte, bodyOff int64, budgetHint int) (string, error) {
	if bodyOff < 0 || int(bodyOff) >= len(data) {
		return "", fmt.Errorf("body offset %#x out of range", bodyOff)
	}
	// Free-slot / variable-prefix path: decode from stock entry start when budget known.
	if budgetHint > 0 {
		if entryOff, err := usEntryFileOff(bodyOff, budgetHint); err == nil {
			if s, err := decodeUSAtEntry(data, entryOff); err == nil {
				return s, nil
			}
		}
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
