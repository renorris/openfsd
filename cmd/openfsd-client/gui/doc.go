// Package gui implements the openfsd Client Setup desktop UI (Fyne).
//
// Pure helpers (form state, legal banner, VATSIM host warnings, constraint
// formatting, settings) live in this package without Fyne imports so they can
// be unit-tested headless. Fyne window code is gated with //go:build !nogui.
//
// Layout is client-agnostic: enabled clients come from the adapter registry;
// future slots appear as "Coming soon". Constraints and blockers always come
// from Plan / Adapter APIs — never from if client_id == "vpilot" layout branches.
package gui
