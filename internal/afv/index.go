package afv

import (
	"github.com/renorris/openfsd/internal/geo"
)

// trxRef is a radio in the frequency spatial index.
type trxRef struct {
	session *VoiceSession
	trxID   uint16
	freqHz  uint32
	lat     float64
	lon     float64
	altM    float64
}

// freqIndex maps freqHz → cellKey → []trxRef for radio routing.
type freqIndex struct {
	// freq → cell → refs
	m map[uint32]map[geo.CellKey][]trxRef
}

func newFreqIndex() *freqIndex {
	return &freqIndex{m: make(map[uint32]map[geo.CellKey][]trxRef)}
}

func (idx *freqIndex) clear() {
	idx.m = make(map[uint32]map[geo.CellKey][]trxRef)
}

// removeSession drops all refs for a session pointer.
func (idx *freqIndex) removeSession(sess *VoiceSession) {
	if idx == nil || sess == nil {
		return
	}
	for freq, cells := range idx.m {
		for ck, refs := range cells {
			out := refs[:0]
			for _, r := range refs {
				if r.session != sess {
					out = append(out, r)
				}
			}
			if len(out) == 0 {
				delete(cells, ck)
			} else {
				cells[ck] = out
			}
		}
		if len(cells) == 0 {
			delete(idx.m, freq)
		}
	}
}

// addSession indexes all transceivers of a session.
func (idx *freqIndex) addSession(sess *VoiceSession) {
	if idx == nil || sess == nil {
		return
	}
	for _, t := range sess.Transceivers {
		ck := geo.CellKey{
			ILat: geo.CellIndex(t.LatDeg, geo.DefaultGridCellDeg),
			ILon: geo.CellIndex(t.LonDeg, geo.DefaultGridCellDeg),
		}
		cells := idx.m[t.Frequency]
		if cells == nil {
			cells = make(map[geo.CellKey][]trxRef)
			idx.m[t.Frequency] = cells
		}
		cells[ck] = append(cells[ck], trxRef{
			session: sess,
			trxID:   t.ID,
			freqHz:  t.Frequency,
			lat:     t.LatDeg,
			lon:     t.LonDeg,
			altM:    t.HeightMslM,
		})
	}
}

// candidates returns transceiver refs near (lat,lon) on freqHz within maxRangeM.
// Uses CellCover of the bounding box; callers must still apply exact range.
func (idx *freqIndex) candidates(freqHz uint32, lat, lon float64, maxRangeM float64) []trxRef {
	if idx == nil {
		return nil
	}
	cells := idx.m[freqHz]
	if len(cells) == 0 {
		return nil
	}
	min, max := geo.BoundingBox([2]float64{lat, lon}, maxRangeM)
	var keys []geo.CellKey
	keys = geo.CellCover(min, max, geo.DefaultGridCellDeg, keys)
	var out []trxRef
	seen := make(map[*VoiceSession]map[uint16]struct{})
	for _, ck := range keys {
		for _, r := range cells[ck] {
			sm := seen[r.session]
			if sm == nil {
				sm = make(map[uint16]struct{})
				seen[r.session] = sm
			}
			if _, ok := sm[r.trxID]; ok {
				continue
			}
			sm[r.trxID] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}
