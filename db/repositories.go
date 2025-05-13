package db

import (
	"database/sql"
	"fmt"
	"github.com/lib/pq"
	"modernc.org/sqlite"
)

// Repositories bundles all repository interfaces
type Repositories struct {
	UserRepo UserRepository
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

// NewRepositories creates a Repositories bundle with implementations for the given database
func NewRepositories(db *sql.DB) (*Repositories, error) {
	userRepo, err := NewUserRepository(db)
	if err != nil {
		return nil, err
	}

	return &Repositories{
		UserRepo: userRepo,
	}, nil
}
