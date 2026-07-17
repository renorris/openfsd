// Package sweatbox implements the pure sweatbox simulator domain:
// airport/scenario file parsing, taxi graph, aircraft state, and tick kinematics.
//
// Allowed imports: standard library and internal/geo only.
// Must not import session, postoffice, server, web, db, auth, metar, fsdclient, or protocol.
package sweatbox

import "strings"

// Surface kind identifiers (TWRTrainer section types).
const (
	SurfaceParking = "PARKING"
	SurfaceRunway  = "RUNWAY"
	SurfaceTaxiway = "TAXIWAY"
	SurfaceHold    = "HOLD"
)

// Engine type codes used in .air files.
const (
	EnginePiston     = "P"
	EngineTurboprop  = "T"
	EngineJet        = "J"
	EngineHelicopter = "H"
)

// Flight-plan / rules codes used in .air files.
const (
	RulesVFR  = "V"
	RulesIFR  = "I"
	RulesDVFR = "D"
	RulesSVFR = "S"
)

// Transponder mode codes used in .air files.
const (
	XPDRModeNormal  = "N"
	XPDRModeStandby = "S"
)

// Point is a geographic coordinate in decimal degrees.
type Point struct {
	Lat float64
	Lon float64
}

// Surface is one airport geometry element: parking, runway, taxiway, or hold.
type Surface struct {
	Kind   string // PARKING, RUNWAY, TAXIWAY, HOLD
	Name   string // parking/taxi/hold name, or "A/B" for runways
	Points []Point

	// Runway-only fields.
	RwyA        string  // first end designator (e.g. "19")
	RwyB        string  // reciprocal end designator (e.g. "1")
	DispA       float64 // displaced threshold feet for end A
	DispB       float64 // displaced threshold feet for end B
	TurnoffLeft bool    // true = left turnoff for end A (default true)
}

// Airport is a parsed .apt file: header metadata plus ordered surfaces.
type Airport struct {
	ICAO           string
	MagVar         float64 // magnetic variation, degrees (east positive convention of source file)
	FieldElev      float64 // field elevation, feet MSL
	PatternElev    float64 // pattern altitude, feet MSL
	PatternSize    float64 // pattern size scale, NM (default 1)
	InitClimbProps float64 // initial climb props, feet
	InitClimbJets  float64 // initial climb jets, feet
	JetAirlines    string  // comma-separated callsign prefixes
	TurboAirlines  string  // comma-separated callsign prefixes
	Registration   string  // single-letter GA callsign prefix (e.g. "N")
	Surfaces       []Surface
}

// FindSurface looks up a surface by name (case-insensitive).
// For runways, matches either end designator or the combined "A/B" name.
func (a *Airport) FindSurface(name string) *Surface {
	if a == nil {
		return nil
	}
	want := strings.ToUpper(name)
	for i := range a.Surfaces {
		s := &a.Surfaces[i]
		if s.Kind == SurfaceRunway {
			if s.RwyA == want || s.RwyB == want || s.Name == want {
				return s
			}
			continue
		}
		if strings.ToUpper(s.Name) == want {
			return s
		}
	}
	return nil
}

// Aircraft is one row from a .air scenario file (position/state snapshot).
type Aircraft struct {
	Callsign  string
	Type      string // ICAO type, may include equipment suffix (e.g. "B738/F")
	Engine    string // P, T, J, H
	Rules     string // V, I, D, S
	Dep       string
	Arr       string
	CruiseAlt int
	Route     string
	Remarks   string
	Squawk    string
	XPDRMode  string // N or S
	Lat       float64
	Lon       float64
	Alt       float64 // feet
	Speed     float64 // knots
	Heading   float64 // degrees
}
