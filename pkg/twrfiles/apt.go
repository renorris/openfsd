package twrfiles

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultRegistration is the TWRTrainer fallback for registration= when the
// header omits it. Sim GA callsign generation should use the same value
// (see internal/sweatbox defaultRegistration).
const DefaultRegistration = "N"

// Default airport header values (TWRTrainer fallbacks).
const (
	defaultPatternSize    = 1.0
	defaultInitClimbProps = 3000.0
	defaultInitClimbJets  = 5000.0
)

// pathDupKey is the shared taxiway/hold name namespace used for duplicate detection
// (TWRTrainer treats taxiways and holds as one definition space).
const pathDupKey = "PATH"

// ParseAPT parses TWRTrainer-compatible .apt text.
// The returned Airport is populated with whatever could be read; errs lists
// validation and format issues (non-fatal — callers decide hard-fail policy).
func ParseAPT(text string) (Airport, []string) {
	apt := Airport{
		PatternSize:    defaultPatternSize,
		InitClimbProps: defaultInitClimbProps,
		InitClimbJets:  defaultInitClimbJets,
		Registration:   DefaultRegistration,
	}
	var errs []string
	var cur *Surface

	// Track surface keys for duplicate detection.
	// Parking/runway: "KIND:NAME". Taxiway and hold share "PATH:NAME".
	seen := make(map[string]int) // key → first line number

	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		lineno := i + 1
		// TrimSpace also strips trailing \r from CRLF lines.
		line := strings.TrimSpace(raw)
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
			idx := strings.Index(line, "=")
			val := strings.TrimSpace(line[idx+1:])
			apt.JetAirlines = val
			if !isValidAirlineList(val) {
				errs = append(errs, fmt.Sprintf("Invalid list of jet airlines on line %d", lineno))
			}
			continue
		}
		if strings.HasPrefix(low, "turboprop airlines=") {
			idx := strings.Index(line, "=")
			val := strings.TrimSpace(line[idx+1:])
			apt.TurboAirlines = val
			if !isValidAirlineList(val) {
				errs = append(errs, fmt.Sprintf("Invalid list of turboprop airlines on line %d", lineno))
			}
			continue
		}
		if strings.HasPrefix(low, "registration=") {
			idx := strings.Index(line, "=")
			val := strings.TrimSpace(line[idx+1:])
			apt.Registration = val
			if !isValidRegistration(val) {
				errs = append(errs, fmt.Sprintf("Invalid registration prefix on line %d", lineno))
			}
			continue
		}

		// Runway-only options (must appear after a RUNWAY section header).
		if strings.HasPrefix(low, "turnoff=") && cur != nil && cur.Kind == SurfaceRunway {
			val := strings.ToLower(strings.TrimSpace(line[len("turnoff="):]))
			switch val {
			case "left":
				cur.TurnoffLeft = true
			case "right":
				cur.TurnoffLeft = false
			default:
				// Leave prior/default value; report error (TWRTrainer: Invalid turnoff direction).
				errs = append(errs, fmt.Sprintf("Invalid turnoff direction found on line %d", lineno))
			}
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
			if !IsWordName(name) {
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
			if !IsTaxiHoldName(name) {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
				cur = nil
				continue
			}
			s := Surface{Kind: SurfaceTaxiway, Name: strings.ToUpper(name)}
			key := pathDupKey + ":" + s.Name
			if first, dup := seen[key]; dup {
				errs = append(errs, fmt.Sprintf("Duplicate taxiway or hold point definition %s (first on line %d, again on line %d)", s.Name, first, lineno))
			} else {
				seen[key] = lineno
			}
			apt.Surfaces = append(apt.Surfaces, s)
			cur = &apt.Surfaces[len(apt.Surfaces)-1]
			continue
		}
		if name, ok := matchSection(line, "HOLD"); ok {
			if !IsTaxiHoldName(name) {
				errs = append(errs, fmt.Sprintf("Unknown line format found on line %d", lineno))
				cur = nil
				continue
			}
			s := Surface{Kind: SurfaceHold, Name: strings.ToUpper(name)}
			key := pathDupKey + ":" + s.Name
			if first, dup := seen[key]; dup {
				errs = append(errs, fmt.Sprintf("Duplicate taxiway or hold point definition %s (first on line %d, again on line %d)", s.Name, first, lineno))
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
			switch {
			case len(s.Points) == 0:
				errs = append(errs, fmt.Sprintf("Parking area %s has no waypoint defined.", s.Name))
			case len(s.Points) > 1:
				errs = append(errs, fmt.Sprintf("Extra waypoint found in parking section %s.", s.Name))
			}
		case SurfaceRunway:
			if len(s.Points) < 2 {
				errs = append(errs, fmt.Sprintf("Runway %s does not have at least two waypoints defined.", s.Name))
			}
		case SurfaceHold:
			switch {
			case len(s.Points) == 0:
				errs = append(errs, fmt.Sprintf("Hold %s has no waypoint defined.", s.Name))
			case len(s.Points) > 1:
				errs = append(errs, fmt.Sprintf("Extra waypoint found in hold section %s.", s.Name))
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

// parseDisplacedThreshold matches TWRTrainer
// ^displaced threshold=(\d+)/(\d+)$ (non-negative integer feet only).
func parseDisplacedThreshold(line string) (float64, float64, bool) {
	low := strings.ToLower(line)
	const prefix = "displaced threshold="
	if !strings.HasPrefix(low, prefix) {
		return 0, 0, false
	}
	rest := ""
	if i := strings.Index(line, "="); i >= 0 {
		rest = strings.TrimSpace(line[i+1:])
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	aStr := strings.TrimSpace(parts[0])
	bStr := strings.TrimSpace(parts[1])
	if !isDigits(aStr) || !isDigits(bStr) {
		return 0, 0, false
	}
	// isDigits guarantees ParseFloat succeeds for digit-only strings.
	a, _ := strconv.ParseFloat(aStr, 64)
	b, _ := strconv.ParseFloat(bStr, 64)
	return a, b, true
}

// isDigits reports whether s is a non-empty sequence of ASCII digits [0-9].
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isValidRegistration matches TWRTrainer registration=(\w)$ — single word character.
func isValidRegistration(s string) bool {
	return len(s) == 1 && IsWordName(s)
}

// isValidAirlineList matches TWRTrainer jet/turboprop airline lists:
// empty (unset) or one-or-more 2–3 character \w prefixes separated by commas
// (trailing comma allowed).
func isValidAirlineList(s string) bool {
	if s == "" {
		return true
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		if p == "" {
			// Only a trailing empty segment from a final comma is allowed.
			if i == len(parts)-1 {
				continue
			}
			return false
		}
		if len(p) < 2 || len(p) > 3 || !IsWordName(p) {
			return false
		}
	}
	return true
}

// matchSection matches "[KIND name]" case-insensitively and returns the name.
//
// Intentional permissiveness: TWRTrainer section regexes use a single space
// after the kind keyword; we use strings.Fields so multiple spaces still match.
// Real airport samples use single spaces; multi-space is accepted for robustness.
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
// See matchSection for multi-space permissiveness.
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
	if !IsRunwayDesignator(ends[0]) || !IsRunwayDesignator(ends[1]) {
		return "", "", false
	}
	return ends[0], ends[1], true
}

// IsRunwayDesignator matches (?:[1-2]\d|3[0-6]|[1-9])[LRC]?
func IsRunwayDesignator(s string) bool {
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

// IsWordName matches \w+ (ASCII letters, digits, underscore).
func IsWordName(s string) bool {
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

// IsTaxiHoldName matches [A-Z]+\d* (letters then optional digits), case-insensitive.
func IsTaxiHoldName(s string) bool {
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

// parsePoint matches lat/lon decimal degrees with a required decimal point on each.
//
// Intentional permissiveness: TWRTrainer uses a single space between lat and lon
// (^(-?\d+\.\d+) (-?\d+\.\d+)$); we use strings.Fields so multiple spaces still
// match. Real airport samples use single spaces.
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
