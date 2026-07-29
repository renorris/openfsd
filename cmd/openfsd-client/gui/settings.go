package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Settings is the persisted user preferences (paths + endpoints only).
// Never store passwords or network credentials.
// UnderstandPublicVATSIM is intentionally NOT persisted — every session must
// re-acknowledge a public VATSIM WebBaseURL soft warning.
type Settings struct {
	ClientID           string `json:"client_id,omitempty"`
	InstallPath        string `json:"install_path,omitempty"`
	WebBaseURL         string `json:"web_base_url,omitempty"`
	FSDHost            string `json:"fsd_host,omitempty"`
	FSDPort            int    `json:"fsd_port,omitempty"`
	FSDServerName      string `json:"fsd_server_name,omitempty"`
	AFVBaseURL         string `json:"afv_base_url,omitempty"`
	ForceDisableAFV    bool   `json:"force_disable_afv,omitempty"`
	PreferShortJWTPath bool   `json:"prefer_short_jwt_path,omitempty"`
}

// SettingsFromForm copies persistable fields from FormState.
// Does not include UnderstandPublicVATSIM (session-only ack).
func SettingsFromForm(f FormState) Settings {
	return Settings{
		ClientID:           f.ClientID,
		InstallPath:        f.InstallPath,
		WebBaseURL:         f.WebBaseURL,
		FSDHost:            f.FSDHost,
		FSDPort:            f.FSDPort,
		FSDServerName:      f.FSDServerName,
		AFVBaseURL:         f.AFVBaseURL,
		ForceDisableAFV:    f.ForceDisableAFV,
		PreferShortJWTPath: f.PreferShortJWTPath,
	}
}

// ApplyToForm merges settings into form (non-empty / meaningful fields).
// Never sets UnderstandPublicVATSIM — always leave false for a fresh session.
func (s Settings) ApplyToForm(f *FormState) {
	if f == nil {
		return
	}
	if s.ClientID != "" {
		f.ClientID = s.ClientID
	}
	if s.InstallPath != "" {
		f.InstallPath = s.InstallPath
	}
	if s.WebBaseURL != "" {
		f.WebBaseURL = s.WebBaseURL
	}
	if s.FSDHost != "" {
		f.FSDHost = s.FSDHost
	}
	f.FSDPort = s.FSDPort
	if s.FSDServerName != "" {
		f.FSDServerName = s.FSDServerName
	}
	if s.AFVBaseURL != "" {
		f.AFVBaseURL = s.AFVBaseURL
	}
	f.ForceDisableAFV = s.ForceDisableAFV
	f.PreferShortJWTPath = s.PreferShortJWTPath
	f.UnderstandPublicVATSIM = false
}

// DefaultConfigDir returns OS user config dir + openfsd-client.
func DefaultConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "openfsd-client"), nil
}

// DefaultSettingsPath is configDir/settings.json.
func DefaultSettingsPath() (string, error) {
	dir, err := DefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// LoadSettings reads settings from path. Missing file returns empty Settings, nil error.
func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("gui: parse settings: %w", err)
	}
	return s, nil
}

// SaveSettings writes settings atomically-ish (write temp + rename).
func SaveSettings(path string, s Settings) error {
	if path == "" {
		return fmt.Errorf("gui: empty settings path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
