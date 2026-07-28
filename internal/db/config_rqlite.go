package db

import (
	"context"
)

// RqliteConfigRepository implements ConfigRepository over rqlite HTTP.
type RqliteConfigRepository struct {
	client    *RqliteClient
	readLevel ReadLevel
}

// NewRqliteConfigRepository creates a config repo.
func NewRqliteConfigRepository(client *RqliteClient, readLevel ReadLevel) *RqliteConfigRepository {
	if readLevel == "" {
		readLevel = ReadWeak
	}
	return &RqliteConfigRepository{client: client, readLevel: readLevel}
}

func (r *RqliteConfigRepository) Set(ctx context.Context, key, value string) error {
	_, err := r.client.Execute(ctx, `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (r *RqliteConfigRepository) SetIfNotExists(ctx context.Context, key, value string) error {
	_, err := r.client.Execute(ctx, `
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO NOTHING`,
		key, value,
	)
	return err
}

func (r *RqliteConfigRepository) Get(ctx context.Context, key string) (string, error) {
	res, err := r.client.Query(ctx, r.readLevel, `SELECT value FROM config WHERE key = ?`, key)
	if err != nil {
		return "", err
	}
	if s, ok := res.scanString(); ok {
		return s, nil
	}
	return "", ErrConfigKeyNotFound
}
