package db

import (
	"context"
	"database/sql"
	"errors"
)

type SQLiteConfigRepository struct {
	db *sql.DB
}

func (s *SQLiteConfigRepository) SetIfNotExists(ctx context.Context, key string, value string) (err error) {
	querystr := `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO NOTHING;
	`
	if _, err = s.db.ExecContext(ctx, querystr, key, value); err != nil {
		return
	}
	return
}

func (s *SQLiteConfigRepository) Set(ctx context.Context, key string, value string) (err error) {
	querystr := `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value;
	`
	if _, err = s.db.ExecContext(ctx, querystr, key, value); err != nil {
		return
	}
	return
}

func (s *SQLiteConfigRepository) Get(ctx context.Context, key string) (value string, err error) {
	querystr := `
		SELECT value FROM config WHERE key = ?;
	`
	if err = s.db.QueryRowContext(ctx, querystr, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConfigKeyNotFound
		}
		return
	}
	return
}
