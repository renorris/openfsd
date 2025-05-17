package db

import (
	"database/sql"
	"fmt"
	"github.com/lib/pq"
	"modernc.org/sqlite"
)

// Repositories bundles all repository interfaces
type Repositories struct {
	UserRepo   UserRepository
	ConfigRepo ConfigRepository
}

// NewUserRepository creates a UserRepository based on the database driver
func NewUserRepository(db *sql.DB) (UserRepository, error) {
	switch db.Driver().(type) {
	case *pq.Driver:
		return &PostgresUserRepository{db: db}, nil
	case *sqlite.Driver:
		return &SQLiteUserRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("unsupported database")
	}
}

// NewConfigRepository creates a ConfigRepository based on the database driver
func NewConfigRepository(db *sql.DB) (ConfigRepository, error) {
	switch db.Driver().(type) {
	case *pq.Driver:
		return &PostgresConfigRepository{db: db}, nil
	case *sqlite.Driver:
		return &SQLiteConfigRepository{db: db}, nil
	default:
		return nil, fmt.Errorf("unsupported database")
	}
}

// NewRepositories creates a Repositories bundle with implementations for the given database
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
