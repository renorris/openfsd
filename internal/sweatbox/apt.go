package sweatbox

import "github.com/renorris/openfsd/pkg/twrfiles"

// defaultRegistration is the GA callsign prefix when airport Registration is
// empty. Single source of truth: pkg/twrfiles.DefaultRegistration (ParseAPT fallback).
const defaultRegistration = twrfiles.DefaultRegistration

// ParseAPT parses TWRTrainer-compatible .apt text.
// Thin wrapper over pkg/twrfiles so internal/server and sim call sites stay stable.
func ParseAPT(text string) (Airport, []string) {
	return twrfiles.ParseAPT(text)
}

// isWordName matches \w+ (ASCII letters, digits, underscore).
// Local wrapper so taxi.go and other sim code need no renames.
func isWordName(s string) bool {
	return twrfiles.IsWordName(s)
}
