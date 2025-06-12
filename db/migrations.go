package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migratePostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	migrateSqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/lib/pq"  // PostgreSQL driver
	"modernc.org/sqlite" // SQLite driver
)

//go:embed migrations
var migrationsFS embed.FS

// Migrate applies database migrations.
func Migrate(db *sql.DB) (err error) {
	var driver database.Driver
	var dbType string
	var migrationPath string
	switch db.Driver().(type) {
	case *pq.Driver:
		dbType = "postgres"
		migrationPath = "migrations/postgres"
		driver, err = migratePostgres.WithInstance(db, &migratePostgres.Config{})
	case *sqlite.Driver:
		dbType = "sqlite"
		migrationPath = "migrations/sqlite"
		driver, err = migrateSqlite.WithInstance(db, &migrateSqlite.Config{})
	default:
		return fmt.Errorf("unsupported database type")
	}
	if err != nil {
		return err
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
