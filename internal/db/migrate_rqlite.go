package db

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// MigrateRqlite applies embedded .up.sql migrations over rqlite HTTP.
// Only one process should run this (DATABASE_MIGRATE_LEADER=true or -migrate).
// Uses schema_migrations(version INTEGER PRIMARY KEY, dirty INTEGER).
func MigrateRqlite(ctx context.Context, client *RqliteClient) error {
	if client == nil {
		return fmt.Errorf("MigrateRqlite: nil client")
	}
	// Ensure migrations table.
	if _, err := client.Execute(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			dirty INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Check dirty.
	res, err := client.Query(ctx, ReadStrong, `SELECT version, dirty FROM schema_migrations WHERE dirty = 1 LIMIT 1`)
	if err != nil {
		return err
	}
	if len(res.Values) > 0 {
		return fmt.Errorf("rqlite schema_migrations dirty; refuse to migrate (ops repair required)")
	}

	applied := map[int]bool{}
	res, err = client.Query(ctx, ReadStrong, `SELECT version FROM schema_migrations WHERE dirty = 0`)
	if err != nil {
		return err
	}
	for _, row := range res.Values {
		if len(row) > 0 {
			applied[anyToInt(row[0])] = true
		}
	}

	ups, err := listUpMigrations()
	if err != nil {
		return err
	}
	for _, m := range ups {
		if applied[m.version] {
			continue
		}
		// Mark dirty.
		if _, err := client.Execute(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES (?, 1)`, m.version); err != nil {
			return fmt.Errorf("mark dirty %d: %w", m.version, err)
		}
		// Apply statements (split on semicolons at line boundaries roughly).
		stmts := splitSQLStatements(m.sql)
		for _, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := client.Execute(ctx, stmt); err != nil {
				return fmt.Errorf("migrate version %d: %w", m.version, err)
			}
		}
		// Clear dirty.
		if _, err := client.Execute(ctx, `UPDATE schema_migrations SET dirty = 0 WHERE version = ?`, m.version); err != nil {
			return fmt.Errorf("clear dirty %d: %w", m.version, err)
		}
	}
	return nil
}

type migFile struct {
	version int
	name    string
	sql     string
}

func listUpMigrations() ([]migFile, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		// version is leading digits
		base := strings.TrimSuffix(name, ".up.sql")
		verStr := strings.SplitN(base, "_", 2)[0]
		ver, err := strconv.ParseInt(verStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration name %q: %w", name, err)
		}
		raw, err := fs.ReadFile(migrationsFS, path.Join("migrations", name))
		if err != nil {
			return nil, err
		}
		out = append(out, migFile{version: int(ver), name: name, sql: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func splitSQLStatements(sql string) []string {
	// Simple split on ';' — migrations are simple CREATE TABLE statements.
	parts := strings.Split(sql, ";")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "--") {
			// strip pure-comment chunks
			lines := strings.Split(p, "\n")
			var keep []string
			for _, ln := range lines {
				t := strings.TrimSpace(ln)
				if t == "" || strings.HasPrefix(t, "--") {
					continue
				}
				keep = append(keep, ln)
			}
			p = strings.TrimSpace(strings.Join(keep, "\n"))
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
