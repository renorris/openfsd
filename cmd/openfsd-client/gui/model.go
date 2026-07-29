package gui

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
)

// FormState holds user-editable Client Setup fields (no passwords).
// Client-agnostic: the same fields apply to every adapter.
type FormState struct {
	ClientID string

	InstallPath string

	WebBaseURL    string
	FSDHost       string
	FSDPort       int // 0 = default 6809
	FSDServerName string
	AFVBaseURL    string

	ForceDisableAFV    bool
	PreferShortJWTPath bool

	// UnderstandPublicVATSIM acknowledges the soft warning when WebBaseURL
	// host is in PublicVATSIMHosts. Does not store passwords.
	UnderstandPublicVATSIM bool
}

// DefaultFormState returns sensible defaults for a new session.
func DefaultFormState() FormState {
	return FormState{
		ClientID:      "vpilot",
		FSDPort:       0,
		FSDServerName: "OPENFSD",
	}
}

// Endpoints maps form fields to clientinject.Endpoints.
func (f FormState) Endpoints() clientinject.Endpoints {
	return clientinject.Endpoints{
		WebBaseURL:         strings.TrimSpace(f.WebBaseURL),
		FSDHost:            strings.TrimSpace(f.FSDHost),
		FSDPort:            f.FSDPort,
		FSDServerName:      strings.TrimSpace(f.FSDServerName),
		AFVBaseURL:         strings.TrimSpace(f.AFVBaseURL),
		ForceDisableAFV:    f.ForceDisableAFV,
		PreferShortJWTPath: f.PreferShortJWTPath,
	}.Normalize()
}

// ValidationIssue is a form-level problem before Plan (empty path, bad URL, …).
type ValidationIssue struct {
	Field   string
	Message string
}

// ValidateForm checks fields that do not need disk/engine access.
func ValidateForm(f FormState) []ValidationIssue {
	var issues []ValidationIssue
	if strings.TrimSpace(f.ClientID) == "" {
		issues = append(issues, ValidationIssue{Field: "ClientID", Message: "select a client"})
	}
	if strings.TrimSpace(f.InstallPath) == "" {
		issues = append(issues, ValidationIssue{Field: "InstallPath", Message: "install path is required"})
	}
	web := strings.TrimSpace(f.WebBaseURL)
	if web == "" {
		issues = append(issues, ValidationIssue{Field: "WebBaseURL", Message: "web base URL is required"})
	} else if _, err := url.ParseRequestURI(web); err != nil {
		issues = append(issues, ValidationIssue{Field: "WebBaseURL", Message: "web base URL is not a valid absolute URL"})
	}
	if strings.TrimSpace(f.FSDHost) == "" {
		issues = append(issues, ValidationIssue{Field: "FSDHost", Message: "FSD host is required"})
	}
	if f.FSDPort < 0 || f.FSDPort > 65535 {
		issues = append(issues, ValidationIssue{Field: "FSDPort", Message: "FSD port must be 0–65535 (0 = default 6809)"})
	}
	afv := strings.TrimSpace(f.AFVBaseURL)
	if afv != "" {
		if _, err := url.ParseRequestURI(afv); err != nil {
			issues = append(issues, ValidationIssue{Field: "AFVBaseURL", Message: "AFV base URL is not a valid absolute URL"})
		}
	}
	return issues
}

// WebBaseHost extracts the hostname from WebBaseURL (lowercased, no port).
func WebBaseHost(webBase string) string {
	webBase = strings.TrimSpace(webBase)
	if webBase == "" {
		return ""
	}
	u, err := url.Parse(webBase)
	if err != nil || u.Host == "" {
		// Fallback: treat as bare host
		host := webBase
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		host = strings.Split(host, "/")[0]
		if h, _, err := net.SplitHostPort(host); err == nil {
			return strings.ToLower(h)
		}
		return strings.ToLower(strings.TrimSpace(host))
	}
	host := u.Hostname()
	return strings.ToLower(host)
}

// IsPublicVATSIMHost reports whether host (or WebBaseURL host) is in the known
// public VATSIM set.
func IsPublicVATSIMHost(hostOrURL string) bool {
	host := strings.ToLower(strings.TrimSpace(hostOrURL))
	if host == "" {
		return false
	}
	// If looks like a URL, extract host.
	if strings.Contains(host, "://") || strings.Contains(host, "/") {
		host = WebBaseHost(hostOrURL)
	} else if h, _, err := net.SplitHostPort(host); err == nil {
		host = strings.ToLower(h)
	}
	_, ok := PublicVATSIMHosts[host]
	return ok
}

// PublicVATSIMWarning returns a non-empty warning when WebBaseURL points at a
// known public VATSIM host.
func PublicVATSIMWarning(webBase string) string {
	host := WebBaseHost(webBase)
	if host == "" || !IsPublicVATSIMHost(host) {
		return ""
	}
	return fmt.Sprintf(
		"Web base host %q is a known public VATSIM host. This tool is for private openfsd networks you are authorized to use. Check “I understand” only if that is intentional (e.g. local mirror).",
		host,
	)
}

// CanApply reports whether Apply should be enabled given form validation,
// optional public-VATSIM override, and plan blockers.
func CanApply(f FormState, plan *clientinject.Plan, formIssues []ValidationIssue) (ok bool, reason string) {
	if len(formIssues) > 0 {
		return false, formIssues[0].Message
	}
	if warn := PublicVATSIMWarning(f.WebBaseURL); warn != "" && !f.UnderstandPublicVATSIM {
		return false, "Web base looks like public VATSIM — confirm “I understand” or change the URL"
	}
	if plan == nil {
		return false, "no plan yet"
	}
	if len(plan.Blockers) > 0 {
		return false, "plan has blockers"
	}
	return true, ""
}

// FormatConstraintLine formats one Plan.Constraints entry for the panel.
func FormatConstraintLine(c clientinject.Constraint) string {
	max := "n/a"
	if c.MaxRunes > 0 {
		max = strconv.Itoa(c.MaxRunes) + " runes"
	}
	strat := c.Strategy
	if strat == "" {
		strat = "n/a"
	}
	desc := c.Description
	if desc == "" {
		return fmt.Sprintf("%s — max %s — %s", c.Field, max, strat)
	}
	return fmt.Sprintf("%s — max %s — %s — %s", c.Field, max, strat, desc)
}

// FormatConstraints joins constraint lines for the multi-line panel.
func FormatConstraints(cs []clientinject.Constraint) string {
	if len(cs) == 0 {
		return "(no constraints from plan yet)"
	}
	var b strings.Builder
	for i, c := range cs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(FormatConstraintLine(c))
	}
	return b.String()
}

// FormatMutations summarizes plan mutations for the log / preview.
func FormatMutations(ms []clientinject.Mutation) string {
	if len(ms) == 0 {
		return "(no mutations)"
	}
	var b strings.Builder
	for i, m := range ms {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "• %s [%s] %s", m.ID, m.Kind, m.Description)
	}
	return b.String()
}

// FormatBlockers formats plan blockers.
func FormatBlockers(bs []string) string {
	if len(bs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range bs {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "• %s", s)
	}
	return b.String()
}

// FormatWarnings formats plan warnings.
func FormatWarnings(ws []string) string {
	if len(ws) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range ws {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "• %s", s)
	}
	return b.String()
}

// FormatFingerprint summarizes install identity for the fingerprint panel.
func FormatFingerprint(install clientinject.Install, profileID string, preflightErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Client: %s\n", install.ClientID)
	fmt.Fprintf(&b, "Root: %s\n", install.RootDir)
	fmt.Fprintf(&b, "Primary PE: %s\n", install.PrimaryPE)
	if install.HashSHA1 != "" {
		fmt.Fprintf(&b, "SHA-1: %s\n", install.HashSHA1)
	} else {
		b.WriteString("SHA-1: (not resolved)\n")
	}
	if profileID != "" {
		fmt.Fprintf(&b, "Profile: %s\n", profileID)
	} else if install.ProfileID != "" {
		fmt.Fprintf(&b, "Profile: %s\n", install.ProfileID)
	} else {
		b.WriteString("Profile: (unresolved)\n")
	}
	if len(install.ConfigPaths) > 0 {
		fmt.Fprintf(&b, "Config files:\n")
		for _, p := range install.ConfigPaths {
			fmt.Fprintf(&b, "  • %s\n", p)
		}
	} else {
		b.WriteString("Config files: (none discovered)\n")
	}
	if preflightErr != nil {
		fmt.Fprintf(&b, "\n⚠ Client appears to be running — quit completely before Apply.\n(%v)\n", preflightErr)
	} else {
		b.WriteString("\nPreflight: PE not locked (or path empty).\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ParsePortString parses FSD port entry; empty or "6809" → 0 (default).
func ParsePortString(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	if n < 0 || n > 65535 {
		return 0, fmt.Errorf("port out of range: %d", n)
	}
	if n == clientinject.DefaultFSDPort {
		return 0, nil
	}
	return n, nil
}

// FormatPortString displays FSD port for the form (empty means default).
func FormatPortString(port int) string {
	if port == 0 || port == clientinject.DefaultFSDPort {
		return strconv.Itoa(clientinject.DefaultFSDPort)
	}
	return strconv.Itoa(port)
}
