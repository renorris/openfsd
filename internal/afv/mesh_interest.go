package afv

import (
	"log/slog"
	"sort"
	"sync/atomic"

	"github.com/renorris/openfsd/internal/geo"
)

// Interest helpers (M-5 / M-15): max TX range coverage, priority cap, dirty on first bind.

// interestMaxRangeNM is the max hearable TX class on frequency (not local IsATC).
func interestMaxRangeNM(cfg *Config, freqHz uint32) float64 {
	if freqHz == FrequencyUnicomHz {
		return cfg.MaxRangeNM(RangeClassUnicom)
	}
	// Non-UNICOM: advertise max(Default, ATC) so ATC→pilot is not filtered out.
	a := cfg.MaxRangeNM(RangeClassDefault)
	b := cfg.MaxRangeNM(RangeClassATC)
	if b > a {
		return b
	}
	return a
}

type interestLocal struct {
	freq uint32
	cell geo.CellKey
}

// atomicInterestTruncate counts cap truncations (observability).
var atomicInterestTruncate atomic.Uint64

// buildInterestEntries collects Interest from local bound sessions with radios.
func (r *Registry) buildInterestEntries(cfg *Config) []InterestEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make(map[FreqCell]bool) // true = local radio cell
	var locals []interestLocal

	for _, sess := range r.byTag {
		if sess == nil || !sess.Bound || len(sess.Transceivers) == 0 {
			continue
		}
		for _, trx := range sess.Transceivers {
			own := geo.CellKey{
				ILat: geo.CellIndex(trx.LatDeg, geo.DefaultGridCellDeg),
				ILon: geo.CellIndex(trx.LonDeg, geo.DefaultGridCellDeg),
			}
			fk := FreqCell{FreqHz: trx.Frequency, Cell: own}
			all[fk] = true
			locals = append(locals, interestLocal{freq: trx.Frequency, cell: own})

			maxM := NMToMeters(interestMaxRangeNM(cfg, trx.Frequency))
			min, max := geo.BoundingBox([2]float64{trx.LatDeg, trx.LonDeg}, maxM)
			keys := geo.CellCover(min, max, geo.DefaultGridCellDeg, nil)
			for _, k := range keys {
				fk2 := FreqCell{FreqHz: trx.Frequency, Cell: k}
				if _, ok := all[fk2]; !ok {
					all[fk2] = false
				}
			}
		}
	}

	if len(all) == 0 {
		return nil
	}
	entries := make([]InterestEntry, 0, len(all))
	for fk, isLoc := range all {
		_ = isLoc
		entries = append(entries, InterestEntry{
			FreqHz: fk.FreqHz,
			ILat:   fk.Cell.ILat,
			ILon:   fk.Cell.ILon,
		})
	}
	if len(entries) > interestMaxEntries {
		entries = capInterestEntries(entries, locals, interestMaxEntries)
		atomicInterestTruncate.Add(1)
		slog.Warn("AFV interest truncated", "kept", len(entries), "cap", interestMaxEntries)
	}
	return entries
}

// capInterestEntries keeps local radio cells first, then by increasing Chebyshev
// distance from any local cell, then (freq, iLat, iLon) for stability (M-5).
func capInterestEntries(entries []InterestEntry, locals []interestLocal, capN int) []InterestEntry {
	if len(entries) <= capN {
		return entries
	}
	type scored struct {
		e    InterestEntry
		dist int
		loc  bool
	}
	localSet := make(map[FreqCell]struct{}, len(locals))
	for _, l := range locals {
		localSet[FreqCell{FreqHz: l.freq, Cell: l.cell}] = struct{}{}
	}
	scoredList := make([]scored, 0, len(entries))
	for _, e := range entries {
		fk := FreqCell{FreqHz: e.FreqHz, Cell: geo.CellKey{ILat: e.ILat, ILon: e.ILon}}
		_, isLoc := localSet[fk]
		d := chebyshevToLocals(e, locals)
		scoredList = append(scoredList, scored{e: e, dist: d, loc: isLoc})
	}
	sort.SliceStable(scoredList, func(i, j int) bool {
		a, b := scoredList[i], scoredList[j]
		if a.loc != b.loc {
			return a.loc // locals first
		}
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if a.e.FreqHz != b.e.FreqHz {
			return a.e.FreqHz < b.e.FreqHz
		}
		if a.e.ILat != b.e.ILat {
			return a.e.ILat < b.e.ILat
		}
		return a.e.ILon < b.e.ILon
	})
	out := make([]InterestEntry, 0, capN)
	for i := 0; i < capN && i < len(scoredList); i++ {
		out = append(out, scoredList[i].e)
	}
	return out
}

func chebyshevToLocals(e InterestEntry, locals []interestLocal) int {
	if len(locals) == 0 {
		return 0
	}
	best := int(^uint(0) >> 1)
	for _, l := range locals {
		dlat := absInt32(e.ILat - l.cell.ILat)
		dlon := absInt32(e.ILon - l.cell.ILon)
		d := int(dlat)
		if int(dlon) > d {
			d = int(dlon)
		}
		if l.freq != e.FreqHz {
			d += 1_000_000
		}
		if d < best {
			best = d
		}
	}
	return best
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
