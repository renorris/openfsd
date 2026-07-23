package twrfiles

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatAIR serializes aircraft rows to TWRTrainer-compatible .air text.
//
// Normative contract (docs/design/apt-air-editor.md):
//   - Exactly 16 colon-separated fields per row; no trailing colon
//   - Callsign, Type, Engine, Rules, Dep, Arr, XPDRMode uppercased
//   - Lat/Lon always %.6f; Alt/Speed/Heading compact (integer form when whole)
//   - Route/Remarks as stored; LF between lines; trailing newline when non-empty
//   - Slice order preserved; no field-legend comment block
func FormatAIR(rows []Aircraft) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	for i, ac := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeAircraftLine(&b, ac)
	}
	b.WriteByte('\n')
	return b.String()
}

func writeAircraftLine(b *strings.Builder, ac Aircraft) {
	// 0 callsign
	b.WriteString(strings.ToUpper(ac.Callsign))
	b.WriteByte(':')
	// 1 type
	b.WriteString(strings.ToUpper(ac.Type))
	b.WriteByte(':')
	// 2 engine
	b.WriteString(strings.ToUpper(ac.Engine))
	b.WriteByte(':')
	// 3 rules
	b.WriteString(strings.ToUpper(ac.Rules))
	b.WriteByte(':')
	// 4 dep
	b.WriteString(strings.ToUpper(ac.Dep))
	b.WriteByte(':')
	// 5 arr
	b.WriteString(strings.ToUpper(ac.Arr))
	b.WriteByte(':')
	// 6 cruise alt (integer)
	b.WriteString(strconv.Itoa(ac.CruiseAlt))
	b.WriteByte(':')
	// 7 route (as stored)
	b.WriteString(ac.Route)
	b.WriteByte(':')
	// 8 remarks (as stored)
	b.WriteString(ac.Remarks)
	b.WriteByte(':')
	// 9 squawk
	b.WriteString(ac.Squawk)
	b.WriteByte(':')
	// 10 xpdr mode
	b.WriteString(strings.ToUpper(ac.XPDRMode))
	b.WriteByte(':')
	// 11 lat
	b.WriteString(fmt.Sprintf("%.6f", ac.Lat))
	b.WriteByte(':')
	// 12 lon
	b.WriteString(fmt.Sprintf("%.6f", ac.Lon))
	b.WriteByte(':')
	// 13 alt
	b.WriteString(formatAirCompactFloat(ac.Alt))
	b.WriteByte(':')
	// 14 speed
	b.WriteString(formatAirCompactFloat(ac.Speed))
	b.WriteByte(':')
	// 15 heading
	b.WriteString(formatAirCompactFloat(ac.Heading))
}

// formatAirCompactFloat emits an integer decimal string when v is a whole
// number; otherwise the shortest ParseFloat-round-tripping form.
func formatAirCompactFloat(v float64) string {
	if !math.IsNaN(v) && !math.IsInf(v, 0) && v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
