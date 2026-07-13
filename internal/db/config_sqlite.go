package db

import (
	"database/sql"
	"errors"
)

type SQLiteConfigRepository struct {
	db *sql.DB
}

func (s *SQLiteConfigRepository) SetIfNotExists(key string, value string) (err error) {
	querystr := `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO NOTHING;
	`
	if _, err = s.db.Exec(querystr, key, value); err != nil {
		return
	}
	return
}

func (s *SQLiteConfigRepository) Set(key string, value string) (err error) {
	querystr := `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value;
	`
	if _, err = s.db.Exec(querystr, key, value); err != nil {
		return
	}
	return
}

func (s *SQLiteConfigRepository) Get(key string) (value string, err error) {
	querystr := `
		SELECT value FROM config WHERE key = ?;
	`
	if err = s.db.QueryRow(querystr, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConfigKeyNotFound
		}
		return
	}
	return
}
