package sweatbox

import "time"

// AircraftSnapshot is a value copy of one aircraft for host / HTTP / UI.
// Domain fields only — wire encoding lives in internal/server.
type AircraftSnapshot struct {
	Callsign string
	Type     string
	Engine   string
	Rules    string
	Weight   string

	Dep       string
	Arr       string
	CruiseAlt int
	Route     string
	Remarks   string

	Squawk   string
	XPDRMode string
	Ident    bool

	Lat     float64
	Lon     float64
	Alt     float64
	Speed   float64
	Heading float64

	Status      string
	Instruction string

	CurrentSurface string
	Parking        string
	LandingRunway  string
	DepRunway      string
	PositionHold   bool

	// Ground clearance / taxi summary (path waypoints stay engine-internal).
	HoldShortOf    string
	ClearedTakeoff bool
	DepHeading     float64
	HasDepHeading  bool
	PatternTraffic string // "", "L", or "R"
	NoStop         bool
	TaxiWPIndex    int
	TaxiParking    string
	TaxiSteps      []string

	// Air vector targets (domain; host/UI may surface instruction instead).
	DesiredHeading    float64
	HasDesiredHeading bool
	TurnDir           int
	ImmediateHeading  bool
	DesiredAlt        float64
	HasDesiredAlt     bool
	DesiredSpeed      float64
	HasDesiredSpeed   bool
}

// EngineSnapshot is a point-in-time view of the pure engine (no sessions).
type EngineSnapshot struct {
	ICAO     string
	Paused   bool
	Elapsed  time.Duration
	ArrCount int
	DepCount int
	Aircraft []AircraftSnapshot
}

// OpsStats is the session statistics returned by the ops/stats command.
type OpsStats struct {
	Elapsed  time.Duration
	ArrCount int
	DepCount int
	// OpsPerMin is (arr+dep) / elapsed minutes; 0 when elapsed is zero.
	OpsPerMin float64
}

// CommandResult is the outcome of an instructor text command.
// Soft validation failures set OK=false with a TWRTrainer-style Message.
// Successful silent commands set OK=true and empty Message (except ops).
type CommandResult struct {
	OK      bool
	Message string

	// Added / Deleted callsigns mutated by this command (host applies sessions).
	Added   []AircraftSnapshot
	Deleted []string
}
