package database

import (
	"context"
	"database/sql"
	"errors"
	"github.com/renorris/openfsd/protocol"
	"golang.org/x/crypto/bcrypt"
	"time"
)

type FSDUserRecord struct {
	CID           int                    `json:"cid"`
	Email         string                 `json:"email"`
	FirstName     string                 `json:"first_name"`
	LastName      string                 `json:"last_name"`
	Password      string                 `json:"password,omitempty"`
	FSDPassword   string                 `json:"fsd_password,omitempty"`
	NetworkRating protocol.NetworkRating `json:"network_rating"`
	PilotRating   protocol.PilotRating   `json:"pilot_rating"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

func noRowsChangedError() error { return errors.New("no rows changed") }

var NoRowsChangedError = noRowsChangedError()

// Update updates this record in the database `db`.
// CID is immutable: it must reference the account to update.
// All values must be set except for password and fsd_password, which are optional.
// Automatically hashes provided passwords, which must be in plaintext.
func (r *FSDUserRecord) Update(db *sql.DB) (err error) {
	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()

	var stmt *sql.Stmt
	// Prepare the initial statement
	if stmt, err = db.PrepareContext(ctx, "UPDATE users SET email=?, first_name=?, last_name=?, network_rating=?, pilot_rating=? WHERE cid=?"); err != nil {
		return err
	}

	if _, err = stmt.ExecContext(ctx, r.Email, r.FirstName, r.LastName, int(r.NetworkRating), r.PilotRating, r.CID); err != nil {
		return err
	}

	if err = stmt.Close(); err != nil {
		return err
	}

	// Update passwords if necessary
	if r.Password != "" {
		var passwordHash []byte
		if passwordHash, err = bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.MinCost); err != nil {
			return err
		}

		if stmt, err = db.PrepareContext(ctx, "UPDATE users SET password=? WHERE cid=?"); err != nil {
			return err
		}

		if _, err = stmt.ExecContext(ctx, string(passwordHash), r.CID); err != nil {
			return err
		}

		if err = stmt.Close(); err != nil {
			return err
		}
	}

	if r.FSDPassword != "" {
		var fsdPasswordHash []byte
		if fsdPasswordHash, err = bcrypt.GenerateFromPassword([]byte(r.FSDPassword), bcrypt.MinCost); err != nil {
			return err
		}

		if stmt, err = db.PrepareContext(ctx, "UPDATE users SET fsd_password=? WHERE cid=?"); err != nil {
			return err
		}

		if _, err = stmt.ExecContext(ctx, string(fsdPasswordHash), r.CID); err != nil {
			return err
		}

		if err = stmt.Close(); err != nil {
			return err
		}
	}

	return nil
}

// LoadByCID loads a user with the primary key `cid` from the database `db`
// Returns sql.ErrNoRows when no record matches the provided `cid`
func (r *FSDUserRecord) LoadByCID(db *sql.DB, cid int) error {
	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()

	var stmt *sql.Stmt
	var err error
	// Prepare the statement
	if stmt, err = db.PrepareContext(ctx, "SELECT * FROM users WHERE cid=? LIMIT 1"); err != nil {
		return err
	}

	var record FSDUserRecord
	if err = stmt.QueryRowContext(ctx, cid).Scan(&record.CID, &record.Email, &record.FirstName,
		&record.LastName, &record.Password, &record.FSDPassword, &record.NetworkRating,
		&record.PilotRating, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return err
	}

	if err = stmt.Close(); err != nil {
		return err
	}

	// Copy record into receiver
	*r = record

	return nil
}

// Insert inserts this user record into the database `db`
// Automatically hashes provided passwords.
// Received FSDUserRecord should contain the plaintext passwords to hash.
// Returns the automatically assigned CID
func (r *FSDUserRecord) Insert(db *sql.DB) (cid int, err error) {
	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()

	// hash the passwords
	var passwordHash []byte
	if passwordHash, err = bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.MinCost); err != nil {
		return -1, err
	}

	var fsdPasswordHash []byte
	if fsdPasswordHash, err = bcrypt.GenerateFromPassword([]byte(r.FSDPassword), bcrypt.MinCost); err != nil {
		return -1, err
	}

	var stmt *sql.Stmt
	// Prepare the statement
	if stmt, err = db.PrepareContext(ctx, "INSERT INTO users (email, first_name, last_name, password, fsd_password, network_rating, pilot_rating) VALUES (?, ?, ?, ?, ?, ?, ?)"); err != nil {
		return -1, err
	}

	var res sql.Result
	if res, err = stmt.ExecContext(ctx, r.Email, r.FirstName, r.LastName, string(passwordHash), string(fsdPasswordHash), int(r.NetworkRating), int(r.PilotRating)); err != nil {
		return -1, err
	}

	if err = stmt.Close(); err != nil {
		return -1, err
	}

	var cid64 int64
	if cid64, err = res.LastInsertId(); err != nil {
		return -1, err
	}

	return int(cid64), nil
}

// Delete deletes this user record from the database `db`
// Returns NoRowsChangedError when nothing is deleted
func (r *FSDUserRecord) Delete(db *sql.DB, cid int) (err error) {
	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()

	var stmt *sql.Stmt
	// Prepare the statement
	if stmt, err = db.PrepareContext(ctx, "DELETE FROM users WHERE cid=?"); err != nil {
		return err
	}

	var res sql.Result
	if res, err = stmt.ExecContext(ctx, cid); err != nil {
		return err
	}

	if err = stmt.Close(); err != nil {
		return err
	}

	if rows, err := res.RowsAffected(); err != nil {
		return err
	} else if rows != 1 {
		return NoRowsChangedError
	}

	return nil
}
