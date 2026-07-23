package twrfiles

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseAIR parses TWRTrainer-compatible .air scenario text.
// Valid aircraft rows are returned even when other lines fail (best-effort load);
// errs lists per-line validation issues. Duplicate callsigns are rejected.
func ParseAIR(text string) ([]Aircraft, []string) {
	var rows []Aircraft
	var errs []string
	seen := make(map[string]int) // callsign → first line

	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		lineno := i + 1
		// TrimSpace also strips trailing \r from CRLF lines.
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		f := strings.Split(line, ":")
		if len(f) < 16 {
			errs = append(errs, fmt.Sprintf("Invalid number of fields found on line %d", lineno))
			continue
		}

		cs := strings.ToUpper(strings.TrimSpace(f[0]))
		if cs == "" {
			errs = append(errs, fmt.Sprintf("Missing callsign on line %d", lineno))
			continue
		}
		if first, dup := seen[cs]; dup {
			errs = append(errs, fmt.Sprintf("Duplicate callsign (%s) on line %d (first on line %d)", cs, lineno, first))
			continue
		}

		eng := strings.ToUpper(strings.TrimSpace(f[2]))
		if eng != EnginePiston && eng != EngineTurboprop && eng != EngineJet && eng != EngineHelicopter {
			errs = append(errs, fmt.Sprintf("Invalid engine type on line %d. Must be P, T, J or H.", lineno))
			continue
		}

		rules := strings.ToUpper(strings.TrimSpace(f[3]))
		if rules != RulesVFR && rules != RulesIFR && rules != RulesDVFR && rules != RulesSVFR {
			errs = append(errs, fmt.Sprintf("Invalid flight plan type on line %d. Must be V, I, D or S.", lineno))
			continue
		}

		mode := strings.ToUpper(strings.TrimSpace(f[10]))
		if mode != XPDRModeNormal && mode != XPDRModeStandby {
			errs = append(errs, fmt.Sprintf("Invalid transponder mode on line %d. Must be N or S. (Normal or Standby)", lineno))
			continue
		}

		sqk := strings.TrimSpace(f[9])
		if !IsSquawk(sqk) {
			errs = append(errs, fmt.Sprintf("Invalid squawk code on line %d", lineno))
			continue
		}

		cruiseAlt, err := strconv.ParseFloat(strings.TrimSpace(f[6]), 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("Invalid numeric field on line %d", lineno))
			continue
		}
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(f[11]), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(f[12]), 64)
		alt, err3 := strconv.ParseFloat(strings.TrimSpace(f[13]), 64)
		spd, err4 := strconv.ParseFloat(strings.TrimSpace(f[14]), 64)
		hdg, err5 := strconv.ParseFloat(strings.TrimSpace(f[15]), 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			errs = append(errs, fmt.Sprintf("Invalid numeric field on line %d", lineno))
			continue
		}

		rec := Aircraft{
			Callsign:  cs,
			Type:      strings.ToUpper(strings.TrimSpace(f[1])),
			Engine:    eng,
			Rules:     rules,
			Dep:       strings.ToUpper(strings.TrimSpace(f[4])),
			Arr:       strings.ToUpper(strings.TrimSpace(f[5])),
			CruiseAlt: int(cruiseAlt),
			Route:     f[7],
			Remarks:   f[8],
			Squawk:    sqk,
			XPDRMode:  mode,
			Lat:       lat,
			Lon:       lon,
			Alt:       alt,
			Speed:     spd,
			Heading:   hdg,
		}
		seen[cs] = lineno
		rows = append(rows, rec)
	}
	return rows, errs
}

// IsSquawk reports whether s is exactly four ASCII digits (TWRTrainer ^\d{4}$).
// Octal-digit-only policy is deferred; any 0-9 is accepted.
func IsSquawk(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
