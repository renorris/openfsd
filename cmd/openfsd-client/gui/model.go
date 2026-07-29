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

// ValidationIssue is a form-level problem before Plan.
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
		issues = append(issues, ValidationIssue{Field: "InstallPath", Message: "set install path"})
	}
	web := strings.TrimSpace(f.WebBaseURL)
	if web == "" {
		issues = append(issues, ValidationIssue{Field: "WebBaseURL", Message: "web URL required"})
	} else if _, err := url.ParseRequestURI(web); err != nil {
		issues = append(issues, ValidationIssue{Field: "WebBaseURL", Message: "invalid web URL"})
	}
	if strings.TrimSpace(f.FSDHost) == "" {
		issues = append(issues, ValidationIssue{Field: "FSDHost", Message: "FSD host required"})
	}
	if f.FSDPort < 0 || f.FSDPort > 65535 {
		issues = append(issues, ValidationIssue{Field: "FSDPort", Message: "invalid port"})
	}
	afv := strings.TrimSpace(f.AFVBaseURL)
	if afv != "" {
		if _, err := url.ParseRequestURI(afv); err != nil {
			issues = append(issues, ValidationIssue{Field: "AFVBaseURL", Message: "invalid AFV URL"})
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
	return strings.ToLower(u.Hostname())
}

// IsPublicVATSIMHost reports whether host (or WebBaseURL host) is in the known
// public VATSIM set.
func IsPublicVATSIMHost(hostOrURL string) bool {
	host := strings.ToLower(strings.TrimSpace(hostOrURL))
	if host == "" {
		return false
	}
	if strings.Contains(host, "://") || strings.Contains(host, "/") {
		host = WebBaseHost(hostOrURL)
	} else if h, _, err := net.SplitHostPort(host); err == nil {
		host = strings.ToLower(h)
	}
	_, ok := PublicVATSIMHosts[host]
	return ok
}

// CanApply reports whether Apply should proceed given form validation and plan.
func CanApply(f FormState, plan *clientinject.Plan, formIssues []ValidationIssue) (ok bool, reason string) {
	if len(formIssues) > 0 {
		return false, formIssues[0].Message
	}
	if IsPublicVATSIMHost(f.WebBaseURL) {
		return false, "use a different web URL"
	}
	if plan == nil {
		return false, "no plan yet"
	}
	if len(plan.Blockers) > 0 {
		return false, plan.Blockers[0]
	}
	return true, ""
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

// FormatPortString displays FSD port for the form.
func FormatPortString(port int) string {
	if port == 0 || port == clientinject.DefaultFSDPort {
		return strconv.Itoa(clientinject.DefaultFSDPort)
	}
	return strconv.Itoa(port)
}
