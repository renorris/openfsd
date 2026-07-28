package main

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/renorris/openfsd/internal/db"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

//go:embed legacy_pg_schema.sql
var legacyPGSchema string

// TestE2E_PostgresToSQLite starts a real ephemeral PostgreSQL (no system
// install; binaries downloaded by embedded-postgres into a temp dir), seeds it
// with the historical openfsd schema + data, runs the migrator end-to-end, and
// proves the SQLite result is correct. Postgres is stopped via t.Cleanup; the
// runtime directory lives under t.TempDir so it is removed automatically.
func TestE2E_PostgresToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded Postgres E2E in -short mode")
	}

	pgDSN := startEmbeddedPostgres(t)

	pg, err := sql.Open("postgres", pgDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })
	require.NoError(t, pg.Ping())

	// Apply historical openfsd Postgres schema exactly as main-branch deploys had it.
	_, err = pg.Exec(legacyPGSchema)
	require.NoError(t, err, "apply legacy postgres schema")

	// Realistic bcrypt hash; char(60) will right-pad in Postgres.
	const plainPassword = "admin-pass-测试"
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.MinCost)
	require.NoError(t, err)
	require.LessOrEqual(t, len(hash), 60)

	// Explicit CIDs with a gap; NULL names; admin rating.
	_, err = pg.Exec(`
		INSERT INTO public.users (cid, password, first_name, last_name, network_rating) VALUES
			(1, $1, 'Default', 'Admin', 12),
			(2, $1, NULL, NULL, 1),
			(10, $1, 'Gap', 'User', 2)`, string(hash))
	require.NoError(t, err)

	_, err = pg.Exec(`SELECT setval('public.users_cid_seq', 10)`)
	require.NoError(t, err)

	_, err = pg.Exec(`
		INSERT INTO config (key, value) VALUES
			('JWT_SECRET_KEY', '0123456789abcdef0123456789abcdef'),
			('WELCOME_MESSAGE', 'Connected to openfsd'),
			('FSD_SERVER_HOSTNAME', 'fsd.example.test'),
			('FSD_SERVER_IDENT', 'OPENFSD'),
			('FSD_SERVER_LOCATION', 'Earth'),
			('API_SERVER_BASE_URL', 'https://api.example.test')`)
	require.NoError(t, err)

	// char(60) pads short values; bcrypt is already 60 chars so insert a short
	// sentinel on CID 2 to prove the migrator trims padding.
	_, err = pg.Exec(`UPDATE public.users SET password = 'short' WHERE cid = 2`)
	require.NoError(t, err)
	var rawPW string
	require.NoError(t, pg.QueryRow(`SELECT password FROM public.users WHERE cid = 2`).Scan(&rawPW))
	require.Equal(t, 60, len(rawPW), "postgres char(60) stores fixed width")
	require.Equal(t, "short", strings.TrimSpace(rawPW))

	outPath := filepath.Join(t.TempDir(), "openfsd-migrated.db")

	// Full CLI path: run() → migrate() → copyUsers/copyConfig
	var stdout, stderr bytes.Buffer
	code := run([]string{"-from", pgDSN, "-to", outPath}, &stderr, &stdout)
	require.Equal(t, 0, code, "stderr=%s stdout=%s", stderr.String(), stdout.String())
	require.Contains(t, stdout.String(), "copied 3 user(s) and 6 config row(s)")
	require.Contains(t, stdout.String(), "OK: migrated PostgreSQL")
	require.FileExists(t, outPath)

	// Source Postgres must be unchanged (read-only migrate).
	var pgUsers, pgConfig int
	require.NoError(t, pg.QueryRow(`SELECT COUNT(*) FROM public.users`).Scan(&pgUsers))
	require.NoError(t, pg.QueryRow(`SELECT COUNT(*) FROM config`).Scan(&pgConfig))
	require.Equal(t, 3, pgUsers)
	require.Equal(t, 6, pgConfig)

	// Verify SQLite contents.
	sqliteDB, err := sql.Open("sqlite", outPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	var nUsers, nConfig int
	require.NoError(t, sqliteDB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&nUsers))
	require.NoError(t, sqliteDB.QueryRow(`SELECT COUNT(*) FROM config`).Scan(&nConfig))
	require.Equal(t, 3, nUsers)
	require.Equal(t, 6, nConfig)

	type userRow struct {
		cid           int
		password      string
		first, last   sql.NullString
		networkRating int
	}
	rows, err := sqliteDB.Query(`SELECT cid, password, first_name, last_name, network_rating FROM users ORDER BY cid`)
	require.NoError(t, err)
	defer rows.Close()

	var got []userRow
	for rows.Next() {
		var r userRow
		require.NoError(t, rows.Scan(&r.cid, &r.password, &r.first, &r.last, &r.networkRating))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 3)

	require.Equal(t, 1, got[0].cid)
	require.Equal(t, "Default", got[0].first.String)
	require.Equal(t, "Admin", got[0].last.String)
	require.Equal(t, 12, got[0].networkRating)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(got[0].password), []byte(plainPassword)))
	require.Equal(t, len(strings.TrimSpace(got[0].password)), len(got[0].password))

	require.Equal(t, 2, got[1].cid)
	require.False(t, got[1].first.Valid)
	require.False(t, got[1].last.Valid)
	require.Equal(t, 1, got[1].networkRating)
	require.Equal(t, "short", got[1].password, "char(60) padding must be trimmed")

	require.Equal(t, 10, got[2].cid)
	require.Equal(t, "Gap", got[2].first.String)

	// AUTOINCREMENT continues after highest migrated CID
	var seq int
	require.NoError(t, sqliteDB.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'users'`).Scan(&seq))
	require.Equal(t, 10, seq)
	res, err := sqliteDB.Exec(`INSERT INTO users (password, first_name, last_name, network_rating) VALUES ('x', 'n', 'ew', 1)`)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	require.Equal(t, int64(11), id)

	// Config key/values round-trip
	wantConfig := map[string]string{
		"JWT_SECRET_KEY":      "0123456789abcdef0123456789abcdef",
		"WELCOME_MESSAGE":     "Connected to openfsd",
		"FSD_SERVER_HOSTNAME": "fsd.example.test",
		"FSD_SERVER_IDENT":    "OPENFSD",
		"FSD_SERVER_LOCATION": "Earth",
		"API_SERVER_BASE_URL": "https://api.example.test",
	}
	for k, v := range wantConfig {
		var gotVal string
		require.NoError(t, sqliteDB.QueryRow(`SELECT value FROM config WHERE key = ?`, k).Scan(&gotVal), k)
		require.Equal(t, v, gotVal, k)
	}

	// Production repositories accept the migrated file (login + config paths).
	repos, err := db.NewRepositories(sqliteDB)
	require.NoError(t, err)
	u, err := repos.UserRepo.GetUserByCID(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, repos.UserRepo.VerifyPasswordHash(plainPassword, u.Password))
	msg, err := repos.ConfigRepo.Get(context.Background(), "WELCOME_MESSAGE")
	require.NoError(t, err)
	require.Equal(t, "Connected to openfsd", msg)

	// Second migrate to same path must refuse overwrite.
	var stdout2, stderr2 bytes.Buffer
	code = run([]string{"-from", pgDSN, "-to", outPath}, &stderr2, &stdout2)
	require.Equal(t, 1, code)
	require.Contains(t, stderr2.String(), "already exists")

	// migrate() error path: destination directory missing after Postgres opens.
	err = migrate(pgDSN, filepath.Join(t.TempDir(), "no-such-dir", "x.db"), io.Discard)
	require.Error(t, err)

	// migrate() error path: copyUsers fails when users table is missing.
	_, err = pg.Exec(`DROP TABLE public.users CASCADE`)
	require.NoError(t, err)
	err = migrate(pgDSN, filepath.Join(t.TempDir(), "nousers.db"), io.Discard)
	require.Error(t, err)
	require.Contains(t, err.Error(), "select users")
}

// TestE2E_MigrateCopyConfigFails exercises migrate() when users copy succeeds
// but config is missing on the Postgres source.
func TestE2E_MigrateCopyConfigFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded Postgres E2E in -short mode")
	}
	pgDSN := startEmbeddedPostgres(t)
	pg, err := sql.Open("postgres", pgDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })
	_, err = pg.Exec(`
		CREATE TABLE public.users (
			cid SERIAL PRIMARY KEY,
			password CHAR(60) NOT NULL,
			first_name VARCHAR(255),
			last_name VARCHAR(255),
			network_rating SMALLINT NOT NULL
		);
		INSERT INTO public.users (password, first_name, last_name, network_rating)
		VALUES ('x', NULL, NULL, 1);
		-- deliberately no config table
	`)
	require.NoError(t, err)
	err = migrate(pgDSN, filepath.Join(t.TempDir(), "noconfig.db"), io.Discard)
	require.Error(t, err)
	require.Contains(t, err.Error(), "select config")
}

// TestE2E_PostgresEmptyTables migrates a schema-only Postgres DB (0 rows).
func TestE2E_PostgresEmptyTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded Postgres E2E in -short mode")
	}

	pgDSN := startEmbeddedPostgres(t)

	pg, err := sql.Open("postgres", pgDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })
	_, err = pg.Exec(legacyPGSchema)
	require.NoError(t, err)

	outPath := filepath.Join(t.TempDir(), "empty.db")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-from", pgDSN, "-to", outPath}, &stderr, &stdout)
	require.Equal(t, 0, code, stderr.String())
	require.Contains(t, stdout.String(), "copied 0 user(s) and 0 config row(s)")

	sqliteDB, err := sql.Open("sqlite", outPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })
	var n int
	require.NoError(t, sqliteDB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n))
	require.Zero(t, n)
}

// TestE2E_PostgresUsersFallbackSchema proves the unqualified "FROM users"
// fallback when public.users is absent but search_path exposes users.
func TestE2E_PostgresUsersFallbackSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded Postgres E2E in -short mode")
	}

	pgDSN := startEmbeddedPostgres(t)

	pg, err := sql.Open("postgres", pgDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Close() })

	_, err = pg.Exec(`
		CREATE SCHEMA app;
		CREATE TABLE app.users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating SMALLINT NOT NULL
		);
		CREATE TABLE public.config (
			key VARCHAR NOT NULL,
			value VARCHAR NOT NULL
		);
		CREATE UNIQUE INDEX config_key_uindex ON public.config (key);
		INSERT INTO app.users VALUES (5, 'hash', 'X', 'Y', 3);
		INSERT INTO public.config VALUES ('K', 'V');
	`)
	require.NoError(t, err)

	// Keyword DSN (not URL): append options so unqualified "users" resolves to
	// app.users. public.users is missing → first SELECT fails → fallback.
	dsn := pgDSN + " options='-csearch_path=app,public'"

	outPath := filepath.Join(t.TempDir(), "fallback.db")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-from", dsn, "-to", outPath}, &stderr, &stdout)
	require.Equal(t, 0, code, "stderr=%s stdout=%s", stderr.String(), stdout.String())
	require.Contains(t, stdout.String(), "copied 1 user(s) and 1 config row(s)")

	sqliteDB, err := sql.Open("sqlite", outPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })
	var cid int
	var pw string
	require.NoError(t, sqliteDB.QueryRow(`SELECT cid, password FROM users`).Scan(&cid, &pw))
	require.Equal(t, 5, cid)
	require.Equal(t, "hash", pw)
}

// startEmbeddedPostgres boots a real PostgreSQL in a temp directory using
// github.com/fergusstrange/embedded-postgres (downloads platform binaries on
// first use; no system Postgres package required). Stop + wipe via t.Cleanup.
func startEmbeddedPostgres(t *testing.T) (dsn string) {
	t.Helper()

	port := freeTCPPort(t)
	runtimePath := t.TempDir()
	dataPath := filepath.Join(runtimePath, "data")

	const (
		user = "openfsd"
		pass = "openfsd"
		name = "openfsd"
	)

	cfg := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Username(user).
		Password(pass).
		Database(name).
		RuntimePath(runtimePath).
		DataPath(dataPath).
		Port(uint32(port)).
		StartTimeout(90 * time.Second).
		Logger(io.Discard)

	database := embeddedpostgres.NewDatabase(cfg)
	err := database.Start()
	require.NoError(t, err, "start embedded postgres (downloads binaries on first run)")

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if err := database.Stop(); err != nil {
			t.Logf("embedded postgres stop: %v", err)
		}
	}
	t.Cleanup(stop)

	dsn = fmt.Sprintf(
		"host=127.0.0.1 port=%d user=%s password=%s dbname=%s sslmode=disable",
		port, user, pass, name,
	)

	// Wait until accepting connections (Start usually does this, belt-and-suspenders).
	deadline := time.Now().Add(30 * time.Second)
	for {
		db, err := sql.Open("postgres", dsn)
		if err == nil {
			pingErr := db.Ping()
			_ = db.Close()
			if pingErr == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("embedded postgres not ready: last err=%v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	return dsn
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}
