package sweatbox

import (
	"fmt"
	"strconv"
	"strings"
)

// Default airport header values (TWRTrainer fallbacks).
const (
	defaultPatternSize    = 1.0
	defaultInitClimbProps = 3000.0
	defaultInitClimbJets  = 5000.0
	defaultRegistration   = "N"
)

// ParseAPT parses TWRTrainer-compatible .apt text.
// The returned Airport is populated with whatever could be read; errs lists
// validation and format issues (non-fatal — callers decide hard-fail policy).
func ParseAPT(text string) (Airport, []string) {
	apt := Airport{
		PatternSize:    defaultPatternSize,
		InitClimbProps: defaultInitClimbProps,
		InitClimbJets:  defaultInitClimbJets,
		Registration:   defaultRegistration,
	}
	var errs []string
	var cur *Surface

	// Track surface keys for duplicate detection: "KIND:NAME".
	seen := make(map[string]int) // key → first line number

	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		lineno := i + 1
		line := strings.TrimSpace(raw)
		// Strip CR from CRLF.
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		low := strings.ToLower(line)

		// Header keys.
		if strings.HasPrefix(low, "icao=") {
			apt.ICAO = strings.ToUpper(strings.TrimSpace(line[len("icao="):]))
			if len(apt.ICAO) != 4 {
				errs = append(errs, "ICAO code must be exactly 4 characters.")
			}
			continue
		}
		if strings.HasPrefix(low, "magnetic variation=") {
			v, err := parseFloatField(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Invalid magnetic variation on line %d", lineno))
				continue
			}
			apt.MagVar = v
			continue
		}
		if strings.HasPrefix(low, "field elevation=") {
			v, err := parseFloatField(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Invalid field elevation on line %d", lineno))
				continue
			}
			apt.FieldElev = v
			continue
		}
		if strings.HasPrefix(low, "pattern elevation=") {
			v, err := parseFloatField(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Invalid pattern elevation on line %d", lineno))
				continue
			}
			apt.PatternElev = v
			continue
		}
		if strings.HasPrefix(low, "pattern size=") {
			v, err := parseFloatField(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Invalid pattern size on line %d", lineno))
				continue
			}
			apt.PatternSize = v
			continue
		}
		if strings.HasPrefix(low, "initial climb props=") {
			v, err := parseFloatField(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Invalid initial climb props on line %d", lineno))
				continue
			}
			apt.InitClimbProps = v
			continue
		}
		if strings.HasPrefix(low, "initial climb jets=") {
			v, err := parseFloatField(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Invalid initial climb jets on line %d", lineno))
				continue
			}
			apt.InitClimbJets = v
			continue
		}
		if strings.HasPrefix(low, "jet airlines=") {
			// Preserve original case of prefixes after '=' (sample is uppercase).
			idx := strings.Index(line, "=")
			apt.JetAirlines = strings.TrimSpace(line[idx+1:])
			continue
		}
		if strings.HasPrefix(low, "turboprop airlines=") {
			idx := strings.Index(line, "=")
			apt.TurboAirlines = strings.TrimSpace(line[idx+1:])
			continue
		}
		if strings.HasPrefix(low, "registration=") {
			idx := strings.Index(line, "=")
			apt.Registration = strings.TrimSpace(line[idx+1:])
			continue
		}

		// Runway-only options (must appear after a RUNWAY section header).
		if strings.HasPrefix(low, "turnoff=") && cur != nil && cur.Kind == SurfaceRunway {
			val := strings.ToLower(strings.TrimSpace(line[len("turnoff="):]))
			cur.TurnoffLeft = val == "left"
			continue
		}
		if da, db, ok := parseDisplacedThreshold(line); ok {
			if cur != nil && cur.Kind == SurfaceRunway {
				cur.DispA = da
				cur.DispB = db
			} else {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
			}
			continue
		}

		// Section headers.
		if name, ok := matchSection(line, "PARKING"); ok {
			// PARKING names: \w+
			if !isWordName(name) {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
				cur = nil
				continue
			}
			s := Surface{Kind: SurfaceParking, Name: strings.ToUpper(name)}
			key := SurfaceParking + ":" + s.Name
			if first, dup := seen[key]; dup {
				errs = append(errs, fmt.Sprintf("Duplicate parking %s (first on line %d, again on line %d)", s.Name, first, lineno))
			} else {
				seen[key] = lineno
			}
			apt.Surfaces = append(apt.Surfaces, s)
			cur = &apt.Surfaces[len(apt.Surfaces)-1]
			continue
		}
		if a, b, ok := matchRunway(line); ok {
			s := Surface{
				Kind:        SurfaceRunway,
				Name:        strings.ToUpper(a) + "/" + strings.ToUpper(b),
				RwyA:        strings.ToUpper(a),
				RwyB:        strings.ToUpper(b),
				TurnoffLeft: true, // default
			}
			key := SurfaceRunway + ":" + s.Name
			if first, dup := seen[key]; dup {
				errs = append(errs, fmt.Sprintf("Duplicate runway %s (first on line %d, again on line %d)", s.Name, first, lineno))
			} else {
				seen[key] = lineno
			}
			apt.Surfaces = append(apt.Surfaces, s)
			cur = &apt.Surfaces[len(apt.Surfaces)-1]
			continue
		}
		if name, ok := matchSection(line, "TAXIWAY"); ok {
			if !isTaxiHoldName(name) {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
				cur = nil
				continue
			}
			s := Surface{Kind: SurfaceTaxiway, Name: strings.ToUpper(name)}
			key := SurfaceTaxiway + ":" + s.Name
			if first, dup := seen[key]; dup {
				errs = append(errs, fmt.Sprintf("Duplicate taxiway %s (first on line %d, again on line %d)", s.Name, first, lineno))
			} else {
				seen[key] = lineno
			}
			apt.Surfaces = append(apt.Surfaces, s)
			cur = &apt.Surfaces[len(apt.Surfaces)-1]
			continue
		}
		if name, ok := matchSection(line, "HOLD"); ok {
			if !isTaxiHoldName(name) {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
				cur = nil
				continue
			}
			s := Surface{Kind: SurfaceHold, Name: strings.ToUpper(name)}
			key := SurfaceHold + ":" + s.Name
			if first, dup := seen[key]; dup {
				errs = append(errs, fmt.Sprintf("Duplicate hold %s (first on line %d, again on line %d)", s.Name, first, lineno))
			} else {
				seen[key] = lineno
			}
			apt.Surfaces = append(apt.Surfaces, s)
			cur = &apt.Surfaces[len(apt.Surfaces)-1]
			continue
		}

		// Waypoint: "lat lon" with required decimal point (TWRTrainer regex).
		if lat, lon, ok := parsePoint(line); ok {
			if cur == nil {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
				continue
			}
			cur.Points = append(cur.Points, Point{Lat: lat, Lon: lon})
			continue
		}

		errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
	}

	// Post-validation: waypoint counts.
	for _, s := range apt.Surfaces {
		switch s.Kind {
		case SurfaceParking:
			if len(s.Points) != 1 {
				errs = append(errs, fmt.Sprintf("Parking area %s has no waypoint defined.", s.Name))
			}
		case SurfaceRunway:
			if len(s.Points) < 2 {
				errs = append(errs, fmt.Sprintf("Runway %s does not have at least two waypoints defined.", s.Name))
			}
		case SurfaceHold:
			if len(s.Points) != 1 {
				errs = append(errs, fmt.Sprintf("Hold %s has no waypoint defined.", s.Name))
			}
		case SurfaceTaxiway:
			if len(s.Points) < 2 {
				errs = append(errs, fmt.Sprintf("Taxiway %s does not have at least two waypoints defined.", s.Name))
			}
		}
	}

	return apt, errs
}

// parseFloatField extracts the value after a key= prefix.
func parseFloatField(line string) (float64, error) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return 0, fmt.Errorf("missing =")
	}
	return strconv.ParseFloat(strings.TrimSpace(line[idx+1:]), 64)
}

// parseDisplacedThreshold matches "displaced threshold=a/b".
func parseDisplacedThreshold(line string) (float64, float64, bool) {
	low := strings.ToLower(line)
	const prefix = "displaced threshold="
	if !strings.HasPrefix(low, prefix) {
		return 0, 0, false
	}
	rest := strings.TrimSpace(line[len(prefix):])
	// Allow original-case prefix length: find '=' then rest.
	if i := strings.Index(line, "="); i >= 0 {
		rest = strings.TrimSpace(line[i+1:])
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	b, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

// matchSection matches "[KIND name]" case-insensitively and returns the name.
func matchSection(line, kind string) (name string, ok bool) {
	if len(line) < 3 || line[0] != '[' || line[len(line)-1] != ']' {
		return "", false
	}
	inner := line[1 : len(line)-1]
	parts := strings.Fields(inner)
	if len(parts) != 2 {
		return "", false
	}
	if !strings.EqualFold(parts[0], kind) {
		return "", false
	}
	return parts[1], true
}

// matchRunway matches [RUNWAY a/b] with TWRTrainer runway designators.
func matchRunway(line string) (a, b string, ok bool) {
	if len(line) < 3 || line[0] != '[' || line[len(line)-1] != ']' {
		return "", "", false
	}
	inner := line[1 : len(line)-1]
	parts := strings.Fields(inner)
	if len(parts) != 2 {
		return "", "", false
	}
	if !strings.EqualFold(parts[0], "RUNWAY") {
		return "", "", false
	}
	ends := strings.Split(parts[1], "/")
	if len(ends) != 2 {
		return "", "", false
	}
	if !isRunwayDesignator(ends[0]) || !isRunwayDesignator(ends[1]) {
		return "", "", false
	}
	return ends[0], ends[1], true
}

// isRunwayDesignator matches (?:[1-2]\d|3[0-6]|[1-9])[LRC]?
func isRunwayDesignator(s string) bool {
	if s == "" {
		return false
	}
	s = strings.ToUpper(s)
	// Optional L/R/C suffix.
	suf := byte(0)
	last := s[len(s)-1]
	if last == 'L' || last == 'R' || last == 'C' {
		suf = last
		s = s[:len(s)-1]
	}
	if s == "" {
		return false
	}
	// Parse number 1–36, no leading zero (except we don't allow 0).
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 36 {
		return false
	}
	// Reject leading zeros: "01" etc. strconv accepts them but TWRTrainer does not.
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	// Single digit 1-9 OK; 10-36 OK; with optional LRC.
	_ = suf
	return true
}

// isWordName matches \w+ (ASCII letters, digits, underscore).
func isWordName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

// isTaxiHoldName matches [A-Z]+\d* (letters then optional digits), case-insensitive.
func isTaxiHoldName(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return false
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parsePoint matches ^(\-?\d+\.\d+) (\-?\d+\.\d+)$ — decimal point required.
func parsePoint(line string) (lat, lon float64, ok bool) {
	parts := strings.Fields(line)
	if len(parts) != 2 {
		return 0, 0, false
	}
	if !hasDecimalPoint(parts[0]) || !hasDecimalPoint(parts[1]) {
		return 0, 0, false
	}
	// hasDecimalPoint guarantees a ParseFloat-able digit form.
	lat, _ = strconv.ParseFloat(parts[0], 64)
	lon, _ = strconv.ParseFloat(parts[1], 64)
	return lat, lon, true
}

func hasDecimalPoint(s string) bool {
	// Must contain '.' and at least one digit on each side (optional leading '-').
	if s == "" {
		return false
	}
	start := 0
	if s[0] == '-' {
		start = 1
	}
	dot := strings.IndexByte(s[start:], '.')
	if dot < 0 {
		return false
	}
	// digit(s) before and after the dot
	before := s[start : start+dot]
	after := s[start+dot+1:]
	if before == "" || after == "" {
		return false
	}
	for i := 0; i < len(before); i++ {
		if before[i] < '0' || before[i] > '9' {
			return false
		}
	}
	for i := 0; i < len(after); i++ {
		if after[i] < '0' || after[i] > '9' {
			return false
		}
	}
	return true
}
