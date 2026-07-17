// Command openfsd-migrate-to-sqlite copies an openfsd PostgreSQL database into
// a new SQLite file. It is a one-shot operator tool for deployments that still
// run on PostgreSQL after openfsd became SQLite-only.
//
// Usage:
//
//	openfsd-migrate-to-sqlite -from 'postgres://user:pass@host:5432/openfsd?sslmode=disable' -to ./openfsd.db
//
// The destination file must not already exist. Source data is read only;
// PostgreSQL is never modified.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/renorris/openfsd/internal/db"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// Test seams for otherwise unreachable driver/OS error paths.
var (
	sqlOpen               = sql.Open
	applySQLiteMigrations = db.Migrate
	osExit                = os.Exit
	txCommit              = func(tx *sql.Tx) error { return tx.Commit() }
	txPrepare             = func(tx *sql.Tx, q string) (*sql.Stmt, error) { return tx.Prepare(q) }
	rowsErr               = func(rows *sql.Rows) error { return rows.Err() }
	seqDeleteSQL          = `DELETE FROM sqlite_sequence WHERE name = 'users'`
	seqInsertSQL          = `INSERT INTO sqlite_sequence(name, seq) VALUES ('users', ?)`
	scanConfigRow         = func(rows *sql.Rows) (key, value string, err error) {
		err = rows.Scan(&key, &value)
		return
	}
)

func main() {
	osExit(run(os.Args[1:], os.Stderr, os.Stdout))
}

// run is the testable entrypoint. Returns a process exit code.
func run(args []string, stderr, stdout io.Writer) int {
	fs := flag.NewFlagSet("openfsd-migrate-to-sqlite", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "PostgreSQL DSN (e.g. postgres://user:pass@localhost:5432/openfsd?sslmode=disable)")
	to := fs.String("to", "", "destination SQLite file path (must not already exist)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		fs.Usage()
		fmt.Fprintln(stderr, "\nboth -from and -to are required")
		return 2
	}

	if err := migrate(*from, *to, stdout); err != nil {
		fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "OK: migrated PostgreSQL → %s\n", *to)
	fmt.Fprintln(stdout, "Next: set DATABASE_SOURCE_NAME to this file (and DATABASE_DRIVER=sqlite or omit it), then start openfsd.")
	return 0
}

func migrate(pgDSN, sqlitePath string, stdout io.Writer) error {
	if err := ensureDestAbsent(sqlitePath); err != nil {
		return err
	}

	pg, err := openPostgres(pgDSN)
	if err != nil {
		return err
	}
	defer pg.Close()

	sqliteDB, err := openSQLiteDest(sqlitePath)
	if err != nil {
		return err
	}
	defer sqliteDB.Close()

	nUsers, err := copyUsers(pg, sqliteDB)
	if err != nil {
		return err
	}
	nConfig, err := copyConfig(pg, sqliteDB)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "copied %d user(s) and %d config row(s)\n", nUsers, nConfig)
	return nil
}

func ensureDestAbsent(sqlitePath string) error {
	_, err := os.Stat(sqlitePath)
	if err == nil {
		return fmt.Errorf("destination %q already exists; refuse to overwrite (move or delete it first)", sqlitePath)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination: %w", err)
	}
	return nil
}

func openPostgres(pgDSN string) (*sql.DB, error) {
	pg, err := sqlOpen("postgres", pgDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pg.Ping(); err != nil {
		_ = pg.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pg, nil
}

func openSQLiteDest(sqlitePath string) (*sql.DB, error) {
	sqliteDB, err := sqlOpen("sqlite", sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqliteDB.SetMaxOpenConns(1)
	if err := sqliteDB.Ping(); err != nil {
		_ = sqliteDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := applySQLiteMigrations(sqliteDB); err != nil {
		_ = sqliteDB.Close()
		return nil, fmt.Errorf("sqlite schema migrate: %w", err)
	}
	return sqliteDB, nil
}

// userSelect statements tried in order. public.users is the historical openfsd
// schema; the unqualified form covers non-public search_path deployments.
var userSelects = []string{
	`SELECT cid, password, first_name, last_name, network_rating
		FROM public.users
		ORDER BY cid`,
	`SELECT cid, password, first_name, last_name, network_rating
		FROM users
		ORDER BY cid`,
}

func queryUsers(pg *sql.DB) (*sql.Rows, error) {
	var lastErr error
	for _, q := range userSelects {
		rows, err := pg.Query(q)
		if err == nil {
			return rows, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("select users: %w", lastErr)
}

func copyUsers(pg, sqliteDB *sql.DB) (int, error) {
	rows, err := queryUsers(pg)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := sqliteDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin users tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := txPrepare(tx, `
		INSERT INTO users (cid, password, first_name, last_name, network_rating)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare user insert: %w", err)
	}
	defer stmt.Close()

	n := 0
	var maxCID int
	for rows.Next() {
		var (
			cid           int
			password      string
			firstName     sql.NullString
			lastName      sql.NullString
			networkRating int
		)
		if err := rows.Scan(&cid, &password, &firstName, &lastName, &networkRating); err != nil {
			return 0, fmt.Errorf("scan user: %w", err)
		}
		// Postgres char(60) may pad with spaces; trim for bcrypt hashes.
		password = strings.TrimSpace(password)

		var fn, ln any
		if firstName.Valid {
			fn = firstName.String
		}
		if lastName.Valid {
			ln = lastName.String
		}
		if _, err := stmt.Exec(cid, password, fn, ln, networkRating); err != nil {
			return 0, fmt.Errorf("insert user cid=%d: %w", cid, err)
		}
		if cid > maxCID {
			maxCID = cid
		}
		n++
	}
	if err := rowsErr(rows); err != nil {
		return 0, fmt.Errorf("iterate users: %w", err)
	}

	if err := setUsersSequence(tx, maxCID); err != nil {
		return 0, err
	}

	if err := txCommit(tx); err != nil {
		return 0, fmt.Errorf("commit users: %w", err)
	}
	return n, nil
}

func setUsersSequence(tx *sql.Tx, maxCID int) error {
	if maxCID <= 0 {
		return nil
	}
	if _, err := tx.Exec(seqDeleteSQL); err != nil {
		return fmt.Errorf("reset sqlite_sequence: %w", err)
	}
	if _, err := tx.Exec(seqInsertSQL, maxCID); err != nil {
		return fmt.Errorf("set sqlite_sequence: %w", err)
	}
	return nil
}

func copyConfig(pg, sqliteDB *sql.DB) (int, error) {
	rows, err := pg.Query(`SELECT key, value FROM config`)
	if err != nil {
		return 0, fmt.Errorf("select config: %w", err)
	}
	defer rows.Close()

	tx, err := sqliteDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin config tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := txPrepare(tx, `INSERT INTO config (key, value) VALUES (?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare config insert: %w", err)
	}
	defer stmt.Close()

	n := 0
	for rows.Next() {
		key, value, err := scanConfigRow(rows)
		if err != nil {
			return 0, fmt.Errorf("scan config: %w", err)
		}
		if _, err := stmt.Exec(key, value); err != nil {
			return 0, fmt.Errorf("insert config key=%q: %w", key, err)
		}
		n++
	}
	if err := rowsErr(rows); err != nil {
		return 0, fmt.Errorf("iterate config: %w", err)
	}
	if err := txCommit(tx); err != nil {
		return 0, fmt.Errorf("commit config: %w", err)
	}
	return n, nil
}
