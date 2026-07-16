# openfsd

[![license](https://img.shields.io/github/license/renorris/openfsd)](https://github.com/renorris/openfsd/blob/main/LICENSE)

**openfsd** is an open-source multiplayer flight simulation server implementing the modern VATSIM FSD protocol. It connects pilots and air traffic controllers in a shared virtual environment.

## About

Flight Sim Daemon (colloquially known as FSD) is the software/protocol responsible for connecting home flight simulator clients to a single, shared multiplayer world on hobbyist networks such as [VATSIM](https://vatsim.net/docs/about/about-vatsim) and [IVAO](https://www.ivao.aero/).
FSD was originally written in the late 90's by [Marty Bochane](https://github.com/kuroneko/fsd) for [SATCO](https://web.archive.org/web/20000619145015/http://www.satco.org/), later to be forked and taken closed-source by VATSIM in 2001.
As of May 2025, FSD is still used to facilitate over 140,000 active members connecting their flight simulators to the [network](https://vatsim-radar.com/).

## Features

- Facilitate multiplayer flight simulation with VATSIM protocol compatibility.
- Integrate web-based management for users, settings, and connections.
- Support SQLite and PostgreSQL for persistent storage.

## Package layout

```
cmd/openfsd/          # FSD binary entrypoint (Docker still outputs /fsd)
cmd/openfsd-web/      # Web binary entrypoint (Docker still outputs /fsdweb)
pkg/protocol/         # Pure wire format (parse/marshal; no I/O)
pkg/fsdclient/        # Public mock/real FSD client for e2e and tools
internal/server/      # TCP accept, login, handlers, service HTTP
internal/session/     # Per-connection state + outbound send worker
internal/postoffice/  # Callsign registry + geospatial index
internal/geo/         # Pure haversine / bounding box
internal/auth/        # JWT + VATSIM client auth
internal/metar/       # METAR worker pool (injectable HTTP)
internal/db/          # Repositories + migrations
internal/web/         # Importable Gin app (PE MPA + /api/v1)
```

There is no residual top-level `fsd/` package.

## Binaries

```bash
go build -o openfsd ./cmd/openfsd
go build -o openfsd-web ./cmd/openfsd-web
```

## Quick Start with Docker

The preferred way to run openfsd is using **Docker** and **Docker Compose**. See the [Deployment Wiki](https://github.com/renorris/openfsd/wiki/Deployment).

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### Steps

1. **Clone the Repository**:
   ```bash
   git clone https://github.com/renorris/openfsd.git
   cd openfsd
   ```

2. **Start with Docker Compose**:
   ```bash
   docker-compose up -d
   ```
   This launches the FSD server and web server sharing an SQLite database persisted in a named Docker volume. This setup will work great for most people running small servers.

3. **Configure the Server via Web Interface**:
    - Open `http://localhost:8000` in a browser.
    - Log in with the default administrator credentials (printed in the FSD server logs on first startup).
    - Navigate to the **Configure Server** menu
    - Set configuration values. See the [Configuration](https://github.com/renorris/openfsd/wiki/Configuration) wiki.

4. **Connect**:
   See the [Client Connection Wiki](https://github.com/renorris/openfsd/wiki/Client-Connection) for client-specific instructions.

## Tests

```bash
# Unit + e2e (race detector). Stress is behind build tag and is not run here.
go test -race ./...

# Coverage excluding cmd/ (CI hard floor ≥80%; aspirational 90%)
# Also enforces pure-package floors: protocol/geo ≥98%, auth ≥95%, postoffice ≥90%.
# Web ≥80% is reported as soft/aspirational only.
go test -coverprofile=cover.out $(go list ./... | grep -v '/cmd/')
go tool cover -func=cover.out | tail -1
# or:
bash scripts/check-coverage.sh 80

# Benchmarks (postoffice + protocol)
go test -bench=. -benchmem ./internal/postoffice/ ./pkg/protocol/

# Stress baselines (optional; not on every PR)
# Also available via GitHub Actions: workflow_dispatch or weekly schedule.
go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
# Override pilot count: OPENFSD_STRESS_M=500 go test -tags=stress ...
```

E2E scenarios live in `internal/server/e2e_test.go` and use `pkg/fsdclient` against `StartTestServer`.

### Compose smoke (manual checklist)

Published compose images (`ghcr.io/...`) may lag local source. For a local smoke after building images from this tree:

```bash
# Build local images (Dockerfiles produce /fsd and /fsdweb entrypoints)
docker build -f Dockerfile_fsd -t openfsd-fsd:local .
docker build -f Dockerfile_web -t openfsd-web:local .

# Point compose at local tags (or run containers manually sharing a volume), then:
docker compose up -d
# Web UI / PE login page
curl -fsS -o /dev/null -w "%{http_code}\n" http://localhost:8000/login
# Public data status (after API base URL is configured)
curl -fsS http://localhost:8000/api/v1/data/status.txt | head
# FSD TCP port open
nc -z localhost 6809 && echo "fsd:6809 open"
docker compose down
```

Full image CI/publish is out of band; this is an operator checklist, not a PR gate.

## API

The web server exposes APIs under `/api/v1` for authentication, user management, and configuration. Although a basic web interface is provided, users are encouraged to call this API from their own external applications. See the [API](https://github.com/renorris/openfsd/tree/main/internal/web) documentation.

## Docs

Unofficial reverse-engineered protocol documentation is included in this repository:

```
pip install mkdocs
git clone git@github.com:renorris/openfsd.git
cd openfsd/
mkdocs serve
```
