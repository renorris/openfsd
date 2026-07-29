package gui

// LegalOneLiner is shown in the window header.
const LegalOneLiner = "Private openfsd networks only — never redistribute proprietary clients."

// LegalApplyBanner is the soft banner shown on every Apply confirmation.
const LegalApplyBanner = "For private openfsd networks you are authorized to use. " +
	"Do not use this tool to reconfigure clients for the public VATSIM network."

// PublicVATSIMHosts is the known public VATSIM host set used for soft warnings
// when WebBaseURL (or related endpoints) point at production VATSIM services.
// Matching does not hard-block Apply; the user may override with "I understand".
var PublicVATSIMHosts = map[string]struct{}{
	"auth.vatsim.net":        {},
	"status.vatsim.net":      {},
	"data.vatsim.net":        {},
	"voice1.vatsim.net":      {},
	"voice2.vatsim.net":      {},
	"fsd.connect.vatsim.net": {},
	"api.vatsim.net":         {},
	"my.vatsim.net":          {},
	"metar.vatsim.net":       {},
	"server.vatsim.net":      {},
	"cert.vatsim.net":        {},
	"tracker.vatsim.net":     {},
	"map.vatsim.net":         {},
	"stats.vatsim.net":       {},
	"www.vatsim.net":         {},
	"vatsim.net":             {},
}
