# openfsd wiki

Operator documentation for [openfsd](https://github.com/renorris/openfsd).

**This directory (`wiki/` in the main repository) is the source of truth** for the [GitHub wiki](https://github.com/renorris/openfsd/wiki). Edit pages here and keep the live wiki in sync when you publish (see [README in this folder](README.md)).

## Pages

| Page | Description |
|------|-------------|
| [Deployment](Deployment.md) | Docker Compose, Windows, single-binary run, optional cluster + AFV |
| [Configuration](Configuration.md) | Env vars (FSD, web, cluster, AFV) and persistent DB settings |
| [Client Connection](Client-Connection.md) | VRC, Euroscope, Swift, vPilot; optional AFV voice |
| [Migrating from PostgreSQL](Migrating-from-PostgreSQL.md) | Convert an existing Postgres DB to SQLite |

## Admin / instructor web tools (in the product UI)

| URL | Who | Notes |
|-----|-----|--------|
| `/dashboard` | Any signed-in user (OBS+) | Connection summary (HTML); optional Leaflet map PE |
| `/account` | Any signed-in user | Change password; soft-delete account (optional hard-delete via env) |
| `/usereditor` | Supervisor+ | Certificate directory create/update |
| `/configeditor` | Administrator | Server config, API tokens, JWT secret reset |
| `/sweatbox` | Instructor1+ | Live ground/taxi simulator control (needs FSD + sweatbox enabled) |
| `/sweatbox/manual` | Instructor1+ | Instructor user manual (also linked from the control panel) |
| `/airport-editor` | Instructor1+ | Map-first `.apt` / `.air` authoring; **download only** (no server save). Validate tab has live client checks + optional server confirm. Download files, then load them on `/sweatbox`. Design: repo `docs/design/apt-air-editor.md` |

## Operator REST

JSON under `/api/v1` (Bearer API token or session cookie + CSRF). Version pin via `OpenFSD-API-Version`. Full guide and OpenAPI: repository `internal/web/README.md` and `GET /api/v1/openapi.yaml`.

## Optional subsystems

| Feature | Flag / env | Docs |
|---------|------------|------|
| AFV voice | `-afv` + `AFV_*` | [Configuration](Configuration.md#afv-voice-optional), `docs/design/afv-server.md` |
| Multi-node FSD | `CLUSTER_ENABLED` + rqlite | [Deployment](Deployment.md#optional-multi-node-cluster), `docs/design/distributed-openfsd.md` |

## Quick links

- Images: `ghcr.io/renorris/openfsd` (`:latest` = main, `:dev` = unstable tip, `sha-*`)
- Protocol notes: repository `docs/`
- Package / import rules for contributors: repository `AGENTS.md`
