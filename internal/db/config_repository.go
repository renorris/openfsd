package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

type ConfigRepository interface {
	// Set sets a value for a given key
	Set(ctx context.Context, key string, value string) (err error)

	// SetIfNotExists sets a value for a given key if it does not already exist
	SetIfNotExists(ctx context.Context, key string, value string) (err error)

	// Get gets a value for a given key.
	//
	// Returns ErrConfigKeyNotFound if no key/value pair is found.
	Get(ctx context.Context, key string) (value string, err error)
}

const (
	ConfigJwtSecretKey = "JWT_SECRET_KEY"

	ConfigFsdServerHostname = "FSD_SERVER_HOSTNAME"
	ConfigFsdServerIdent    = "FSD_SERVER_IDENT"
	ConfigFsdServerLocation = "FSD_SERVER_LOCATION"

	ConfigApiServerBaseURL = "API_SERVER_BASE_URL"

	ConfigWelcomeMessage = "WELCOME_MESSAGE"

	// ConfigRequirePilotPPL, when true, rejects pilot (#AP) connections unless the
	// certificate's pilot_rating is PPL (1) or higher. ATC connections are unaffected.
	// Default false (see InitDefaultConfig). Values: true/false, 1/0, yes/no.
	ConfigRequirePilotPPL = "REQUIRE_PILOT_PPL"
)

var ErrConfigKeyNotFound = errors.New("config: key not found")

// secretKeyBytes is the number of random bytes used for HS256 secrets (256-bit entropy).
const secretKeyBytes = 32

// GenerateJwtSecretKey returns a hex-encoded 256-bit secret (64 hex chars).
func GenerateJwtSecretKey() (key string, err error) {
	secretKey := make([]byte, secretKeyBytes)
	if _, err = io.ReadFull(rand.Reader, secretKey); err != nil {
		return
	}
	return hex.EncodeToString(secretKey), nil
}

// GetWelcomeMessage returns any configured welcome message.
// Returns an empty string if no message is found.
func GetWelcomeMessage(ctx context.Context, r ConfigRepository) (msg string) {
	msg, _ = r.Get(ctx, ConfigWelcomeMessage)
	return
}

// ParseBoolConfig interprets common config string booleans.
// Empty / unknown / missing → false (safe default for restrictive flags).
func ParseBoolConfig(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// RequirePilotPPL reports whether pilot connections require pilot_rating ≥ PPL.
// Missing key or parse failure → false (disabled by default).
func RequirePilotPPL(ctx context.Context, r ConfigRepository) bool {
	v, err := r.Get(ctx, ConfigRequirePilotPPL)
	if err != nil {
		return false
	}
	return ParseBoolConfig(v)
}

func InitDefaultConfig(ctx context.Context, r ConfigRepository) (err error) {
	secretKey, err := GenerateJwtSecretKey()
	if err != nil {
		return
	}

	defaultConfig := map[string]string{
		ConfigJwtSecretKey:      secretKey,
		ConfigWelcomeMessage:    "Connected to openfsd",
		ConfigFsdServerHostname: "localhost",
		ConfigFsdServerIdent:    "OPENFSD",
		ConfigFsdServerLocation: "Earth",
		ConfigApiServerBaseURL:  "http://localhost",
		ConfigRequirePilotPPL:   "false",
	}

	for k, v := range defaultConfig {
		if err = r.SetIfNotExists(ctx, k, v); err != nil {
			return
		}
	}

	return
}
