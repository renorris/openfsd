package sweatbox

import "github.com/renorris/openfsd/pkg/twrfiles"

// ParseAIR parses TWRTrainer-compatible .air scenario text.
// Thin wrapper over pkg/twrfiles so internal/server and sim call sites stay stable.
func ParseAIR(text string) ([]Aircraft, []string) {
	return twrfiles.ParseAIR(text)
}

// isSquawk reports whether s is exactly four ASCII digits.
// Local wrapper so engine/dispatch need no renames.
func isSquawk(s string) bool {
	return twrfiles.IsSquawk(s)
}
