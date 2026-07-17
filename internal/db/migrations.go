package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migrateSqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"modernc.org/sqlite" // SQLite driver (side-effect: registers "sqlite")
)

//go:embed migrations
var migrationsFS embed.FS

// Migrate applies database migrations. Only SQLite is supported.
func Migrate(db *sql.DB) (err error) {
	var driver database.Driver
	switch db.Driver().(type) {
	case *sqlite.Driver:
		driver, err = migrateSqlite.WithInstance(db, &migrateSqlite.Config{})
	default:
		return fmt.Errorf("unsupported database type: only sqlite is supported")
	}
	if err != nil {
		return err
	}

	d, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, "sqlite", driver)
	if err != nil {
		return err
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	return nil
}
