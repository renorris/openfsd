package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// Repositories bundles all repository interfaces.
type Repositories struct {
	UserRepo   UserRepository
	ConfigRepo ConfigRepository
}

// RequireDatabaseDriver accepts "", "sqlite", or "rqlite".
// Empty and "sqlite" are the default single-node path.
func RequireDatabaseDriver(driver string) error {
	d := strings.TrimSpace(strings.ToLower(driver))
	switch d {
	case "", "sqlite", "rqlite":
		return nil
	default:
		return fmt.Errorf(
			"DATABASE_DRIVER=%q is not supported: use sqlite (default) or rqlite; "+
				"legacy PostgreSQL: use openfsd-migrate-to-sqlite first "+
				"(see wiki/Migrating-from-PostgreSQL.md)",
			driver,
		)
	}
}

// RequireSQLiteDriver is deprecated; use RequireDatabaseDriver.
// Kept for any external callers; rejects non-sqlite (including rqlite).
func RequireSQLiteDriver(driver string) error {
	d := strings.TrimSpace(strings.ToLower(driver))
	if d == "" || d == "sqlite" {
		return nil
	}
	return fmt.Errorf(
		"DATABASE_DRIVER=%q is not supported: openfsd is SQLite-only on this path; "+
			"use DATABASE_DRIVER=rqlite with RequireDatabaseDriver for multi-node",
		driver,
	)
}

// NewUserRepository creates a UserRepository for the given database.
// Only SQLite (modernc.org/sqlite) is supported on this constructor.
func NewUserRepository(db *sql.DB) (UserRepository, error) {
	switch db.Driver().(type) {
	case *sqlite.Driver:
		return &SQLiteUserRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("unsupported database: only sqlite is supported")
	}
}

// NewConfigRepository creates a ConfigRepository for the given database.
// Only SQLite (modernc.org/sqlite) is supported on this constructor.
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

// ReadLevel is the rqlite read consistency level.
type ReadLevel string

const (
	ReadStrong ReadLevel = "strong"
	ReadWeak   ReadLevel = "weak"
	ReadNone   ReadLevel = "none"
)

// ParseReadLevel accepts strong|weak|none (case-insensitive). Empty → weak.
func ParseReadLevel(s string) (ReadLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "weak":
		return ReadWeak, nil
	case "strong", "linearizable":
		return ReadStrong, nil
	case "none":
		return ReadNone, nil
	default:
		return "", fmt.Errorf("invalid AUTH_READ_LEVEL / read level %q (want strong|weak|none)", s)
	}
}

// OpenRepositories opens sqlite or rqlite repositories from driver + DSN.
// For sqlite, dsn is a file DSN. For rqlite, dsn is an HTTP base URL
// (e.g. http://127.0.0.1:4001).
// authReadLevel applies to rqlite GetUserByCID reads (default weak).
// With caches=true, wraps user and config repos in required process caches.
func OpenRepositories(ctx context.Context, driver, dsn string, authReadLevel ReadLevel, wrapCaches bool) (*Repositories, error) {
	d := strings.TrimSpace(strings.ToLower(driver))
	if d == "" {
		d = "sqlite"
	}
	if err := RequireDatabaseDriver(d); err != nil {
		return nil, err
	}

	var repos *Repositories
	var err error
	switch d {
	case "sqlite":
		sqlDB, errOpen := sql.Open("sqlite", dsn)
		if errOpen != nil {
			return nil, errOpen
		}
		if err = sqlDB.PingContext(ctx); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		repos, err = NewRepositories(sqlDB)
	case "rqlite":
		if authReadLevel == "" {
			authReadLevel = ReadWeak
		}
		client := NewRqliteClient(dsn, RqliteClientOptions{
			Timeout: 5 * time.Second,
		})
		if err = client.Ping(ctx); err != nil {
			return nil, fmt.Errorf("rqlite ping: %w", err)
		}
		repos = &Repositories{
			UserRepo:   NewRqliteUserRepository(client, authReadLevel),
			ConfigRepo: NewRqliteConfigRepository(client, ReadWeak),
		}
	default:
		return nil, fmt.Errorf("unsupported driver %q", d)
	}
	if err != nil {
		return nil, err
	}
	if wrapCaches {
		repos.UserRepo = NewUserCache(repos.UserRepo, 0)
		repos.ConfigRepo = NewConfigCache(repos.ConfigRepo, 0)
	}
	return repos, nil
}
