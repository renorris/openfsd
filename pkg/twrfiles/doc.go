// Package twrfiles implements pure TWRTrainer-compatible .apt / .air wire format:
// file-shaped types, ParseAPT, ParseAIR, FormatAPT, FormatAIR, and name validators.
//
// Stdlib only — no I/O beyond parsing/serializing text in memory.
// Sim domain (taxi graph, engine) lives in internal/sweatbox.
//
// Format emission follows the normative contract in docs/design/apt-air-editor.md
// (all headers, always runway displaced/turnoff, %.6f coords, Surfaces slice
// order, AIR 16 fields). Golden fixtures under testdata/*.formatted.* are the oracle.
package twrfiles
