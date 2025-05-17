package db

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

type ConfigRepository interface {
	// InitDefault initializes the default state of the Config if one does not already exist.
	InitDefault() (err error)

	// Set sets a value for a given key
	Set(key string, value string) (err error)

	// Get gets a value for a given key.
	//
	// Returns ErrConfigKeyNotFound if no key/value pair is found.
	Get(key string) (value string, err error)
}

const (
	ConfigJwtSecretKey   = "JWT_SECRET_KEY"
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
