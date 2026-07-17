package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/db"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func TestMainCoversEntrypoint(t *testing.T) {
	var code int
	origExit := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = origExit })

	origArgs := os.Args
	os.Args = []string{"openfsd-migrate-to-sqlite"} // missing -from/-to
	t.Cleanup(func() { os.Args = origArgs })

	main()
	require.Equal(t, 2, code)
}

func TestOpenSQLOpenFailures(t *testing.T) {
	orig := sqlOpen
	t.Cleanup(func() { sqlOpen = orig })
	sqlOpen = func(driverName, dataSourceName string) (*sql.DB, error) {
		return nil, errors.New("open denied")
	}
	_, err := openPostgres("postgres://x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "open postgres")

	_, err = openSQLiteDest(filepath.Join(t.TempDir(), "x.db"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "open sqlite")
}

func TestOpenSQLiteMigrateFailure(t *testing.T) {
	orig := applySQLiteMigrations
	t.Cleanup(func() { applySQLiteMigrations = orig })
	applySQLiteMigrations = func(*sql.DB) error { return errors.New("migrate boom") }
	_, err := openSQLiteDest(filepath.Join(t.TempDir(), "x.db"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "sqlite schema migrate")
}

func TestCopyUsersQueryFails(t *testing.T) {
	src, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })
	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "select users")
}

func TestCopyUsersScanFails(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	// network_rating NULL cannot scan into int.
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER,
			password TEXT,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER
		);
		INSERT INTO users VALUES (1, 'h', NULL, NULL, NULL);
	`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "scan user")
}

func TestCopyUsersIterateAndCommitFails(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER NOT NULL
		);
		INSERT INTO users VALUES (1, 'h', NULL, NULL, 1);
	`)
	require.NoError(t, err)
	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	origRows := rowsErr
	rowsErr = func(*sql.Rows) error { return errors.New("rows boom") }
	_, err = copyUsers(src, dest)
	rowsErr = origRows
	require.Error(t, err)
	require.Contains(t, err.Error(), "iterate users")

	origCommit := txCommit
	txCommit = func(*sql.Tx) error { return errors.New("commit boom") }
	t.Cleanup(func() { txCommit = origCommit })
	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "commit users")
}

func TestCopyUsersSequenceFails(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER NOT NULL
		);
		INSERT INTO users VALUES (1, 'h', NULL, NULL, 1);
	`)
	require.NoError(t, err)
	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	orig := seqInsertSQL
	seqInsertSQL = `INSERT INTO no_such_sequence_table VALUES (?)`
	t.Cleanup(func() { seqInsertSQL = orig })
	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "set sqlite_sequence")
}

func TestSetUsersSequenceInsertFails(t *testing.T) {
	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })
	tx, err := dest.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	orig := seqInsertSQL
	seqInsertSQL = `INSERT INTO no_such_table(name, seq) VALUES ('users', ?)`
	t.Cleanup(func() { seqInsertSQL = orig })
	err = setUsersSequence(tx, 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "set sqlite_sequence")
}

func TestCopyConfigScanAndIterateAndCommitFails(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`CREATE TABLE config (key TEXT, value TEXT); INSERT INTO config VALUES ('a','b');`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	origScan := scanConfigRow
	scanConfigRow = func(*sql.Rows) (string, string, error) {
		return "", "", errors.New("scan boom")
	}
	_, err = copyConfig(src, dest)
	scanConfigRow = origScan
	require.Error(t, err)
	require.Contains(t, err.Error(), "scan config")

	origRows := rowsErr
	rowsErr = func(*sql.Rows) error { return errors.New("cfg rows") }
	_, err = copyConfig(src, dest)
	rowsErr = origRows
	require.Error(t, err)
	require.Contains(t, err.Error(), "iterate config")

	origCommit := txCommit
	txCommit = func(*sql.Tx) error { return errors.New("cfg commit") }
	t.Cleanup(func() { txCommit = origCommit })
	_, err = copyConfig(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "commit config")
}

func TestPrepareFailuresViaSeam(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER NOT NULL
		);
		INSERT INTO users VALUES (1, 'h', NULL, NULL, 1);
		CREATE TABLE config (key TEXT, value TEXT);
		INSERT INTO config VALUES ('k','v');
	`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	orig := txPrepare
	txPrepare = func(*sql.Tx, string) (*sql.Stmt, error) {
		return nil, errors.New("prepare denied")
	}
	t.Cleanup(func() { txPrepare = orig })

	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prepare user insert")

	_, err = copyConfig(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prepare config insert")
}

func TestRunMissingFlags(t *testing.T) {
	var stderr, stdout bytes.Buffer
	code := run(nil, &stderr, &stdout)
	require.Equal(t, 2, code)
	require.Contains(t, stderr.String(), "both -from and -to are required")

	stderr.Reset()
	code = run([]string{"-from", "postgres://x"}, &stderr, &stdout)
	require.Equal(t, 2, code)

	stderr.Reset()
	code = run([]string{"-to", "out.db"}, &stderr, &stdout)
	require.Equal(t, 2, code)
}

func TestRunBadFlag(t *testing.T) {
	var stderr, stdout bytes.Buffer
	code := run([]string{"-nope"}, &stderr, &stdout)
	require.Equal(t, 2, code)
}

func TestRunMigrateFailure(t *testing.T) {
	var stderr, stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "out.db")
	code := run([]string{"-from", "postgres://nope:nope@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1", "-to", path}, &stderr, &stdout)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "migrate:")
}

func TestEnsureDestAbsent(t *testing.T) {
	require.NoError(t, ensureDestAbsent(filepath.Join(t.TempDir(), "missing.db")))

	path := filepath.Join(t.TempDir(), "exists.db")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	err := ensureDestAbsent(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	// Stat error that is not IsNotExist: path component is a file, not a directory.
	parent := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(parent, []byte("x"), 0o600))
	err = ensureDestAbsent(filepath.Join(parent, "child.db"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "stat destination")
}

func TestMigrateRefusesExistingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exists.db")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	err := migrate("postgres://invalid", path, ioDiscard{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

func TestOpenPostgresPingFailure(t *testing.T) {
	_, err := openPostgres("postgres://nope:nope@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ping postgres")
}

func TestOpenSQLiteDest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.db")
	sqlDB, err := openSQLiteDest(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	var n int
	require.NoError(t, sqlDB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n))
	require.Zero(t, n)
}

func TestOpenSQLiteDestBadPath(t *testing.T) {
	// Parent does not exist → SQLite open/ping fails.
	path := filepath.Join(t.TempDir(), "no-such-dir", "nested", "out.db")
	_, err := openSQLiteDest(path)
	require.Error(t, err)
}

func TestQueryUsersNoTable(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, err = queryUsers(sqlDB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "select users")
}

func TestSetUsersSequence(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), "dest.db")
	dest, err := openSQLiteDest(destPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	tx, err := dest.Begin()
	require.NoError(t, err)
	// maxCID <= 0 is a no-op (empty migration).
	require.NoError(t, setUsersSequence(tx, 0))
	require.NoError(t, setUsersSequence(tx, 42))
	require.NoError(t, tx.Commit())

	var seq int
	require.NoError(t, dest.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'users'`).Scan(&seq))
	require.Equal(t, 42, seq)
}

func TestCopyConfigMissingTable(t *testing.T) {
	src, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	_, err = copyConfig(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "select config")
}

func TestCopyUsersInsertConflict(t *testing.T) {
	// Build a fake source that returns user rows via a real SQLite table named
	// to match the fallback SELECT ... FROM users (after public.users fails).
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db")+"?_pragma=busy_timeout(1000)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER NOT NULL
		)`)
	require.NoError(t, err)
	_, err = src.Exec(`INSERT INTO users VALUES (1, 'hash', 'A', 'B', 1), (1, 'x', NULL, NULL, 2)`)
	// SQLite rejects duplicate PK on second insert — seed two distinct then
	// force conflict on destination instead.
	require.Error(t, err)
	_, err = src.Exec(`DELETE FROM users`)
	require.NoError(t, err)
	_, err = src.Exec(`INSERT INTO users VALUES (1, 'hash1', 'A', 'B', 1), (2, 'hash2', NULL, NULL, 2)`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })
	// Pre-seed destination CID 1 so second insert of CID 1 fails… source has 1 and 2.
	// Pre-seed CID 2:
	_, err = dest.Exec(`INSERT INTO users (cid, password, first_name, last_name, network_rating) VALUES (2, 'x', NULL, NULL, 1)`)
	require.NoError(t, err)

	// public.users fails on SQLite → fallback to users → insert hits unique conflict on cid=2
	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insert user")
}

func TestCopyUsersAndConfigFromSQLiteFallback(t *testing.T) {
	// Proves the unqualified FROM users fallback and full copy path without Postgres.
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER NOT NULL
		);
		CREATE TABLE config (key TEXT NOT NULL, value TEXT NOT NULL);
	`)
	require.NoError(t, err)

	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	require.NoError(t, err)
	// Simulate char(60) padding from Postgres.
	padded := hash
	for len(padded) < 60 {
		padded = append(padded, ' ')
	}
	_, err = src.Exec(`INSERT INTO users VALUES (?, ?, 'Ada', 'Lovelace', 12)`, 7, string(padded))
	require.NoError(t, err)
	_, err = src.Exec(`INSERT INTO users VALUES (3, '  bare  ', NULL, NULL, 1)`)
	require.NoError(t, err)
	_, err = src.Exec(`INSERT INTO config VALUES ('WELCOME_MESSAGE', 'hello'), ('JWT_SECRET_KEY', 'deadbeef')`)
	require.NoError(t, err)

	destPath := filepath.Join(t.TempDir(), "out.db")
	dest, err := openSQLiteDest(destPath)
	require.NoError(t, err)

	nUsers, err := copyUsers(src, dest)
	require.NoError(t, err)
	require.Equal(t, 2, nUsers)
	nConfig, err := copyConfig(src, dest)
	require.NoError(t, err)
	require.Equal(t, 2, nConfig)
	require.NoError(t, dest.Close())

	// Re-open and assert
	check, err := sql.Open("sqlite", destPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = check.Close() })

	var (
		cid           int
		password      string
		first, last   sql.NullString
		networkRating int
	)
	require.NoError(t, check.QueryRow(`SELECT cid, password, first_name, last_name, network_rating FROM users WHERE cid = 7`).
		Scan(&cid, &password, &first, &last, &networkRating))
	require.Equal(t, 7, cid)
	require.True(t, first.Valid)
	require.Equal(t, "Ada", first.String)
	require.Equal(t, "Lovelace", last.String)
	require.Equal(t, 12, networkRating)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(password), []byte("s3cret")))
	require.False(t, strings.HasSuffix(password, " "), "password must be trimmed")

	require.NoError(t, check.QueryRow(`SELECT password, first_name FROM users WHERE cid = 3`).Scan(&password, &first))
	require.Equal(t, "bare", password)
	require.False(t, first.Valid)

	var seq int
	require.NoError(t, check.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'users'`).Scan(&seq))
	require.Equal(t, 7, seq)

	res, err := check.Exec(`INSERT INTO users (password, first_name, last_name, network_rating) VALUES ('z', 'x', 'y', 1)`)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	require.Equal(t, int64(8), id)

	var welcome string
	require.NoError(t, check.QueryRow(`SELECT value FROM config WHERE key = 'WELCOME_MESSAGE'`).Scan(&welcome))
	require.Equal(t, "hello", welcome)
}

func TestCopyConfigDuplicateKey(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`CREATE TABLE config (key TEXT, value TEXT)`)
	require.NoError(t, err)
	// SQLite allows duplicate keys without unique index — insert two same keys.
	_, err = src.Exec(`INSERT INTO config VALUES ('K', '1'), ('K', '2')`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })

	_, err = copyConfig(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insert config")
}

func TestCopyUsersClosedDest(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`
		CREATE TABLE users (
			cid INTEGER PRIMARY KEY,
			password TEXT NOT NULL,
			first_name TEXT,
			last_name TEXT,
			network_rating INTEGER NOT NULL
		);
		INSERT INTO users VALUES (1, 'h', NULL, NULL, 1);
	`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	require.NoError(t, dest.Close())

	_, err = copyUsers(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "begin users tx")
}

func TestCopyConfigClosedDest(t *testing.T) {
	src, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "src.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	_, err = src.Exec(`CREATE TABLE config (key TEXT, value TEXT); INSERT INTO config VALUES ('a','b');`)
	require.NoError(t, err)

	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	require.NoError(t, dest.Close())

	_, err = copyConfig(src, dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "begin config tx")
}

func TestSetUsersSequenceClosedTx(t *testing.T) {
	dest, err := openSQLiteDest(filepath.Join(t.TempDir(), "d.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dest.Close() })
	tx, err := dest.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	err = setUsersSequence(tx, 9)
	require.Error(t, err)
}

func TestOpenSQLiteDestDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-file")
	require.NoError(t, os.Mkdir(dir, 0o755))
	_, err := openSQLiteDest(dir)
	require.Error(t, err)
}

func TestMigratePingPostgresFailure(t *testing.T) {
	err := migrate(
		"postgres://nope:nope@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1",
		filepath.Join(t.TempDir(), "out.db"),
		ioDiscard{},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ping postgres")
}

func TestSQLiteDestinationAcceptsMigratorShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.db")
	sqlDB, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.Migrate(sqlDB))

	_, err = sqlDB.Exec(`
		INSERT INTO users (cid, password, first_name, last_name, network_rating)
		VALUES (1, '$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012', 'Ada', 'Lovelace', 12),
		       (5, '$2a$10$abcdefghijklmnopqrstuuABCDEFGHIJKLMNOPQRSTUVWXYZ012', NULL, NULL, 1)`)
	require.NoError(t, err)

	tx, err := sqlDB.Begin()
	require.NoError(t, err)
	require.NoError(t, setUsersSequence(tx, 5))
	require.NoError(t, tx.Commit())

	res, err := sqlDB.Exec(`
		INSERT INTO users (password, first_name, last_name, network_rating)
		VALUES ('x', 'a', 'b', 1)`)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	require.Equal(t, int64(6), id)
}

// ioDiscard is a tiny io.Writer for tests.
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
