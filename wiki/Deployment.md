# Deployment

openfsd is a **single binary** (`openfsd`) that can run:

1. The FSD server (TCP protocol + internal service HTTP)
2. The web UI and `/api/v1`

By default both run in one process. Use CLI flags to select services.

Storage is **SQLite only** (a file on disk, or `:memory:` for ephemeral use). If you are upgrading from a PostgreSQL deployment, see [Migrating from PostgreSQL](Migrating-from-PostgreSQL.md).

## Docker Compose (recommended)

A sample compose file is in the repository root. It runs one container, one SQLite volume, and publishes FSD (`6809`) and web (`8000`).

### Quick start

```bash
git clone https://github.com/renorris/openfsd.git
cd openfsd
docker compose up -d
# or: docker compose up -d --build
```

1. Open `http://localhost:8000`
2. Log in with the default admin credentials (printed in container logs on first startup)
3. **Configure Server** — see [Configuration](Configuration.md)
4. Connect a client — [Client Connection](Client-Connection.md)

Images: **`ghcr.io/renorris/openfsd`** (`:latest`, `:dev`, `sha-*`) published by CI on pushes to `main` and `dev`.

### Service selection

```bash
# Both (default CMD)
docker run --rm -p 6809:6809 -p 8000:8000 ghcr.io/renorris/openfsd:latest

# FSD only
docker run --rm -p 6809:6809 ghcr.io/renorris/openfsd:latest /openfsd -fsd

# Web only (remote FSD)
docker run --rm -p 8000:8000 \
  -e FSD_HTTP_SERVICE_ADDRESS=http://fsd-host:13618 \
  ghcr.io/renorris/openfsd:latest /openfsd -web
```

Back up the SQLite volume (or the `.db` file) regularly.

## Manual / bare metal

```bash
go build -o openfsd ./cmd/openfsd

export DATABASE_SOURCE_NAME=./openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
export DATABASE_AUTO_MIGRATE=true

./openfsd              # both FSD and web
./openfsd -fsd         # FSD only
./openfsd -web         # web only
```

See [Configuration](Configuration.md) for the full environment list.

## Windows

### Option 1: Docker Desktop

Install Docker Desktop, clone the repository, then from the repo root:

```bash
docker compose up -d
```

### Option 2: Batch file

1. Install the [Go Programming Language](https://go.dev/dl/)
2. [Download](https://github.com/renorris/openfsd/archive/refs/heads/main.zip) the repository, unzip, open the folder, run `run-windows.bat`
3. Wait for downloads and startup
4. Find **DEFAULT ADMINISTRATOR CREDENTIALS** in the console output
5. Open [http://localhost:8000](http://localhost:8000) and log in

To reset the database, delete the created `openfsd.db` file next to `run-windows.bat`.

## Kubernetes and multi-host

- Prefer a single pod/process with a persistent volume for the SQLite file for small/medium networks.
- If you split `-fsd` and `-web` across hosts, set `FSD_HTTP_SERVICE_ADDRESS` on the web process to the FSD internal service HTTP address (default `http://127.0.0.1:13618` when colocated).
- Do not put two writers on the same SQLite file over NFS without understanding SQLite locking limits; local disk or a block volume is preferred.
