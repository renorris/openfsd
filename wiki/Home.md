# openfsd wiki

Operator documentation for [openfsd](https://github.com/renorris/openfsd).

**This directory (`wiki/` in the main repository) is the source of truth** for the [GitHub wiki](https://github.com/renorris/openfsd/wiki). Edit pages here and keep the live wiki in sync when you publish (see [README in this folder](README.md)).

## Pages

| Page | Description |
|------|-------------|
| [Deployment](Deployment.md) | Docker Compose, Windows, and single-binary run |
| [Configuration](Configuration.md) | Env vars and persistent DB settings |
| [Client Connection](Client-Connection.md) | VRC, Euroscope, Swift, vPilot, xPilot |
| [Migrating from PostgreSQL](Migrating-from-PostgreSQL.md) | Convert an existing Postgres DB to SQLite |

## Admin web tools (in the product UI)

| URL | Who | Notes |
|-----|-----|--------|
| `/sweatbox` | Administrator | Live ground/taxi simulator control (needs FSD + sweatbox enabled) |
| `/sweatbox/manual` | Administrator | Instructor user manual (also linked from the control panel, opens in a new tab) |
| `/airport-editor` | Administrator | Map-first `.apt` / `.air` authoring; **download only** (no server save). Validate tab has live client checks + optional server confirm. Download files, then load them on `/sweatbox`. Design: repo `docs/design/apt-air-editor.md` |

## Quick links

- Images: `ghcr.io/renorris/openfsd` (`:latest`, `:dev`, `sha-*`)
- Protocol notes: repository `docs/`
