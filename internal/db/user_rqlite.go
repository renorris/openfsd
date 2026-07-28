package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// RqliteUserRepository implements UserRepository over rqlite HTTP.
type RqliteUserRepository struct {
	client    *RqliteClient
	readLevel ReadLevel
}

// NewRqliteUserRepository creates a user repo. readLevel applies to GetUserByCID.
func NewRqliteUserRepository(client *RqliteClient, readLevel ReadLevel) *RqliteUserRepository {
	if readLevel == "" {
		readLevel = ReadWeak
	}
	return &RqliteUserRepository{client: client, readLevel: readLevel}
}

func (r *RqliteUserRepository) CreateUser(ctx context.Context, user *User) error {
	if strings.Contains(user.Password, ":") {
		return errors.New("password cannot contain colon `:` characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// rqlite: insert then query last_insert_id via result
	res, err := r.client.Execute(ctx, `
		INSERT INTO users (password, first_name, last_name, network_rating, pilot_rating)
		VALUES (?, ?, ?, ?, ?)`,
		string(hash), user.FirstName, user.LastName, user.NetworkRating, user.PilotRating,
	)
	if err != nil {
		return err
	}
	if res.LastInsertID > 0 {
		user.CID = int(res.LastInsertID)
		return nil
	}
	// Fallback: some rqlite versions put last_insert_id differently
	q, err := r.client.Query(ctx, ReadStrong, `SELECT last_insert_rowid()`)
	if err != nil {
		return err
	}
	if s, ok := q.scanString(); ok {
		user.CID = anyToInt(s)
	}
	return nil
}

func (r *RqliteUserRepository) GetUserByCID(ctx context.Context, cid int) (*User, error) {
	res, err := r.client.Query(ctx, r.readLevel, `
		SELECT cid, password, first_name, last_name, network_rating, pilot_rating
		FROM users WHERE cid = ?`, cid)
	if err != nil {
		return nil, err
	}
	if len(res.Values) == 0 {
		return nil, sql.ErrNoRows
	}
	row := res.Values[0]
	if len(row) < 6 {
		return nil, errors.New("rqlite: unexpected user row width")
	}
	u := &User{
		CID:           anyToInt(row[0]),
		Password:      anyToString(row[1]),
		FirstName:     anyToStringPtr(row[2]),
		LastName:      anyToStringPtr(row[3]),
		NetworkRating: anyToInt(row[4]),
		PilotRating:   anyToInt(row[5]),
	}
	return u, nil
}

func (r *RqliteUserRepository) UpdateUser(ctx context.Context, user *User) error {
	var res *rqliteResult
	var err error
	if user.Password != "" {
		if strings.Contains(user.Password, ":") {
			return errors.New("password cannot contain colon `:` characters")
		}
		hash, errH := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
		if errH != nil {
			return errH
		}
		res, err = r.client.Execute(ctx, `
			UPDATE users
			SET password = ?, first_name = ?, last_name = ?, network_rating = ?, pilot_rating = ?
			WHERE cid = ?`,
			string(hash), user.FirstName, user.LastName, user.NetworkRating, user.PilotRating, user.CID,
		)
	} else {
		res, err = r.client.Execute(ctx, `
			UPDATE users
			SET first_name = ?, last_name = ?, network_rating = ?, pilot_rating = ?
			WHERE cid = ?`,
			user.FirstName, user.LastName, user.NetworkRating, user.PilotRating, user.CID,
		)
	}
	if err != nil {
		return err
	}
	if res.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *RqliteUserRepository) ListUsers(ctx context.Context, filter UserListFilter) ([]*User, error) {
	where, args := userListWhere(filter)
	limit, offset := normalizeListLimitOffset(filter)
	orderBy := userListOrderBy(filter)
	// rqlite uses ? placeholders same as sqlite
	query := `
		SELECT cid, first_name, last_name, network_rating, pilot_rating
		FROM users
		` + where + `
		` + orderBy + `
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	res, err := r.client.Query(ctx, ReadWeak, query, args...)
	if err != nil {
		return nil, err
	}
	users := make([]*User, 0, len(res.Values))
	for _, row := range res.Values {
		if len(row) < 5 {
			continue
		}
		u := &User{
			CID:           anyToInt(row[0]),
			FirstName:     anyToStringPtr(row[1]),
			LastName:      anyToStringPtr(row[2]),
			NetworkRating: anyToInt(row[3]),
			PilotRating:   anyToInt(row[4]),
		}
		users = append(users, u)
	}
	return users, nil
}

func (r *RqliteUserRepository) CountUsers(ctx context.Context, filter UserListFilter) (int, error) {
	where, args := userListWhere(filter)
	query := `SELECT COUNT(*) FROM users ` + where
	res, err := r.client.Query(ctx, ReadWeak, query, args...)
	if err != nil {
		return 0, err
	}
	if s, ok := res.scanString(); ok {
		return anyToInt(s), nil
	}
	return 0, nil
}

func (r *RqliteUserRepository) VerifyPasswordHash(plaintext, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

func (r *RqliteUserRepository) DeleteUser(ctx context.Context, cid int) error {
	res, err := r.client.Execute(ctx, `DELETE FROM users WHERE cid = ?`, cid)
	if err != nil {
		return err
	}
	if res.RowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
