# Migrating from PostgreSQL to SQLite

openfsd is **SQLite-only**. PostgreSQL support (driver selection, repositories, and migrations) has been removed. Existing deployments that still use Postgres must convert once, then run with a SQLite file.

## What you need

- Network access to the old PostgreSQL instance (read-only is enough)
- A place to write a new SQLite file (the tool will not overwrite an existing path)
- A build of `openfsd-migrate-to-sqlite` from a release that includes this tool, or from source:

```bash
go build -o openfsd-migrate-to-sqlite ./cmd/openfsd-migrate-to-sqlite
```

## Steps

1. **Stop** openfsd (FSD + web) so nothing writes to either database during the copy.
2. **Back up** PostgreSQL (`pg_dump`) and keep a copy until you confirm the new server works.
3. **Run the migrator**:

```bash
./openfsd-migrate-to-sqlite \
  -from 'postgres://USER:PASSWORD@HOST:5432/DBNAME?sslmode=disable' \
  -to ./openfsd.db
```

Example success output:

```text
copied 12 user(s) and 6 config row(s)
OK: migrated PostgreSQL → ./openfsd.db
```

4. **Point openfsd at the SQLite file** and drop Postgres-only env:

| Variable | Action |
|----------|--------|
| `DATABASE_SOURCE_NAME` | Set to the new file (see recommended pragmas below) |
| `DATABASE_DRIVER` | Omit, or set to `sqlite`. Any other value (e.g. `postgres`) causes startup to fail with a pointer to this page |
| Postgres URL / secrets | Remove from compose/k8s once cut over |

Recommended DSN (file + WAL, busy timeout):

```text
/db/openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
```

Docker Compose example (matches the sample in the repo root):

```yaml
environment:
  DATABASE_SOURCE_NAME: /db/openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
  DATABASE_AUTO_MIGRATE: "true"
volumes:
  - sqlite:/db
```

5. **Start** openfsd and verify:
   - Web login with an existing CID/password still works (password hashes are copied as-is)
   - Configure Server still shows your hostname / API base URL
   - FSD clients can connect

6. Only after verification, decommission the PostgreSQL instance.

## What is copied

| Table | Notes |
|-------|--------|
| `users` | All rows; **CID preserved**; bcrypt password hashes copied without re-hashing; `first_name` / `last_name` / `network_rating` preserved. SQLite autoincrement is set past the highest CID. |
| `config` | All key/value pairs (JWT secret, welcome message, hostnames, etc.) |

Schema is created with openfsd’s built-in SQLite migrations before data is inserted. The tool does **not** modify PostgreSQL.

## Safety

- Destination path **must not exist**; the tool refuses to overwrite.
- Source is opened read/write only as a normal client; no DDL is run against Postgres.
- Re-run: delete or rename the destination SQLite file first, then run again.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `destination "…" already exists` | Move/rename the file, or choose another `-to` path |
| `select users` / connection errors | Check DSN, network, SSL mode, and that the DB still has openfsd tables |
| Login fails after migrate | Confirm you pointed `DATABASE_SOURCE_NAME` at the **new** file; confirm migrator reported non-zero user count |
| Startup: `DATABASE_DRIVER="postgres" is not supported` | Unset `DATABASE_DRIVER` or set `sqlite`; Postgres is no longer a runtime option |

## Docker note

The published image ships the main `openfsd` binary only. Build the migrator on a workstation (or a one-off container with the Go toolchain), run it against your Postgres URL, then mount the resulting `.db` into the openfsd container.

## Verification in CI / locally

The migrator package includes unit tests (100% statement coverage) and **end-to-end tests against a real ephemeral PostgreSQL** via [`embedded-postgres`](https://github.com/fergusstrange/embedded-postgres) (downloads platform binaries into a temp dir; no system Postgres package or Docker required; cleaned up after the test).

```bash
# Full suite including real Postgres E2E (first run may download Postgres binaries)
go test -count=1 -timeout 10m ./cmd/openfsd-migrate-to-sqlite/

# Skip embedded Postgres (unit tests only)
go test -short ./cmd/openfsd-migrate-to-sqlite/
```
