package gui

// PublicVATSIMHosts is the known public VATSIM host set (web-base host match).
// Used only for a short status hint; not a lecture.
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
