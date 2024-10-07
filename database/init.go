package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"github.com/renorris/openfsd/protocol"
	"io"
	"log"
	"time"
)

const createUsersTableStatement = `
CREATE TABLE IF NOT EXISTS users (
	cid INT PRIMARY KEY AUTO_INCREMENT,
	email VARCHAR(255),
	first_name VARCHAR(255),
	last_name VARCHAR(255),
	password CHAR(60) NOT NULL,
	fsd_password CHAR(60) NOT NULL,
	network_rating INT NOT NULL,
	pilot_rating INT NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
)
`

// Initialize initializes a database `db`
func Initialize(db *sql.DB) error {
	if err := initializeUserTable(db); err != nil {
		return err
	}

	return nil
}

func initializeUserTable(db *sql.DB) error {
	ctx, cancelCtx := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelCtx()

	// Execute create users table if not exists
	var err error
	if _, err = db.ExecContext(ctx, createUsersTableStatement); err != nil {
		return err
	}

	// Set auto_increment to the initial CID value
	if _, err = db.ExecContext(ctx, "ALTER TABLE users AUTO_INCREMENT 100000"); err != nil {
		return err
	}

	// Check if the table is empty
	var emptyCheckStmt *sql.Stmt
	if emptyCheckStmt, err = db.PrepareContext(ctx, "SELECT EXISTS (SELECT 1 FROM users)"); err != nil {
		return err
	}

	var exists int64
	if err = emptyCheckStmt.QueryRowContext(ctx).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		// Generate a default user if the table is empty
		randBytes := make([]byte, 32)
		if _, err = io.ReadFull(rand.Reader, randBytes); err != nil {
			return err
		}

		primaryPassword := hex.EncodeToString(randBytes[0:16])
		fsdPassword := hex.EncodeToString(randBytes[16:32])

		defaultUser := FSDUserRecord{
			FirstName:     "Default Administrator",
			Password:      primaryPassword,
			FSDPassword:   fsdPassword,
			NetworkRating: protocol.NetworkRatingADM,
			PilotRating:   protocol.PilotRatingFE,
		}

		var cid int
		if cid, err = defaultUser.Insert(db); err != nil {
			return err
		}

		log.Printf(`

        DEFAULT ADMINISTRATOR USER:
        CID:              %d
        PRIMARY PASSWORD: %s
        FSD PASSWORD:     %s

`, cid, primaryPassword, fsdPassword)

		var testRecord FSDUserRecord
		if err = testRecord.LoadByCID(db, cid); err != nil {
			return err
		}
	}

	return nil
}
