# openfsd

[![license](https://img.shields.io/github/license/renorris/openfsd)](https://github.com/renorris/openfsd/blob/main/LICENSE)

**openfsd** is an open-source multiplayer flight simulation server implementing the modern VATSIM FSD protocol. It connects pilots and air traffic controllers in a shared virtual environment.

## About

Flight Sim Daemon (colloquially known as FSD) is the software/protocol responsible for connecting home flight simulator clients to a single, shared multiplayer world on hobbyist networks such as [VATSIM](https://vatsim.net/docs/about/about-vatsim) and [IVAO](https://www.ivao.aero/).
FSD was originally written in the late 90's by [Marty Bochane](https://github.com/kuroneko/fsd) for [SATCO](https://web.archive.org/web/20000619145015/http://www.satco.org/), later to be forked and taken closed-source by VATSIM in 2001.
As of May 2025, FSD is still used to facilitate over 140,000 active members connecting their flight simulators to the [network](https://vatsim-radar.com/).

## Features

- Multiplayer flight simulation with VATSIM protocol compatibility
- Web-based management: users, config, live connections, account self-service
- SQLite for persistent storage (single file; easy backups); optional **rqlite** for multi-node durable state
- **Single binary** — FSD, web, and optional AFV share one process and one database; enable services with CLI flags
- **Sweatbox** ground/taxi simulator + map-first **airport editor** (`.apt` / `.air`)
- **Operator REST** under `/api/v1` with date microversions, discovery, and embedded OpenAPI
- **Optional AFV** voice server (`-afv`: REST + UDP CryptoDTO) for TrackAudio / xPilot-shaped clients
- **Optional FSD cluster** (rqlite + TCP mesh) for multi-edge deployments

## Package layout

```
cmd/openfsd/                 # Binary entrypoint (FSD + web; -afv opt-in)
cmd/openfsd-migrate-to-sqlite  # Postgres → SQLite row copy
cmd/openfsd-migrate-to-rqlite  # SQLite file → rqlite HTTP row copy
cmd/aptdat2apt/               # Optional: XP12 apt.dat → sweatbox .apt
pkg/protocol/                # Pure FSD wire format (parse/marshal; no I/O)
pkg/fsdclient/               # Mock/real FSD client for e2e and tools
pkg/twrfiles/                # Pure .apt/.air parse + format
pkg/afvprotocol/             # Pure AFV CryptoDTO + DTOs + AEAD
internal/server/             # TCP accept, login, handlers, service HTTP, FSD mesh
internal/session/            # Per-connection state + outbound send worker
internal/postoffice/         # Callsign registry + geospatial index
internal/geo/                # Pure haversine / bounding box
internal/auth/               # JWT + VATSIM client auth
internal/metar/              # METAR worker pool (injectable HTTP)
internal/db/                 # Shared repositories + migrations (sqlite | rqlite)
internal/serviceapi/         # Pure JSON DTOs for FSD service HTTP
internal/web/                # Gin MPA + progressive enhancement + /api/v1
internal/sweatbox/           # Ground/taxi sim (stdlib + geo + twrfiles)
internal/cluster/            # FSD mesh framing, claim, directory, interest
internal/afv/                # AFV REST + UDP voice + optional AFV mesh
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

Instructors (**Instructor1+**) can author paired **`.apt`** (geometry) and **`.air`** (scenario
aircraft) files in the browser at **`/airport-editor`**:

- Map-first editing (Leaflet; JS required for the map; text paste + echo-download works without JS)
- Undo/redo (keyboard; client-only history stack)
- **No server persistence** of APT/AIR — open/download only (Blob download with JS; form echo-download without)
- Validate tab: live client parse + soft cross-file warnings; optional **Confirm with server**
  (`POST /api/v1/editor/validate-apt` and `validate-air`)
- Workflow: download files → load them on **`/sweatbox`** (editor never pushes into a live session)

Design notes: [docs/design/apt-air-editor.md](docs/design/apt-air-editor.md).

### AFV voice (optional)

Opt-in with **`-afv`**. Serves AFV REST (auth, callsign session, transceivers) and UDP
CryptoDTO voice (H/HA heartbeat, AT→AR radio routing). Not started by default.

```bash
./openfsd -fsd -web -afv   # colocated FSD + web + AFV
./openfsd -afv             # AFV only (shares DATABASE_* with FSD for users)
```

Minimum config: set `AFV_UDP_ADVERTISE_IPV4` (and typically `AFV_API_PUBLIC_BASE_URL`) so
clients learn the correct voice endpoint. See [wiki/Configuration](wiki/Configuration.md#afv-voice-optional)
and design [docs/design/afv-server.md](docs/design/afv-server.md). Multi-node voice directory
relay is in-tree (MemoryMesh tests); production TCP mesh is PR-10b.

## Build and run

```bash
go build -o openfsd ./cmd/openfsd

./openfsd              # FSD + web (default; AFV remains off)
./openfsd -fsd         # FSD only (:6809 + service HTTP :13618)
./openfsd -web         # web only (:8000)
./openfsd -afv         # AFV voice only (REST + UDP)
./openfsd -fsd -web -afv
```

| Flag | Effect |
|------|--------|
| *(none)* | FSD + web (AFV off) |
| `-fsd` | FSD only |
| `-web` | Web only |
| `-afv` | AFV only |
| `-fsd -web` | Both data plane + UI (same as default) |
| `-fsd -web -afv` | All three services in one process |

### Environment

| Variable | Default | Notes |
|----------|---------|-------|
| `DATABASE_SOURCE_NAME` | `openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` | SQLite path (preferred) or rqlite HTTP URL; must be shared by colocated services. Bare `:memory:` is rewritten to a process-shared DSN when ≥2 of FSD/web/AFV run together. |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` (default) or `rqlite`; Postgres removed (use migrate tools) |
| `DATABASE_AUTO_MIGRATE` | `true` | Apply migrations on startup (idempotent; rqlite uses `DATABASE_MIGRATE_LEADER`) |
| `FSD_LISTEN_ADDRS` | `:6809` | FSD TCP listen address(es) |
| `SERVICE_HTTP_LISTEN_ADDR` | `:13618` | Internal FSD admin HTTP |
| `FSD_HTTP_SERVICE_ADDRESS` | `http://127.0.0.1:13618` | Web → FSD service HTTP |
| `LISTEN_ADDR` | `:8000` | Web UI + `/api/v1` |
| `LOG_DEBUG` | *(unset)* | Set `true` for slog debug logging (default is info / release) |
| `GIN_MODE` | `release` | Gin mode for web + FSD service HTTP; set `debug` for Gin debug output |

AFV, cluster, and multi-FSD web vars: [wiki/Configuration](wiki/Configuration.md).

Colocated mode (default) uses the shared DB and in-process service HTTP. For `-web` against a remote FSD, set `FSD_HTTP_SERVICE_ADDRESS`.

## Quick start (Docker)

Preferred for operators. See the [Deployment Wiki](https://github.com/renorris/openfsd/wiki/Deployment) (source: [`wiki/`](wiki/)).

Images: **`ghcr.io/renorris/openfsd`** — CI publishes `:latest` from **`main` only**, `:dev` from **`dev`** (unstable), plus `sha-*` / branch tags.

**Upgrading from PostgreSQL?** Use [`openfsd-migrate-to-sqlite`](cmd/openfsd-migrate-to-sqlite) and [Migrating from PostgreSQL](wiki/Migrating-from-PostgreSQL.md).

**Optional multi-node:** rqlite + FSD mesh (`CLUSTER_ENABLED`, see [wiki/Deployment](wiki/Deployment.md) and `docs/design/distributed-openfsd.md`). Sample: `docker-compose.cluster.yml`.

```bash
git clone https://github.com/renorris/openfsd.git
cd openfsd
docker compose up -d          # pull/build single image; FSD + web
# or: docker compose up -d --build
```

1. Open `http://localhost:8000`
2. Log in with the default admin credentials (printed in container logs on first startup)
3. **Configure Server** — see the [Configuration](https://github.com/renorris/openfsd/wiki/Configuration) wiki
4. Connect a client — [Client Connection Wiki](https://github.com/renorris/openfsd/wiki/Client-Connection). For modern pilot clients (vPilot first), use **openfsd Client Setup** (`openfsd-client`): operator guide [docs/client-injector/README.md](docs/client-injector/README.md) (Phase 0 = on-disk apply; honest JWT host limits).

### Service selection

```bash
# FSD + web (default CMD)
docker run --rm -p 6809:6809 -p 8000:8000 ghcr.io/renorris/openfsd:latest

# FSD only
docker run --rm -p 6809:6809 ghcr.io/renorris/openfsd:latest /openfsd -fsd

# Web only (remote FSD)
docker run --rm -p 8000:8000 \
  -e FSD_HTTP_SERVICE_ADDRESS=http://fsd-host:13618 \
  ghcr.io/renorris/openfsd:latest /openfsd -web

# AFV only (example ports; set advertise + public base URL for real clients)
docker run --rm -p 8080:8080 -p 50000:50000/udp \
  -e AFV_API_LISTEN=0.0.0.0:8080 \
  -e AFV_UDP_ADVERTISE_IPV4=host.example:50000 \
  -e AFV_API_PUBLIC_BASE_URL=https://voice.example \
  ghcr.io/renorris/openfsd:latest /openfsd -afv
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
bash scripts/check-webjs.sh          # Node ≥20 (CI pins 24) airport-editor JS modules
go test -bench=. -benchmem ./internal/postoffice/ ./pkg/protocol/
go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
```

E2E: `internal/server` via `pkg/fsdclient` + `StartTestServer`. Stress is optional (CI schedule / `workflow_dispatch`).

## API

Operator automation under **`/api/v1`**: versioned discovery, users, config, FSD connections, sweatbox control, editor validate, and provisional account self-service.

- Operator guide + authz matrix: [`internal/web/README.md`](internal/web/README.md)
- OpenAPI (embedded): `GET /api/v1/openapi.yaml` or `internal/web/openapi/openapi.v1.yaml`
- Design: [docs/design/rest-api-versioning.md](docs/design/rest-api-versioning.md)

Send `OpenFSD-API-Version: YYYY-MM-DD` on production resource calls (see the web README).

## Protocol docs

Unofficial reverse-engineered FSD protocol docs live under `docs/`:

```bash
pip install mkdocs
mkdocs serve
```

## Operator wiki

Tracked source for the GitHub wiki is under [`wiki/`](wiki/) (Deployment, Configuration, Client Connection, Postgres migration).

**Client Setup:** configure user-installed Windows vPilot for private openfsd networks with auxiliary binary `openfsd-client` — see [docs/client-injector/README.md](docs/client-injector/README.md) and [wiki/Client-Connection.md](wiki/Client-Connection.md). Traditional installers (Windows Setup, macOS pkg/dmg, Linux deb) are built in CI — [packaging/openfsd-client/README.md](packaging/openfsd-client/README.md).
