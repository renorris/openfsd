package db

import (
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

type PostgresUserRepository struct {
	db *sql.DB
}

func (r *PostgresUserRepository) CreateUser(user *User) (err error) {
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
		INSERT INTO public.users
		(password, first_name, last_name, network_rating)
		VALUES 
		($1, $2, $3, $4, $5)
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

func (r *PostgresUserRepository) GetUserByCID(cid int) (user *User, err error) {
	row := r.db.QueryRow(`
		SELECT 
		cid, password, first_name, 
		last_name, network_rating
		FROM public.users
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

func (r *PostgresUserRepository) VerifyPasswordHash(plaintext string, hash string) (ok bool) {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

func (r *PostgresUserRepository) UpdateUser(user *User) (err error) {
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
			UPDATE public.users
			SET password = $1, first_name = $2, last_name = $3, network_rating = $4
			WHERE cid = $5`
		args = []interface{}{hash, user.FirstName, user.LastName, user.NetworkRating, user.CID}
	} else {
		// Exclude password from update
		query = `
			UPDATE public.users
			SET first_name = $1, last_name = $2, network_rating = $3
			WHERE cid = $4`
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
