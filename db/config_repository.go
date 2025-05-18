package db

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

type ConfigRepository interface {
	// Set sets a value for a given key
	Set(key string, value string) (err error)

	// SetIfNotExists sets a value for a given key if it does not already exist
	SetIfNotExists(key string, value string) (err error)

	// Get gets a value for a given key.
	//
	// Returns ErrConfigKeyNotFound if no key/value pair is found.
	Get(key string) (value string, err error)
}

const (
	ConfigJwtSecretKey = "JWT_SECRET_KEY"

	ConfigFsdServerHostname = "FSD_SERVER_HOSTNAME"
	ConfigFsdServerIdent    = "FSD_SERVER_IDENT"
	ConfigFsdServerLocation = "FSD_SERVER_LOCATION"

	ConfigApiServerBaseURL = "API_SERVER_BASE_URL"

	ConfigWelcomeMessage = "WELCOME_MESSAGE"
)

var ErrConfigKeyNotFound = errors.New("config: key not found")

const secretKeyBits = 256

func GenerateJwtSecretKey() (key [secretKeyBits / 8]byte, err error) {
	secretKey := make([]byte, (secretKeyBits/8)/2)
	if _, err = io.ReadFull(rand.Reader, secretKey); err != nil {
		return
	}

	hex.Encode(key[:], secretKey)

	return
}

// GetWelcomeMessage returns any configured welcome message.
// Returns an empty string if no message is found.
func GetWelcomeMessage(r *ConfigRepository) (msg string) {
	msg, _ = (*r).Get(ConfigWelcomeMessage)
	return
}

func InitDefaultConfig(r *ConfigRepository) (err error) {
	secretKey, err := GenerateJwtSecretKey()
	if err != nil {
		return
	}

	defaultConfig := map[string]string{
		ConfigJwtSecretKey:      string(secretKey[:]),
		ConfigWelcomeMessage:    "Connected to openfsd",
		ConfigFsdServerHostname: "localhost",
		ConfigFsdServerIdent:    "OPENFSD",
		ConfigFsdServerLocation: "Earth",
		ConfigApiServerBaseURL:  "http://localhost",
	}

	for k, v := range defaultConfig {
		if err = (*r).SetIfNotExists(k, v); err != nil {
			return
		}
	}

	return
}
