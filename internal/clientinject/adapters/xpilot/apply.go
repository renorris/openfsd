package xpilot

import (
	"context"
	"fmt"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/pepatch"
)

// Apply writes plan mutations via FileWriter.
func (a *Adapter) Apply(ctx context.Context, plan *clientinject.Plan, w clientinject.FileWriter) error {
	_ = ctx
	if plan == nil {
		return fmt.Errorf("xpilot: nil plan")
	}
	if w == nil {
		return fmt.Errorf("xpilot: nil FileWriter")
	}
	pePath := clientinject.AbsPrimaryPE(plan.Install)
	if pePath == "" {
		return fmt.Errorf("xpilot: PE mutation planned but PrimaryPE empty")
	}

	needPE := false
	for _, m := range plan.Mutations {
		switch m.Kind {
		case clientinject.MutPaddedString, clientinject.MutRawOverwrite:
			needPE = true
		}
	}
	if !needPE {
		return nil
	}

	peFile, err := w.OpenReadWrite(pePath)
	if err != nil {
		return fmt.Errorf("xpilot: open PE: %w", err)
	}
	defer peFile.Close()

	for _, m := range plan.Mutations {
		switch m.Kind {
		case clientinject.MutPaddedString:
			d, ok := paddedDetail(m.Detail)
			if !ok {
				return fmt.Errorf("xpilot: mutation %s: bad PaddedStringDetail", m.ID)
			}
			if err := pepatch.PaddedStringOverwrite(peFile, d.FileOffset, d.NewString, d.SlotLen, d.Encoding); err != nil {
				return fmt.Errorf("xpilot: %s: %w", m.ID, err)
			}
		case clientinject.MutRawOverwrite:
			d, ok := rawDetail(m.Detail)
			if !ok {
				return fmt.Errorf("xpilot: mutation %s: bad RawOverwriteDetail", m.ID)
			}
			if len(d.NewBytes) == 0 {
				continue
			}
			if err := pepatch.OverwriteAt(peFile, d.FileOffset, d.NewBytes); err != nil {
				return fmt.Errorf("xpilot: %s: %w", m.ID, err)
			}
		case clientinject.MutLaunchFlag:
			continue
		default:
			return fmt.Errorf("xpilot: unsupported mutation kind %q (id=%s)", m.Kind, m.ID)
		}
	}
	return nil
}

func paddedDetail(d any) (clientinject.PaddedStringDetail, bool) {
	switch v := d.(type) {
	case clientinject.PaddedStringDetail:
		return v, true
	case *clientinject.PaddedStringDetail:
		if v == nil {
			return clientinject.PaddedStringDetail{}, false
		}
		return *v, true
	default:
		return clientinject.PaddedStringDetail{}, false
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
