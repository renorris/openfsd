package db

import (
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

type SQLiteUserRepository struct {
	db *sql.DB
}

func (r *SQLiteUserRepository) CreateUser(user *User) (err error) {
	// Password must not contain colon characters
	if strings.Contains(user.Password, ":") {
		err = errors.New("password cannot contain colon `:` characters")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return
	}

	row := r.db.QueryRow(`
		INSERT INTO users
		(password, first_name, last_name, network_rating)
		VALUES 
		(?, ?, ?, ?)
		RETURNING cid`,
		hash, user.FirstName, user.LastName, user.NetworkRating,
	)
	if err = row.Err(); err != nil {
		return
	}

	if err = row.Scan(&user.CID); err != nil {
		return
	}

	return
}

func (r *SQLiteUserRepository) GetUserByCID(cid int) (user *User, err error) {
	row := r.db.QueryRow(`
		SELECT 
		cid, password, first_name, 
		last_name, network_rating
		FROM users
		WHERE cid = $1`,
		cid,
	)
	if err = row.Err(); err != nil {
		return
	}

	user = &User{}
	if err = row.Scan(
		&user.CID,
		&user.Password,
		&user.FirstName,
		&user.LastName,
		&user.NetworkRating,
	); err != nil {
		return
	}

	return
}

func (r *SQLiteUserRepository) UpdateUser(user *User) (err error) {
	// Prepare query and arguments based on whether password is provided
	var query string
	var args []interface{}

	if user.Password != "" {
		// Check if password contains colon characters
		if strings.Contains(user.Password, ":") {
			return errors.New("password cannot contain colon `:` characters")
		}

		// Hash the password
		hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		// Include password in update
		query = `
			UPDATE users
			SET password = ?, first_name = ?, last_name = ?, network_rating = ?
			WHERE cid = ?`
		args = []interface{}{hash, user.FirstName, user.LastName, user.NetworkRating, user.CID}
	} else {
		// Exclude password from update
		query = `
			UPDATE users
			SET first_name = ?, last_name = ?, network_rating = ?
			WHERE cid = ?`
		args = []interface{}{user.FirstName, user.LastName, user.NetworkRating, user.CID}
	}

	// Execute the UPDATE statement
	result, err := r.db.Exec(query, args...)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *SQLiteUserRepository) VerifyPasswordHash(plaintext string, hash string) (ok bool) {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
