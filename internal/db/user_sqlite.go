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
		(password, first_name, last_name, network_rating, pilot_rating)
		VALUES 
		(?, ?, ?, ?, ?)
		RETURNING cid`,
		hash, user.FirstName, user.LastName, user.NetworkRating, user.PilotRating,
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
		last_name, network_rating, pilot_rating
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
		&user.PilotRating,
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
			SET password = ?, first_name = ?, last_name = ?, network_rating = ?, pilot_rating = ?
			WHERE cid = ?`
		args = []interface{}{hash, user.FirstName, user.LastName, user.NetworkRating, user.PilotRating, user.CID}
	} else {
		// Exclude password from update
		query = `
			UPDATE users
			SET first_name = ?, last_name = ?, network_rating = ?, pilot_rating = ?
			WHERE cid = ?`
		args = []interface{}{user.FirstName, user.LastName, user.NetworkRating, user.PilotRating, user.CID}
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

// escapeLike escapes \, %, and _ for use in LIKE ... ESCAPE '\' patterns.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// userListWhere builds the shared WHERE clause and args for ListUsers / CountUsers.
func userListWhere(f UserListFilter) (clause string, args []any) {
	var parts []string

	q := strings.TrimSpace(f.Query)
	if q != "" {
		like := "%" + escapeLike(q) + "%"
		parts = append(parts, `(
			CAST(cid AS TEXT) LIKE ? ESCAPE '\'
			OR IFNULL(first_name,'') LIKE ? ESCAPE '\'
			OR IFNULL(last_name,'') LIKE ? ESCAPE '\'
			OR TRIM(IFNULL(first_name,'') || ' ' || IFNULL(last_name,'')) LIKE ? ESCAPE '\'
		)`)
		args = append(args, like, like, like, like)
	}

	if f.Rating != nil {
		parts = append(parts, `network_rating = ?`)
		args = append(args, *f.Rating)
	}

	if len(parts) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(parts, " AND "), args
}

func normalizeListLimitOffset(f UserListFilter) (limit, offset int) {
	limit = f.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	offset = f.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func userListOrderBy(f UserListFilter) string {
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	switch f.Sort {
	case "name":
		return "ORDER BY lower(IFNULL(last_name,'')) " + dir +
			", lower(IFNULL(first_name,'')) " + dir +
			", cid ASC"
	case "rating":
		return "ORDER BY network_rating " + dir + ", cid ASC"
	default: // "cid" and any invalid sort
		return "ORDER BY cid " + dir
	}
}

func (r *SQLiteUserRepository) ListUsers(filter UserListFilter) ([]*User, error) {
	where, args := userListWhere(filter)
	limit, offset := normalizeListLimitOffset(filter)
	orderBy := userListOrderBy(filter)

	query := `
		SELECT cid, first_name, last_name, network_rating, pilot_rating
		FROM users
		` + where + `
		` + orderBy + `
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{} // Password left empty — not selected
		if err := rows.Scan(&u.CID, &u.FirstName, &u.LastName, &u.NetworkRating, &u.PilotRating); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if users == nil {
		users = []*User{}
	}
	return users, nil
}

func (r *SQLiteUserRepository) CountUsers(filter UserListFilter) (int, error) {
	where, args := userListWhere(filter)
	query := `SELECT COUNT(*) FROM users ` + where
	var n int
	if err := r.db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
