// Package gui implements the openfsd Client Setup desktop UI (Fyne).
//
// Pure helpers (form state, settings, install detection) live in this package
// without Fyne imports so they can be unit-tested headless. Fyne window code is
// gated with //go:build !nogui.
//
// Layout is client-agnostic: enabled clients come from the adapter registry.
// Product voice: short labels and status only — no didactic disclaimer copy.
package gui
