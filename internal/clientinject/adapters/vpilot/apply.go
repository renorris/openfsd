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
			budget := d.BudgetBytes
			if budget <= 0 {
				budget = fallbackBudget(d.StringRef, d.NewString)
			}
			if err := patchUSString(peFile, d, budget); err != nil {
				return fmt.Errorf("vpilot: %s: %w", m.ID, err)
			}
		case clientinject.MutLdstrRemap:
			// R1 incomplete: Plan must not emit this until freeSlotRemapReady.
			return fmt.Errorf("vpilot: mutation %s: ldstr remap not implemented (R1); plan should have blocked", m.ID)
		case clientinject.MutRawOverwrite, clientinject.MutAFVDisablePE:
			d, ok := rawDetail(m.Detail)
			if !ok {
				return fmt.Errorf("vpilot: mutation %s: bad RawOverwriteDetail", m.ID)
			}
			if peFile == nil {
				return fmt.Errorf("vpilot: PE not open for %s", m.ID)
			}
			if d.FileOffset == 0 || len(d.NewBytes) == 0 {
				continue
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
	data, err := w.ReadFile(path)
	if err != nil {
		// Missing file: create minimal config (lab / first-run).
		// If Stat succeeds, the file exists but is unreadable — fail (do not wipe).
		if _, stErr := w.Stat(path); stErr == nil {
			return fmt.Errorf("vpilot: read config %s: %w", path, err)
		}
		cfg := &vpilotconfig.Config{}
		vpilotconfig.ApplyEndpoints(cfg, d.NetworkStatusURL, d.CachedServers, d.ClearCredentials)
		out, ferr := vpilotconfig.Format(cfg)
		if ferr != nil {
			return fmt.Errorf("vpilot: format new config %s: %w", path, ferr)
		}
		if werr := w.WriteFile(path, out); werr != nil {
			return fmt.Errorf("vpilot: write config %s: %w", path, werr)
		}
		return nil
	}

	// Existing file: parse must succeed; preserve unknown XML via Rewrite.
	doc, err := vpilotconfig.ParseDocument(data)
	if err != nil {
		return fmt.Errorf("vpilot: parse config %s (refuse wipe): %w", path, err)
	}
	vpilotconfig.ApplyEndpoints(&doc.Config, d.NetworkStatusURL, d.CachedServers, d.ClearCredentials)
	out, err := doc.Format()
	if err != nil {
		return fmt.Errorf("vpilot: rewrite config %s: %w", path, err)
	}
	if err := w.WriteFile(path, out); err != nil {
		return fmt.Errorf("vpilot: write config %s: %w", path, err)
	}
	return nil
}

// fallbackBudget when Plan omitted BudgetBytes (should not happen for profile-driven plans).
func fallbackBudget(stringRef, newString string) int {
	switch stringRef {
	case "fsd_jwt":
		return 71
	case "afv_base":
		return 51
	case "fsd_auto":
		return 45
	default:
		return cilus.BodyBudgetBytes(newString)
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
	// Stock pad region uses budget-sized body; prefix width follows budget (≤0x7F → 1 byte).
	stockPrefix, err := cilus.LengthPrefixBytes(budget)
	if err != nil {
		return err
	}
	if len(prefix) != len(stockPrefix) {
		return fmt.Errorf("length prefix width changed (%d → %d); refuse in-place rewrite", len(stockPrefix), len(prefix))
	}
	prefixOff := bodyOff - int64(len(stockPrefix))
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
