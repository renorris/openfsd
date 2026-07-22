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
}

// OnlineUserATC is an ATC entry in the online-users snapshot.
type OnlineUserATC struct {
	OnlineUserGeneralData
	Frequency string `json:"frequency"`
	Facility  int    `json:"facility"`
	VisRange  int    `json:"visual_range"`
}

// OnlineUsersResponseData is the JSON body for GET /online_users.
type OnlineUsersResponseData struct {
	Pilots []OnlineUserPilot `json:"pilots"`
	ATC    []OnlineUserATC   `json:"atc"`
}
