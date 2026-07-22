package serviceapi

// SweatboxCommandRequest is the JSON body for POST /sweatbox/command.
type SweatboxCommandRequest struct {
	// Callsign is optional UI-selected aircraft for aircraft-scoped verbs.
	Callsign string `json:"callsign"`
	// Command is the instructor text command (required).
	Command string `json:"command"`
}

// SweatboxCommandResponse is returned for POST /sweatbox/command.
// Soft validation failures use HTTP 200 with OK=false (TWRTrainer-style).
type SweatboxCommandResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// SweatboxScenarioResponse is returned for POST /sweatbox/scenario.
type SweatboxScenarioResponse struct {
	Loaded int      `json:"loaded"`
	Errors []string `json:"errors"`
}

// SweatboxOpsJSON is returned for GET /sweatbox/ops.
type SweatboxOpsJSON struct {
	ElapsedSec float64 `json:"elapsed_sec"`
	ArrCount   int     `json:"arr_count"`
	DepCount   int     `json:"dep_count"`
	OpsPerMin  float64 `json:"ops_per_min"`
	Message    string  `json:"message,omitempty"`
}

// SweatboxAircraftJSON is one aircraft in State(), with session wire overrides.
type SweatboxAircraftJSON struct {
	Callsign    string  `json:"callsign"`
	Type        string  `json:"type"`
	Rules       string  `json:"rules"`
	Squawk      string  `json:"squawk"`
	XPDRMode    string  `json:"xpdr_mode"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Alt         float64 `json:"alt"`
	Speed       float64 `json:"speed"`
	Heading     float64 `json:"heading"`
	Status      string  `json:"status"`
	Instruction string  `json:"instruction"`
	// FlightPlan is the live wire info section from the session (ATC $AM wins).
	FlightPlan string `json:"flight_plan"`
	Dep        string `json:"dep"`
	Arr        string `json:"arr"`
	CruiseAlt  int    `json:"cruise_alt"`
	Route      string `json:"route"`
	Remarks    string `json:"remarks"`
}

// SweatboxStateJSON is the instructor/UI snapshot over GET /sweatbox/state.
type SweatboxStateJSON struct {
	ICAO     string                 `json:"icao"`
	Paused   bool                   `json:"paused"`
	Elapsed  float64                `json:"elapsed_sec"`
	ArrCount int                    `json:"arr_count"`
	DepCount int                    `json:"dep_count"`
	Aircraft []SweatboxAircraftJSON `json:"aircraft"`
}
