package vpilot

import (
	"context"
	"fmt"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/cilus"
	"github.com/renorris/openfsd/internal/clientinject/pepatch"
	"github.com/renorris/openfsd/internal/clientinject/vpilotconfig"
)

// Apply writes plan mutations via FileWriter. Launch flags are plan-only (no-op).
func (a *Adapter) Apply(ctx context.Context, plan *clientinject.Plan, w clientinject.FileWriter) error {
	_ = ctx
	if plan == nil {
		return fmt.Errorf("vpilot: nil plan")
	}
	if w == nil {
		return fmt.Errorf("vpilot: nil FileWriter")
	}
	pePath := clientinject.AbsPrimaryPE(plan.Install)

	// Open PE once if any PE mutation needs it.
	needPE := false
	for _, m := range plan.Mutations {
		switch m.Kind {
		case clientinject.MutUSHeapString, clientinject.MutLdstrRemap,
			clientinject.MutRawOverwrite, clientinject.MutAFVDisablePE,
			clientinject.MutPaddedString:
			needPE = true
		}
	}

	var peFile clientinject.ReadWriteSeekCloser
	if needPE {
		if pePath == "" {
			return fmt.Errorf("vpilot: PE mutation planned but PrimaryPE empty")
		}
		f, err := w.OpenReadWrite(pePath)
		if err != nil {
			return fmt.Errorf("vpilot: open PE: %w", err)
		}
		peFile = f
		defer peFile.Close()
	}

	// Budget map from profile string_ref — applied via USStringDetail.
	// For length-prefix rewrite we need payload budget; look up from install profile id is not on plan.
	// USStringDetail has NewString; budget must be ≥ body. We pad to stock body size derived from
	// reading existing length prefix, or use BodyBudgetBytes of stock from detail length.
	// Practical approach: pad to max(EncodeBody(new), existing slot) — profile budgets:
	// we encode padded to BodyBudgetBytes(new) only when equal slot; for in-place, pad to
	// the profile budget stored by expanding from existing body length at offset.
	//
	// Use: budget = max body we can write = derive from first body offset's stock slot.
	// Plan encodes NewString that FitsBudget(profile.budget). Pad to profile budget via
	// a side channel: read current compressed length at bodyOff-1 (1-byte typical).
	// Safer: pad to len from existing #US entry at body offset (stock budget).

	for _, m := range plan.Mutations {
		switch m.Kind {
		case clientinject.MutConfigRewrite:
			if err := applyConfig(w, m); err != nil {
				return err
			}
		case clientinject.MutUSHeapString:
			d, ok := usDetail(m.Detail)
			if !ok {
				return fmt.Errorf("vpilot: mutation %s: bad USStringDetail", m.ID)
			}
			if peFile == nil {
				return fmt.Errorf("vpilot: PE not open for %s", m.ID)
			}
			budget, err := slotBudgetAt(w, pePath, d)
			if err != nil {
				return fmt.Errorf("vpilot: %s: %w", m.ID, err)
			}
			if err := patchUSString(peFile, d, budget); err != nil {
				return fmt.Errorf("vpilot: %s: %w", m.ID, err)
			}
		case clientinject.MutLdstrRemap:
			d, ok := remapDetail(m.Detail)
			if !ok {
				return fmt.Errorf("vpilot: mutation %s: bad LdstrRemapDetail", m.ID)
			}
			if peFile == nil {
				return fmt.Errorf("vpilot: PE not open for %s", m.ID)
			}
			// Free-slot body write: need NewString from a paired mutation — store in
			// NewToken as UTF-8 of the URL for now, or require SlotBudget + write from
			// endpoints. Plan currently leaves NewToken empty for remap-only.
			// For R1: write using SlotBudget; NewString must be on a related field.
			// Minimal: if NewToken empty, skip body write (incomplete R1).
			if len(d.NewToken) == 0 {
				return fmt.Errorf("vpilot: %s: ldstr remap requires NewToken (R1 incomplete / no free-slot URL encoding)", m.ID)
			}
			// Write free slot body + length prefix.
			s := string(d.NewToken)
			if err := patchUSAtBody(peFile, d.SlotBodyOff, s, d.SlotBudget); err != nil {
				return fmt.Errorf("vpilot: %s free slot: %w", m.ID, err)
			}
			// ldstr token rewrite: NewToken as raw CIL token bytes if provided as 4-byte token.
			// When NewToken is the URL string, skip ldstr byte patch (needs token bytes).
			// Real R1 will set NewToken to the 4-byte metadata token.
			if len(d.NewToken) == 4 && d.LdstrFileOff > 0 {
				if err := pepatch.OverwriteAt(peFile, d.LdstrFileOff, d.NewToken); err != nil {
					return fmt.Errorf("vpilot: %s ldstr: %w", m.ID, err)
				}
			}
		case clientinject.MutRawOverwrite, clientinject.MutAFVDisablePE:
			d, ok := rawDetail(m.Detail)
			if !ok {
				return fmt.Errorf("vpilot: mutation %s: bad RawOverwriteDetail", m.ID)
			}
			if peFile == nil {
				return fmt.Errorf("vpilot: PE not open for %s", m.ID)
			}
			if d.FileOffset == 0 || len(d.NewBytes) == 0 {
				continue // R2 not ready
			}
			if err := pepatch.OverwriteAt(peFile, d.FileOffset, d.NewBytes); err != nil {
				return fmt.Errorf("vpilot: %s: %w", m.ID, err)
			}
		case clientinject.MutPaddedString:
			d, ok := m.Detail.(clientinject.PaddedStringDetail)
			if !ok {
				if p, ok2 := m.Detail.(*clientinject.PaddedStringDetail); ok2 && p != nil {
					d = *p
				} else {
					return fmt.Errorf("vpilot: mutation %s: bad PaddedStringDetail", m.ID)
				}
			}
			if peFile == nil {
				return fmt.Errorf("vpilot: PE not open for %s", m.ID)
			}
			if err := pepatch.PaddedStringOverwrite(peFile, d.FileOffset, d.NewString, d.SlotLen, d.Encoding); err != nil {
				return fmt.Errorf("vpilot: %s: %w", m.ID, err)
			}
		case clientinject.MutLaunchFlag:
			// Plan-only: nothing to write.
			continue
		default:
			return fmt.Errorf("vpilot: unsupported mutation kind %q (id=%s)", m.Kind, m.ID)
		}
	}

	return nil
}

func applyConfig(w clientinject.FileWriter, m clientinject.Mutation) error {
	d, ok := configDetail(m.Detail)
	if !ok {
		return fmt.Errorf("vpilot: config mutation: bad ConfigRewriteDetail")
	}
	paths := d.Paths
	if len(paths) == 0 && m.TargetRel != "" {
		paths = []string{m.TargetRel}
	}
	for _, p := range paths {
		if err := writeOneConfig(w, p, d); err != nil {
			return err
		}
	}
	return nil
}

func writeOneConfig(w clientinject.FileWriter, path string, d clientinject.ConfigRewriteDetail) error {
	if path == "" {
		return fmt.Errorf("vpilot: empty config path")
	}
	var cfg *vpilotconfig.Config
	data, err := w.ReadFile(path)
	if err != nil {
		// Create minimal config.
		cfg = &vpilotconfig.Config{}
	} else {
		cfg, err = vpilotconfig.Parse(data)
		if err != nil {
			// If unreadable, start fresh rather than brick Apply in lab.
			cfg = &vpilotconfig.Config{}
		}
	}
	vpilotconfig.ApplyEndpoints(cfg, d.NetworkStatusURL, d.CachedServers, d.ClearCredentials)
	out, err := vpilotconfig.Format(cfg)
	if err != nil {
		return fmt.Errorf("vpilot: format config %s: %w", path, err)
	}
	if err := w.WriteFile(path, out); err != nil {
		return fmt.Errorf("vpilot: write config %s: %w", path, err)
	}
	return nil
}

// slotBudgetAt determines the stock #US body budget for a string slot.
// Prefer reading the compressed length prefix immediately before the first body offset.
func slotBudgetAt(w clientinject.FileWriter, pePath string, d clientinject.USStringDetail) (int, error) {
	if len(d.BodyOffs) == 0 {
		return 0, fmt.Errorf("no body offsets")
	}
	// Default budgets from known stock (JWT 71, AFV 51) via body size of NewString upper bound.
	// Read PE bytes around body to get length prefix.
	data, err := w.ReadFile(pePath)
	if err != nil {
		return 0, err
	}
	bodyOff := d.BodyOffs[0]
	// Try 1-byte then 2-byte compressed length immediately before body.
	for _, plen := range []int{1, 2, 4} {
		if bodyOff < int64(plen) {
			continue
		}
		start := bodyOff - int64(plen)
		if start < 0 || int(start)+plen > len(data) {
			continue
		}
		s, err := cilus.DecodeUserString(data[start:])
		if err != nil {
			continue
		}
		// Validate body starts at bodyOff.
		body := cilus.EncodeBody(s)
		full, err := cilus.EncodeUserString(s)
		if err != nil {
			continue
		}
		if len(full)-len(body) != plen {
			continue
		}
		// Stock budget is the body length currently declared (or larger padded region).
		// For in-place, budget is stock body size (payload_budget_bytes).
		return len(body), nil
	}
	// Fallback: use BodyBudgetBytes of NewString (exact pad, no residual zero-fill to stock).
	// Prefer known budgets by string ref.
	switch d.StringRef {
	case "fsd_jwt":
		return 71, nil
	case "afv_base":
		return 51, nil
	case "fsd_auto":
		return 45, nil
	default:
		return cilus.BodyBudgetBytes(d.NewString), nil
	}
}

func patchUSString(f clientinject.ReadWriteSeekCloser, d clientinject.USStringDetail, budget int) error {
	if budget <= 0 {
		return fmt.Errorf("invalid budget %d", budget)
	}
	if !cilus.FitsBudget(d.NewString, budget) {
		return fmt.Errorf("string exceeds budget %d", budget)
	}
	for _, bodyOff := range d.BodyOffs {
		if err := patchUSAtBody(f, bodyOff, d.NewString, budget); err != nil {
			return err
		}
	}
	return nil
}

// patchUSAtBody rewrites compressed length prefix + padded body at bodyOff.
func patchUSAtBody(f clientinject.ReadWriteSeekCloser, bodyOff int64, s string, budget int) error {
	body := cilus.EncodeBody(s)
	prefix, err := cilus.LengthPrefixBytes(len(body))
	if err != nil {
		return fmt.Errorf("length prefix: %w", err)
	}
	padded, err := cilus.EncodeBodyPadded(s, budget)
	if err != nil {
		return fmt.Errorf("encode body: %w", err)
	}
	// Length prefix sits immediately before the body. Stock and new both use 1-byte
	// prefixes for budgets ≤ 0x7F. If prefix width changes, refuse (would corrupt heap).
	stockPrefixLen := 1
	if budget > 0x7F && budget <= 0x3FFF {
		stockPrefixLen = 2
	} else if budget > 0x3FFF {
		stockPrefixLen = 4
	}
	if len(prefix) != stockPrefixLen {
		// Still allow when stock was same width as new; recompute stock width from budget
		// encoding of stock body size (= budget for full slots).
		stockPrefix, err := cilus.LengthPrefixBytes(budget)
		if err != nil {
			return err
		}
		if len(prefix) != len(stockPrefix) {
			return fmt.Errorf("length prefix width changed (%d → %d); refuse in-place rewrite", len(stockPrefix), len(prefix))
		}
		stockPrefixLen = len(stockPrefix)
	}
	prefixOff := bodyOff - int64(stockPrefixLen)
	if prefixOff < 0 {
		return fmt.Errorf("negative prefix offset for body %d", bodyOff)
	}
	if err := pepatch.OverwriteAt(f, prefixOff, prefix); err != nil {
		return fmt.Errorf("write length prefix at %d: %w", prefixOff, err)
	}
	if err := pepatch.OverwriteAt(f, bodyOff, padded); err != nil {
		return fmt.Errorf("write body at %d: %w", bodyOff, err)
	}
	return nil
}

func usDetail(d any) (clientinject.USStringDetail, bool) {
	switch v := d.(type) {
	case clientinject.USStringDetail:
		return v, true
	case *clientinject.USStringDetail:
		if v == nil {
			return clientinject.USStringDetail{}, false
		}
		return *v, true
	default:
		return clientinject.USStringDetail{}, false
	}
}

func remapDetail(d any) (clientinject.LdstrRemapDetail, bool) {
	switch v := d.(type) {
	case clientinject.LdstrRemapDetail:
		return v, true
	case *clientinject.LdstrRemapDetail:
		if v == nil {
			return clientinject.LdstrRemapDetail{}, false
		}
		return *v, true
	default:
		return clientinject.LdstrRemapDetail{}, false
	}
}

func rawDetail(d any) (clientinject.RawOverwriteDetail, bool) {
	switch v := d.(type) {
	case clientinject.RawOverwriteDetail:
		return v, true
	case *clientinject.RawOverwriteDetail:
		if v == nil {
			return clientinject.RawOverwriteDetail{}, false
		}
		return *v, true
	default:
		return clientinject.RawOverwriteDetail{}, false
	}
}

func configDetail(d any) (clientinject.ConfigRewriteDetail, bool) {
	switch v := d.(type) {
	case clientinject.ConfigRewriteDetail:
		return v, true
	case *clientinject.ConfigRewriteDetail:
		if v == nil {
			return clientinject.ConfigRewriteDetail{}, false
		}
		return *v, true
	default:
		return clientinject.ConfigRewriteDetail{}, false
	}
}
