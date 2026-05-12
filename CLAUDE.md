# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

OpenFSD is an open-source server implementing the VATSIM FSD (Flight Sim Daemon) protocol. It lets flight simulator clients (pilots and ATC) connect to a shared multiplayer network. Two services run together: an FSD protocol server and a web admin interface.

## Commands

All commands assume Go 1.24 is installed. The FSD server and web server are separate Go binaries built from the same module.

```bash
# Build
go build -o fsd .           # FSD server binary
cd web && go build -o fsdweb # Web server binary

# Test
go test ./...               # All tests
go test ./fsd/...           # FSD package only
go test -v -run TestName ./fsd/  # Single test

# Lint / vet
go vet ./...
go fmt ./...

# Run FSD server (from repo root)
DATABASE_AUTO_MIGRATE=true \
DATABASE_SOURCE_NAME="openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" \
go run .

# Run web server (separate terminal, from /web)
FSD_HTTP_SERVICE_ADDRESS="http://localhost:13618" \
DATABASE_SOURCE_NAME="../openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" \
go run .

# Docker Compose (recommended for local dev)
docker-compose up -d
```

## Architecture

Two services share one database and communicate over an internal HTTP API:

```
Flight sim clients
      ↓ TCP :6809
  [FSD Server]  ←→  SQLite / PostgreSQL
      ↓ HTTP :13618 (internal)
  [Web Server]  ←  Admin clients (HTTP :8000)
```

- **`/` (root)** — FSD server entry point (`main.go` → `fsd.NewDefaultServer`)
- **`/fsd/`** — Core protocol engine: TCP listener, per-client goroutines, packet parsing, PostOffice message router, METAR worker pool
- **`/web/`** — Gin-based REST API + HTML admin UI; calls FSD's internal HTTP service to query/manage live connections
- **`/db/`** — Repository pattern over `database/sql`; separate implementations for SQLite (`*_sqlite.go`) and PostgreSQL (`*_postgres.go`); migrations in `/db/migrations/{sqlite,postgres}/`

### Key types in `/fsd/`

| Type | Role |
|------|------|
| `Server` | Owns the TCP listeners, METAR service, and PostOffice |
| `Client` | Per-connection state: callsign, lat/lon, altitude, rating, flight plan |
| `postOffice` | Routes packets — holds a `clientMap` (callsign → Client) and an R-tree spatial index for range-based broadcasts |
| `metarService` | Worker-pool fetching real METAR data from NOAA |
| `vatsimAuthState` | MD5 challenge-response for legacy VATSIM client authentication |

### Request / connection lifecycle

1. Client connects → server sends `$DI` (server ident)
2. Client sends `$ID` (client ident) then `#AP` (pilot) or `#AA` (ATC) with a JWT token
3. Server validates the JWT, checks callsign uniqueness, registers the client in the PostOffice
4. Event loop: reads lines, dispatches to packet handlers, handlers broadcast via PostOffice
5. On disconnect: remove from PostOffice, broadcast `#DP`/`#DA` departure packet

## FSD Protocol

Line-based plaintext over TCP, ISO 8859-1, CRLF-terminated. Fields separated by `:`.

Packet format: `{PREFIX}{FROM}:{TO}:{FIELDS...}`

Key prefixes: `$DI`/`$ID` (ident), `#AP`/`#AA`/`#DP`/`#DA` (login/logout), `@`/`^`/`#SL`/`#ST` (position), `#TM` (text), `$AX` (METAR), `$FP`/`$AM` (flight plan), `$ZC`/`$ZR` (auth challenge/response), `$!!` (kick).

Special recipients: `*` (all), `*S` (supervisors), `SERVER` (server-handled), `@FREQ` (frequency broadcast, e.g. `@12400`).

The full protocol spec is in `/docs/protocol.md`.

## Database

SQLite is the default. Switch to PostgreSQL by setting `DATABASE_DRIVER=postgres` and a valid `DATABASE_SOURCE_NAME`.

Schema has two tables: `users` (CID, hashed password, name, network rating) and `config` (key-value for JWT secret, server name, etc.). Migrations run automatically when `DATABASE_AUTO_MIGRATE=true`.

## Authentication

- **Web API** — JWT access + refresh tokens issued by `/api/v1/auth/login`. Pass as `Bearer` in `Authorization` header.
- **FSD protocol** — Clients embed a JWT in their login packet (`#AP`/`#AA`). The server also performs a VATSIM legacy MD5 challenge-response handshake (`$ZC`/`$ZR`) for compatible clients.

## Environment Variables

**FSD server** (`fsd/env.go`):

| Variable | Default | Description |
|----------|---------|-------------|
| `FSD_LISTEN_ADDRS` | `:6809` | FSD TCP listen addresses (comma-separated) |
| `DATABASE_DRIVER` | `sqlite` | `sqlite` or `postgres` |
| `DATABASE_SOURCE_NAME` | `:memory:` | SQL DSN |
| `DATABASE_AUTO_MIGRATE` | `false` | Run migrations on startup |
| `DATABASE_MAX_CONNS` | `1` | Connection pool size |
| `NUM_METAR_WORKERS` | `4` | METAR fetch worker count |
| `SERVICE_HTTP_LISTEN_ADDR` | `:13618` | Internal HTTP service address |
| `LOG_DEBUG` | — | Set to `true` for debug logging |

**Web server** (`web/env.go`):

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8000` | HTTP listen address |
| `DATABASE_DRIVER` | `sqlite` | Same as FSD server |
| `DATABASE_SOURCE_NAME` | `:memory:` | Same DSN as FSD server |
| `DATABASE_MAX_CONNS` | `1` | Connection pool size |
| `FSD_HTTP_SERVICE_ADDRESS` | *(required)* | URL of FSD's internal HTTP service |
