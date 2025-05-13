package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	migrateSqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/lib/pq"  // PostgreSQL driver
	"modernc.org/sqlite" // SQLite driver
)

//go:embed migrations
var migrationsFS embed.FS

// Migrate applies database migrations.
func Migrate(db *sql.DB) (err error) {
	var migrationPath string
	var driver database.Driver
	var dbType string

	switch db.Driver().(type) {
	case *pq.Driver:
		dbType = "postgres"
		migrationPath = "migrations/postgres"
		if driver, err = postgres.WithInstance(db, &postgres.Config{}); err != nil {
			return
		}
	case *sqlite.Driver:
		dbType = "sqlite"
		migrationPath = "migrations/sqlite"
		if driver, err = migrateSqlite.WithInstance(db, &migrateSqlite.Config{}); err != nil {
			return
		}
	default:
		return fmt.Errorf("unsupported database type")
	}

	d, err := iofs.New(migrationsFS, migrationPath)
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, dbType, driver)
	if err != nil {
		return err
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	return nil
}
