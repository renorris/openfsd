package db

import (
	"database/sql"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

// Repositories bundles all repository interfaces.
type Repositories struct {
	UserRepo   UserRepository
	ConfigRepo ConfigRepository
}

// RequireSQLiteDriver rejects non-sqlite DATABASE_DRIVER values so operators
// migrating from older openfsd releases get a clear error instead of a cryptic
// sql.Open failure. Empty and "sqlite" are accepted.
func RequireSQLiteDriver(driver string) error {
	d := strings.TrimSpace(strings.ToLower(driver))
	if d == "" || d == "sqlite" {
		return nil
	}
	return fmt.Errorf(
		"DATABASE_DRIVER=%q is not supported: openfsd is SQLite-only; "+
			"use openfsd-migrate-to-sqlite to convert a PostgreSQL database "+
			"(see wiki/Migrating-from-PostgreSQL.md)",
		driver,
	)
}

// NewUserRepository creates a UserRepository for the given database.
// Only SQLite (modernc.org/sqlite) is supported.
func NewUserRepository(db *sql.DB) (UserRepository, error) {
	switch db.Driver().(type) {
	case *sqlite.Driver:
		return &SQLiteUserRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("unsupported database: only sqlite is supported")
	}
}

// NewConfigRepository creates a ConfigRepository for the given database.
// Only SQLite (modernc.org/sqlite) is supported.
func NewConfigRepository(db *sql.DB) (ConfigRepository, error) {
	switch db.Driver().(type) {
	case *sqlite.Driver:
		return &SQLiteConfigRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("unsupported database: only sqlite is supported")
	}
}

// NewRepositories creates a Repositories bundle with implementations for the given database.
func NewRepositories(db *sql.DB) (repositories *Repositories, err error) {
	repositories = &Repositories{}
	if repositories.UserRepo, err = NewUserRepository(db); err != nil {
		return
	}
	if repositories.ConfigRepo, err = NewConfigRepository(db); err != nil {
		return
	}
	return
}
