// Package sweatbox implements the pure sweatbox simulator domain:
// taxi graph, aircraft state, and tick kinematics.
//
// File-shaped .apt/.air types and parsers live in pkg/twrfiles; this package
// re-exports them via type aliases and thin wrappers so call sites stay stable.
//
// Allowed imports: standard library, internal/geo, and pkg/twrfiles.
// Must not import session, postoffice, server, web, db, auth, metar, fsdclient, or protocol.
package sweatbox

import "github.com/renorris/openfsd/pkg/twrfiles"

// File-shaped types (canonical definitions in pkg/twrfiles).
type (
	Point    = twrfiles.Point
	Surface  = twrfiles.Surface
	Airport  = twrfiles.Airport
	Aircraft = twrfiles.Aircraft
)

// Surface kind identifiers (TWRTrainer section types).
const (
	SurfaceParking = twrfiles.SurfaceParking
	SurfaceRunway  = twrfiles.SurfaceRunway
	SurfaceTaxiway = twrfiles.SurfaceTaxiway
	SurfaceHold    = twrfiles.SurfaceHold
)

// Engine type codes used in .air files.
const (
	EnginePiston     = twrfiles.EnginePiston
	EngineTurboprop  = twrfiles.EngineTurboprop
	EngineJet        = twrfiles.EngineJet
	EngineHelicopter = twrfiles.EngineHelicopter
)

// Flight-plan / rules codes used in .air files.
const (
	RulesVFR  = twrfiles.RulesVFR
	RulesIFR  = twrfiles.RulesIFR
	RulesDVFR = twrfiles.RulesDVFR
	RulesSVFR = twrfiles.RulesSVFR
)

// Transponder mode codes used in .air files.
const (
	XPDRModeNormal  = twrfiles.XPDRModeNormal
	XPDRModeStandby = twrfiles.XPDRModeStandby
)
