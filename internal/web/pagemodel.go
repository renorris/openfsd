package web

import (
	"fmt"
	"strings"

	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

// pageUser is the authenticated user projection for HTML templates.
type pageUser struct {
	CID                int
	DisplayName        string
	FirstName          string
	LastName           string
	NetworkRating      int
	NetworkRatingLabel string
	CanEditUsers       bool
	CanEditConfig      bool
}

// basePage is embedded by every HTML page model so layout has nav data.
type basePage struct {
	User      *pageUser
	CSRFToken string
}

type loginPage struct {
	basePage
	CID        string
	RememberMe bool
	Error      string
	CIDError   string
	PassError  string
}

// connectionRow is one pilot or ATC line in the dashboard summary table.
type connectionRow struct {
	Callsign  string
	CID       int
	Name      string
	Kind      string // "pilot" or "atc"
	Detail    string // altitude/gs or frequency
	Synthetic bool   // sweatbox in-process pilot (badge on dashboard)
}

type dashboardPage struct {
	basePage
	// Server-rendered connection summary (no-JS path). Map is enhancement only.
	ConnectionCount    int
	PilotCount         int
	ATCCount           int
	Connections        []connectionRow
	SummaryAvailable   bool
	SummaryUnavailable bool
	SummaryError       string
}

// ratingOption is a network-rating select entry.
type ratingOption struct {
	Value    int
	Label    string
	Selected bool
}

// userForm holds create/edit form field values and field-level errors.
type userForm struct {
	CID           string
	FirstName     string
	LastName      string
	Password      string
	NetworkRating int
	CIDError      string
	PasswordError string
	RatingError   string
	Error         string
}

type userEditorPage struct {
	basePage
	FlashSuccess string
	FlashError   string
	// Create form (left column)
	Create userForm
	// Search CID field
	SearchCID string
	// Edit form (right column); Edit.CID non-empty means a user was loaded
	Edit          userForm
	EditLoaded    bool
	RatingOptions []ratingOption
}

// configField is one editable config key for the config editor form.
type configField struct {
	Key         string
	Label       string
	Description string
	Value       string
	Placeholder string
}

type configEditorPage struct {
	basePage
	Fields       []configField
	FlashSuccess string
	FlashError   string
	// CreatedToken is set after a successful form POST create-token (server-rendered, escaped).
	CreatedToken string
	TokenExpiry  string
}

// sweatboxAircraftRow is one aircraft line in the instructor table (no-JS).
type sweatboxAircraftRow struct {
	Callsign    string
	Type        string
	Rules       string
	Squawk      string
	Heading     string
	Alt         string
	Speed       string
	Status      string
	Instruction string
}

// sweatboxPage is the Administrator instructor MPA model.
type sweatboxPage struct {
	basePage
	FlashSuccess string
	FlashError   string

	// Availability of the FSD sweatbox control plane.
	Available   bool // state snapshot fetched OK
	Disabled    bool // FSD returned 404 (SWEATBOX_ENABLED off / no routes)
	Unavailable bool // FSD HTTP unreachable or unexpected error
	StatusMsg   string

	// Live snapshot fields
	ICAO     string
	Paused   bool
	Elapsed  string
	ArrCount int
	DepCount int
	Aircraft []sweatboxAircraftRow
}

// airportEditorPage is the Administrator airport .apt/.air editor MPA model.
// No durable server state — document lives in the browser; download is echo-only.
type airportEditorPage struct {
	basePage
	FlashSuccess string
	FlashError   string
}

func pageUserFromClaims(claims *auth.CustomClaims) *pageUser {
	if claims == nil {
		return nil
	}
	display := strings.TrimSpace(claims.FirstName + " " + claims.LastName)
	if display == "" {
		display = fmt.Sprintf("CID %d", claims.CID)
	}
	rating := int(claims.NetworkRating)
	return &pageUser{
		CID:                claims.CID,
		DisplayName:        display,
		FirstName:          claims.FirstName,
		LastName:           claims.LastName,
		NetworkRating:      rating,
		NetworkRatingLabel: networkRatingLabel(rating),
		CanEditUsers:       claims.NetworkRating >= protocol.NetworkRatingSupervisor,
		CanEditConfig:      claims.NetworkRating >= protocol.NetworkRatingAdministator,
	}
}

func networkRatingLabel(val int) string {
	switch protocol.NetworkRating(val) {
	case protocol.NetworkRatingInactive:
		return "Inactive"
	case protocol.NetworkRatingSuspended:
		return "Suspended"
	case protocol.NetworkRatingObserver:
		return "Observer"
	case protocol.NetworkRatingStudent1:
		return "Student 1"
	case protocol.NetworkRatingStudent2:
		return "Student 2"
	case protocol.NetworkRatingStudent3:
		return "Student 3"
	case protocol.NetworkRatingController1:
		return "Controller 1"
	case protocol.NetworkRatingController2:
		return "Controller 2"
	case protocol.NetworkRatingController3:
		return "Controller 3"
	case protocol.NetworkRatingInstructor1:
		return "Instructor 1"
	case protocol.NetworkRatingInstructor2:
		return "Instructor 2"
	case protocol.NetworkRatingInstructor3:
		return "Instructor 3"
	case protocol.NetworkRatingSupervisor:
		return "Supervisor"
	case protocol.NetworkRatingAdministator:
		return "Administrator"
	default:
		return "Unknown"
	}
}

// ratingOptionsUpTo returns network rating select options from Inactive (−1)
// through maxInclusive (clamped to Administrator). Actors only see ratings
// they are allowed to assign (server still enforces the ceiling).
func ratingOptionsUpTo(maxInclusive int, selected int) []ratingOption {
	if maxInclusive > int(protocol.NetworkRatingAdministator) {
		maxInclusive = int(protocol.NetworkRatingAdministator)
	}
	if maxInclusive < int(protocol.NetworkRatingInactive) {
		maxInclusive = int(protocol.NetworkRatingInactive)
	}
	out := make([]ratingOption, 0, maxInclusive-int(protocol.NetworkRatingInactive)+1)
	for v := int(protocol.NetworkRatingInactive); v <= maxInclusive; v++ {
		out = append(out, ratingOption{
			Value:    v,
			Label:    networkRatingLabel(v),
			Selected: v == selected,
		})
	}
	return out
}

// editableConfigKeys is the allowlist of config keys shown/mutated via the form UI.
// Keys use db.Config* constants so the form allowlist cannot drift from the repository.
var editableConfigKeys = []struct {
	Key         string
	Label       string
	Description string
	Placeholder string
}{
	{
		Key:         db.ConfigWelcomeMessage,
		Label:       "Welcome Message",
		Description: "Welcome message sent to FSD clients after they connect",
		Placeholder: "Welcome to my FSD server!",
	},
	{
		Key:         db.ConfigFsdServerHostname,
		Label:       "FSD Server Hostname",
		Description: "Server hostname advertised to clients",
		Placeholder: "myfsdserver.com",
	},
	{
		Key:         db.ConfigFsdServerIdent,
		Label:       "FSD Server Ident",
		Description: "Server ident advertised to clients",
		Placeholder: "MY-FSD-SERVER",
	},
	{
		Key:         db.ConfigFsdServerLocation,
		Label:       "FSD Server Location",
		Description: "Geographical server location advertised to clients",
		Placeholder: "East US",
	},
	{
		Key:         db.ConfigApiServerBaseURL,
		Label:       "API Server Base URL",
		Description: "API server base URL advertised to clients",
		Placeholder: "https://example.com",
	},
}

func isEditableConfigKey(key string) bool {
	for _, k := range editableConfigKeys {
		if k.Key == key {
			return true
		}
	}
	return false
}
