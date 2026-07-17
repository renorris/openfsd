package sweatbox

import (
	"strings"
)

// tokenizeCommand splits an instructor command into upper-cased tokens.
// Commas are treated as whitespace so "AAL123, del" and "AAL123 del" both work
// for the callsign-prefix form after stripCallsignPrefix.
func tokenizeCommand(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	// Normalize commas to spaces.
	line = strings.Map(func(r rune) rune {
		if r == ',' {
			return ' '
		}
		return r
	}, line)
	fields := strings.Fields(line)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f) // preserve case for args like type ICAO; normalize per-cmd
	}
	return out
}

// stripCallsignPrefix detects "CALLSIGN, rest" or "CALLSIGN rest" when the first
// token looks like a callsign and the remainder is a known aircraft command.
// Returns target callsign (upper), remaining tokens, and whether a prefix was used.
//
// Global commands (add, p, un, ops) never consume a callsign prefix even if the
// first token resembles one — instructors type those without a target.
func stripCallsignPrefix(tokens []string) (cs string, rest []string, hadPrefix bool) {
	if len(tokens) == 0 {
		return "", nil, false
	}
	first := strings.ToUpper(tokens[0])
	// Bare global single-token or multi-token global verbs.
	if isGlobalVerb(first) {
		return "", tokens, false
	}
	if len(tokens) < 2 {
		return "", tokens, false
	}
	// If second token is a known aircraft verb, treat first as callsign.
	second := strings.ToLower(tokens[1])
	if isAircraftVerb(second) {
		return first, tokens[1:], true
	}
	return "", tokens, false
}

func isGlobalVerb(tok string) bool {
	switch strings.ToLower(tok) {
	case "add", "p", "pause", "un", "unp", "unpause", "u", "up", "ops", "stats":
		return true
	default:
		return false
	}
}

func isAircraftVerb(tok string) bool {
	switch strings.ToLower(tok) {
	case "del", "pos", "ph",
		"sq", "sqi", "sn", "ss", "id":
		return true
	// Reserved for later PRs so "CS, taxi …" still targets if typed early:
	case "taxi", "hold", "res", "cross", "cto", "ctoc",
		"fh", "fhn", "tr", "tl", "fph", "cm", "spd", "speed",
		"fp", "vp", "remarks":
		return true
	default:
		return false
	}
}

// normalizeVerb maps aliases to a canonical verb.
func normalizeVerb(tok string) string {
	switch strings.ToLower(tok) {
	case "p", "pause":
		return "pause"
	case "un", "unp", "unpause", "u", "up":
		return "unpause"
	case "ops", "stats":
		return "ops"
	case "ph":
		return "pos"
	default:
		return strings.ToLower(tok)
	}
}
