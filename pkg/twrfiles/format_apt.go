package twrfiles

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatAPT serializes an Airport to TWRTrainer-compatible .apt text.
//
// Normative contract (docs/design/apt-air-editor.md):
//   - Always emit all ten header keys in fixed order (parse defaults when zero/empty)
//   - One blank line after headers; one blank line between surfaces
//   - Surfaces in Airport.Surfaces slice order (not re-bucketed by kind)
//   - RUNWAY always emits displaced threshold and turnoff
//   - Coordinates always %.6f %.6f; LF only; trailing newline
func FormatAPT(a Airport) string {
	var b strings.Builder

	icao := strings.ToUpper(a.ICAO)
	patternSize := a.PatternSize
	if patternSize == 0 {
		patternSize = defaultPatternSize
	}
	initProps := a.InitClimbProps
	if initProps == 0 {
		initProps = defaultInitClimbProps
	}
	initJets := a.InitClimbJets
	if initJets == 0 {
		initJets = defaultInitClimbJets
	}
	reg := a.Registration
	if reg == "" {
		reg = DefaultRegistration
	}

	b.WriteString("icao=")
	b.WriteString(icao)
	b.WriteByte('\n')

	b.WriteString("magnetic variation=")
	b.WriteString(formatHeaderFloat(a.MagVar))
	b.WriteByte('\n')

	b.WriteString("field elevation=")
	b.WriteString(formatHeaderFloat(a.FieldElev))
	b.WriteByte('\n')

	b.WriteString("pattern elevation=")
	b.WriteString(formatHeaderFloat(a.PatternElev))
	b.WriteByte('\n')

	b.WriteString("pattern size=")
	b.WriteString(formatHeaderFloat(patternSize))
	b.WriteByte('\n')

	b.WriteString("initial climb props=")
	b.WriteString(formatHeaderFloat(initProps))
	b.WriteByte('\n')

	b.WriteString("initial climb jets=")
	b.WriteString(formatHeaderFloat(initJets))
	b.WriteByte('\n')

	b.WriteString("jet airlines=")
	b.WriteString(a.JetAirlines)
	b.WriteByte('\n')

	b.WriteString("turboprop airlines=")
	b.WriteString(a.TurboAirlines)
	b.WriteByte('\n')

	b.WriteString("registration=")
	b.WriteString(reg)
	b.WriteByte('\n')

	// Blank line after headers (also provides trailing newline when no surfaces).
	b.WriteByte('\n')

	for i, s := range a.Surfaces {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeSurface(&b, s)
	}

	return b.String()
}

func writeSurface(b *strings.Builder, s Surface) {
	switch s.Kind {
	case SurfaceRunway:
		name := s.Name
		if name == "" && (s.RwyA != "" || s.RwyB != "") {
			name = s.RwyA + "/" + s.RwyB
		}
		b.WriteString("[RUNWAY ")
		b.WriteString(name)
		b.WriteString("]\n")
		b.WriteString("displaced threshold=")
		b.WriteString(strconv.FormatInt(int64(s.DispA), 10))
		b.WriteByte('/')
		b.WriteString(strconv.FormatInt(int64(s.DispB), 10))
		b.WriteByte('\n')
		if s.TurnoffLeft {
			b.WriteString("turnoff=left\n")
		} else {
			b.WriteString("turnoff=right\n")
		}
	case SurfaceParking:
		b.WriteString("[PARKING ")
		b.WriteString(s.Name)
		b.WriteString("]\n")
	case SurfaceTaxiway:
		b.WriteString("[TAXIWAY ")
		b.WriteString(s.Name)
		b.WriteString("]\n")
	case SurfaceHold:
		b.WriteString("[HOLD ")
		b.WriteString(s.Name)
		b.WriteString("]\n")
	default:
		// Unknown kind: still emit a section so round-trip retains data order.
		b.WriteString("[")
		b.WriteString(s.Kind)
		b.WriteByte(' ')
		b.WriteString(s.Name)
		b.WriteString("]\n")
	}

	for _, p := range s.Points {
		b.WriteString(fmt.Sprintf("%.6f %.6f\n", p.Lat, p.Lon))
	}
}

// formatHeaderFloat emits a compact deterministic representation of a header
// numeric field (no unnecessary trailing zeros).
func formatHeaderFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
