package db

import (
	"database/sql"
	"errors"
)

type PostgresConfigRepository struct {
	db *sql.DB
}

// InitDefault initializes the default configuration values.
func (p *PostgresConfigRepository) InitDefault() (err error) {
	if err = p.ensureSecretKeyExists(); err != nil {
		return
	}
	return
}

func (p *PostgresConfigRepository) ensureSecretKeyExists() (err error) {
	secretKey, err := GenerateJwtSecretKey()
	if err != nil {
		return
	}

	querystr := `
		INSERT INTO config (key, value)
		SELECT $1, $2
		WHERE NOT EXISTS (
			SELECT 1 FROM config WHERE key = $1
		);`
	_, err = p.db.Exec(querystr, ConfigJwtSecretKey, secretKey)
	return
}

// Set sets the value for the given key in the configuration.
// If the key already exists, it updates the value.
func (p *PostgresConfigRepository) Set(key string, value string) (err error) {
	querystr := `
		INSERT INTO config (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
	`
	_, err = p.db.Exec(querystr, key, value)
	return
}

func (p *PostgresConfigRepository) SetIfNotExists(key string, value string) (err error) {
	querystr := `
		INSERT INTO config (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO NOTHING;
	`
	_, err = p.db.Exec(querystr, key, value)
	return
}

// Get retrieves the value for the given key from the configuration.
// If the key does not exist, it returns ErrConfigKeyNotFound.
func (p *PostgresConfigRepository) Get(key string) (value string, err error) {
	querystr := `
		SELECT value FROM config WHERE key = $1;
	`
	err = p.db.QueryRow(querystr, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConfigKeyNotFound
		}
		return
	}
	return
}
