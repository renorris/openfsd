// Package twrfiles implements pure TWRTrainer-compatible .apt / .air wire format:
// file-shaped types, ParseAPT, ParseAIR, and name validators.
//
// Stdlib only — no I/O beyond parsing text in memory. Format (serialize) is not
// implemented yet. Sim domain (taxi graph, engine) lives in internal/sweatbox.
package twrfiles
