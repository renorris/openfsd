package clientinject

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// DefaultFSDPort is the classic FSD TCP port.
const DefaultFSDPort = 6809

// Endpoints holds user-configured openfsd connection surfaces (client-agnostic).
type Endpoints struct {
	// Public HTTPS origin of openfsd web, no trailing slash.
	WebBaseURL string

	// FSD hostname only, or host:port if non-default port is required.
	FSDHost string

	// TCP port for FSD. Zero means default 6809 and omit from server list
	// unless IncludePortInServerList is true.
	FSDPort int

	// If true, CachedServers uses "NAME|host:port" even when port is 6809.
	IncludePortInServerList bool

	// Human label for cached server row, e.g. "OPENFSD".
	FSDServerName string

	// AFV REST public base, e.g. "https://voice.example.com".
	AFVBaseURL string

	// Prefer no voice (CLI -novoice / skip AFV #US retarget).
	ForceDisableAFV bool

	// Prefer short JWT path templates when server short paths are available.
	PreferShortJWTPath bool

	// Optional override of short-path candidates (empty = defaults).
	ShortJWTPathCandidates []string
}

// DefaultShortJWTPathCandidates returns the default short JWT path list
// (shortest first for #US budget).
func DefaultShortJWTPathCandidates() []string {
	return []string{"/j", "/fsd-jwt", "/api/fsd-jwt"}
}

// Normalize returns a copy with WebBaseURL trimmed of trailing slash and
// default port zeroed when equal to DefaultFSDPort (callers still see 0 = default).
func (e Endpoints) Normalize() Endpoints {
	out := e
	out.WebBaseURL = strings.TrimRight(strings.TrimSpace(e.WebBaseURL), "/")
	out.FSDHost = strings.TrimSpace(e.FSDHost)
	out.FSDServerName = strings.TrimSpace(e.FSDServerName)
	out.AFVBaseURL = strings.TrimRight(strings.TrimSpace(e.AFVBaseURL), "/")
	if out.FSDPort == DefaultFSDPort {
		out.FSDPort = 0
	}
	return out
}

// JWTURL returns the FSD JWT POST URL. When PreferShortJWTPath is true and
// candidates are non-empty, the first candidate path is used; otherwise the
// default /api/v1/fsd-jwt path is used. Soft probe of live routes is left to
// a higher layer — this helper only picks a path template.
func (e Endpoints) JWTURL() string {
	n := e.Normalize()
	base := n.WebBaseURL
	if base == "" {
		return ""
	}
	if n.PreferShortJWTPath {
		cands := n.ShortJWTPathCandidates
		if len(cands) == 0 {
			cands = DefaultShortJWTPathCandidates()
		}
		path := cands[0]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return base + path
	}
	return base + "/api/v1/fsd-jwt"
}

// StatusURL returns WebBaseURL + /api/v1/data/status.txt.
func (e Endpoints) StatusURL() string {
	n := e.Normalize()
	if n.WebBaseURL == "" {
		return ""
	}
	return n.WebBaseURL + "/api/v1/data/status.txt"
}

// StatusJSONURL returns WebBaseURL + /api/v1/data/status.json.
func (e Endpoints) StatusJSONURL() string {
	n := e.Normalize()
	if n.WebBaseURL == "" {
		return ""
	}
	return n.WebBaseURL + "/api/v1/data/status.json"
}

// FSDAddress returns host or host:port for CachedServers / launch override.
// Default port 6809 is omitted unless IncludePortInServerList is true.
func (e Endpoints) FSDAddress() string {
	n := e.Normalize()
	host := n.FSDHost
	if host == "" {
		return ""
	}
	// If caller already put host:port in FSDHost, respect it.
	if h, p, err := net.SplitHostPort(host); err == nil {
		portNum, _ := strconv.Atoi(p)
		if portNum == DefaultFSDPort && !n.IncludePortInServerList {
			return h
		}
		return host
	}
	port := n.FSDPort
	if port == 0 {
		port = DefaultFSDPort
	}
	if port == DefaultFSDPort && !n.IncludePortInServerList {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// CachedServerEntry returns "NAME|address" for vPilot-style CachedServers.
func (e Endpoints) CachedServerEntry() string {
	n := e.Normalize()
	name := n.FSDServerName
	if name == "" {
		name = "OPENFSD"
	}
	addr := n.FSDAddress()
	if addr == "" {
		return name + "|"
	}
	return fmt.Sprintf("%s|%s", name, addr)
}
