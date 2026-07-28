// Package serviceapi holds pure JSON DTOs for the FSD process service-HTTP API
// (consumed by internal/web over authenticated HTTP; produced by internal/server).
//
// Keep this package free of session/postoffice/server imports so the web → server
// import edge can stay forbidden (AGENTS.md §2).
package serviceapi

import "time"

// OnlineUserGeneralData is shared identity/position state for online users.
type OnlineUserGeneralData struct {
	Callsign         string    `json:"callsign"`
	CID              int       `json:"cid"`
	Name             string    `json:"name"`
	NetworkRating    int       `json:"network_rating"`
	MaxNetworkRating int       `json:"max_network_rating"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	LogonTime        time.Time `json:"logon_time"`
	LastUpdated      time.Time `json:"last_updated"`
	// NodeID is the FSD edge node hosting this session (empty on single-node).
	NodeID string `json:"node_id,omitempty"`
	// ServerIdent is optional server identity string for multi-FSD UIs.
	ServerIdent string `json:"server_ident,omitempty"`
}

// OnlineUserPilot is a pilot entry in the online-users snapshot.
type OnlineUserPilot struct {
	OnlineUserGeneralData
	Altitude    int    `json:"altitude"`
	Groundspeed int    `json:"groundspeed"`
	Heading     int    `json:"heading"`
	Transponder string `json:"transponder"`
	// Synthetic is true for in-process sweatbox pilots (no TCP client).
	// Omitted from JSON when false so human pilots stay compact.
	Synthetic bool `json:"synthetic,omitempty"`

	// PilotRating is the VATSIM pilot rating wire ID from the certificate
	// at login (0,1,3,7,15,31,63). 0 if unknown (e.g. synthetic without DB).
	// Always present as a number (0 is valid P0).
	PilotRating int `json:"pilot_rating"`

	// FlightPlan is the session info-section string (no $FP source/dest),
	// empty if none filed. Same layout as session.FlightPlan / encodeFlightPlanInfo.
	FlightPlan string `json:"flight_plan,omitempty"`

	// AssignedBeaconCode is the ATC-assigned squawk if set; may be empty.
	AssignedBeaconCode string `json:"assigned_beacon_code,omitempty"`
}

// OnlineUserATC is an ATC entry in the online-users snapshot.
type OnlineUserATC struct {
	OnlineUserGeneralData
	Frequency string `json:"frequency"`
	Facility  int    `json:"facility"`
	VisRange  int    `json:"visual_range"`

	// TextATIS is multi-line controller ATIS when the server stores it.
	// Empty when not available (openfsd does not persist NEWINFO by default).
	TextATIS []string `json:"text_atis,omitempty"`
}

// OnlineUsersResponseData is the JSON body for GET /online_users.
type OnlineUsersResponseData struct {
	Pilots []OnlineUserPilot `json:"pilots"`
	ATC    []OnlineUserATC   `json:"atc"`
}
