# openfsd

[![license](https://img.shields.io/github/license/renorris/openfsd)](https://github.com/renorris/openfsd/blob/main/LICENSE)

**openfsd** is an open-source multiplayer flight simulation server implementing the modern VATSIM FSD protocol. It connects pilots and air traffic controllers in a shared virtual environment.

## About

Flight Sim Daemon (colloquially known as FSD) is the software/protocol responsible for connecting home flight simulator clients to a single, shared multiplayer world on hobbyist networks such as [VATSIM](https://vatsim.net/docs/about/about-vatsim) and [IVAO](https://www.ivao.aero/).
FSD was originally written in the late 90's by [Marty Bochane](https://github.com/kuroneko/fsd) for [SATCO](https://web.archive.org/web/20000619145015/http://www.satco.org/), later to be forked and taken closed-source by VATSIM in 2001.
As of May 2025, FSD is still used to facilitate over 140,000 active members connecting their flight simulators to the [network](https://vatsim-radar.com/).

## Features

- Multiplayer flight simulation with VATSIM protocol compatibility
- Web-based management for users, settings, and connections
- SQLite for persistent storage (single file; easy backups)
- **Single binary** — FSD and web share one process and one database; enable services with CLI flags

## Package layout

```
cmd/openfsd/          # Binary entrypoint (FSD + web; image CMD is /openfsd)
pkg/protocol/         # Pure wire format (parse/marshal; no I/O)
pkg/fsdclient/        # Mock/real FSD client for e2e and tools
pkg/twrfiles/         # Pure .apt/.air parse + format (sweatbox + editor)
internal/server/      # TCP accept, login, handlers, service HTTP
internal/session/     # Per-connection state + outbound send worker
internal/postoffice/  # Callsign registry + geospatial index
internal/geo/         # Pure haversine / bounding box
internal/auth/        # JWT + VATSIM client auth
internal/metar/       # METAR worker pool (injectable HTTP)
internal/db/          # Shared repositories + migrations
internal/serviceapi/  # Pure JSON DTOs for FSD service HTTP (shared by server + web)
internal/web/         # Gin MPA + /api/v1
internal/sweatbox/    # Ground/taxi sim (stdlib + geo)
cmd/aptdat2apt/       # Optional: convert XP12 apt.dat → sweatbox .apt (user-fetched data)
```

### Airport layout data (sweatbox)

openfsd **does not ship** X-Plane Global Airports `apt.dat` or bulk generated
airport files. Operators fetch layout data themselves and convert locally:

```bash
go run ./cmd/aptdat2apt -download -out generated-apt -quiet
```

Sources, licensing (Gateway / Global Airports GPLv2), and packaging rules:
[docs/xplane-airport-data.md](docs/xplane-airport-data.md).

### Airport editor (web)

Administrators can author paired **`.apt`** (geometry) and **`.air`** (scenario
aircraft) files in the browser at **`/airport-editor`**:

- Map-first editing (Leaflet; JS required for the map; text paste + echo-download works without JS)
- **No server persistence** of APT/AIR — open/download only (Blob download with JS; form echo-download without)
- Validate tab: live client parse + soft cross-file warnings; optional **Confirm with server**
  (`POST /api/v1/editor/validate-apt` and `validate-air`)
- Workflow: download files → load them on **`/sweatbox`** (editor never pushes into a live session)

Design notes: [docs/design/apt-air-editor.md](docs/design/apt-air-editor.md).

## Build and run

```bash
go build -o openfsd ./cmd/openfsd

./openfsd              # both FSD and web (default)
./openfsd -fsd         # FSD only (:6809 + service HTTP :13618)
./openfsd -web         # web only (:8000)
```

| Flag | Effect |
|------|--------|
| *(none)* | Both services |
| `-fsd` | FSD only |
| `-web` | Web only |
| `-fsd -web` | Both (same as default) |

### Environment

| Variable | Default | Notes |
|----------|---------|-------|
| `DATABASE_SOURCE_NAME` | `openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` | SQLite path (preferred) or `:memory:`; must be shared by both services. Bare `:memory:` is rewritten to a process-shared DSN when FSD+web run together. |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` (default) or `rqlite`; Postgres removed (use migrate tools) |
| `DATABASE_AUTO_MIGRATE` | `true` | FSD and web apply migrations on startup (idempotent) |
| `FSD_LISTEN_ADDRS` | `:6809` | FSD TCP listen address(es) |
| `SERVICE_HTTP_LISTEN_ADDR` | `:13618` | Internal FSD admin HTTP |
| `FSD_HTTP_SERVICE_ADDRESS` | `http://127.0.0.1:13618` | Web → FSD service HTTP |
| `LISTEN_ADDR` | `:8000` | Web UI + `/api/v1` |
| `LOG_DEBUG` | *(unset)* | Set `true` for slog debug logging (default is info / release) |
| `GIN_MODE` | `release` | Gin mode for web + FSD service HTTP; set `debug` for Gin debug output |

Colocated mode (default) uses the shared DB and in-process service HTTP. For `-web` against a remote FSD, set `FSD_HTTP_SERVICE_ADDRESS`.

## Quick start (Docker)

Preferred for operators. See the [Deployment Wiki](https://github.com/renorris/openfsd/wiki/Deployment) (source: [`wiki/`](wiki/)).

Images: **`ghcr.io/renorris/openfsd`** (`:latest`, `:dev`, `sha-*`) published by CI on every push to `main` and `dev`.

**Upgrading from PostgreSQL?** Use [`openfsd-migrate-to-sqlite`](cmd/openfsd-migrate-to-sqlite) and [Migrating from PostgreSQL](wiki/Migrating-from-PostgreSQL.md). Optional multi-node: rqlite + mesh (`CLUSTER_ENABLED`, see [wiki/Deployment](wiki/Deployment.md) and `docs/design/distributed-openfsd.md`).

```bash
git clone https://github.com/renorris/openfsd.git
cd openfsd
docker compose up -d          # pull/build single image; both services
# or: docker compose up -d --build
```

1. Open `http://localhost:8000`
2. Log in with the default admin credentials (printed in container logs on first startup)
3. **Configure Server** — see the [Configuration](https://github.com/renorris/openfsd/wiki/Configuration) wiki
4. Connect a client — [Client Connection Wiki](https://github.com/renorris/openfsd/wiki/Client-Connection)

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

### Local smoke

```bash
docker compose up -d --build
curl -fsS -o /dev/null -w "%{http_code}\n" http://localhost:8000/login
nc -z localhost 6809 && echo "fsd:6809 open"
docker compose down
```

## Tests

```bash
go test -race ./...
bash scripts/check-coverage.sh 80    # overall ≥80%; pure-pkg floors (see AGENTS.md)
bash scripts/check-webjs.sh          # Node ≥20 unit tests for airport-editor JS modules
go test -bench=. -benchmem ./internal/postoffice/ ./pkg/protocol/
go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
```

E2E: `internal/server/e2e_test.go` via `pkg/fsdclient` + `StartTestServer`. Stress is optional (CI schedule / `workflow_dispatch`).

## API

`/api/v1` covers auth, users, config, and FSD connections. See [internal/web](https://github.com/renorris/openfsd/tree/main/internal/web).

## Protocol docs

Unofficial reverse-engineered FSD protocol docs live under `docs/`:

```bash
pip install mkdocs
mkdocs serve
```
