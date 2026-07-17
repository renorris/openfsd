# Configuration

## Persistent configuration (database)

Some settings are stored in the SQLite database and shared by FSD and web when they use the same file.

**Important values to set after first boot:**

1. `FSD Server Hostname`
2. `API Server Base URL`

Use the web UI (**Configure Server**) to set these.

| Key | Description | Default |
| --- | ----------- | ------- |
| Welcome Message | Message sent to FSD clients upon successful connection | Connected to openfsd |
| FSD Server Hostname | Public hostname clients use for the FSD server (advertised in `/api/v1/data`) | localhost |
| FSD Server Ident | Ident name of FSD server (all caps, no whitespace) | OPENFSD |
| FSD Server Location | Location advertised to clients | Earth |
| API Server Base URL | Public base URL for the API (scheme + host, no path), e.g. `https://api.myfsdserver.com` | http://localhost |

## Environment variables

### Shared / database

| Name | Description | Default |
|------|-------------|---------|
| `DATABASE_SOURCE_NAME` | SQLite DSN: file path or `:memory:`. Prefer a file with WAL + busy timeout for production. | `:memory:` |
| `DATABASE_DRIVER` | **Compatibility only.** Must be omitted or `sqlite`. Any other value (including `postgres`) fails startup. See [Migrating from PostgreSQL](Migrating-from-PostgreSQL.md). | `sqlite` |
| `DATABASE_AUTO_MIGRATE` | When `true`, FSD applies SQLite migrations on startup. | `true` |
| `DATABASE_MAX_CONNS` | Max open SQL connections. `1` is fine for small servers. | `1` |

Example production DSN:

```text
/db/openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
```

### FSD server

| Name | Description | Default |
|------|-------------|---------|
| `FSD_LISTEN_ADDRS` | Comma-delimited FSD listen addresses, e.g. `0.0.0.0:6809` | `:6809` |
| `NUM_METAR_WORKERS` | METAR fetch worker count | `4` |
| `SERVICE_HTTP_LISTEN_ADDR` | Internal HTTP service (web ↔ FSD control plane) | `:13618` |

### Web server

| Name | Description | Default |
|------|-------------|---------|
| `LISTEN_ADDR` | HTTP listen address for UI + `/api/v1` | `:8000` |
| `FSD_HTTP_SERVICE_ADDRESS` | Base URL of the FSD internal HTTP API. Default assumes colocated FSD. | `http://127.0.0.1:13618` |
| `COOKIE_SECURE` | `true` / `false` force Secure cookies; empty derives from TLS / `X-Forwarded-Proto` | *(empty)* |

### Logging

| Name | Description | Default |
|------|-------------|---------|
| `LOG_DEBUG` | Set to `true` for debug logging | *(unset)* |

## CLI flags

| Flag | Effect |
|------|--------|
| *(none)* | Both FSD and web |
| `-fsd` | FSD only |
| `-web` | Web only |
| `-fsd -web` | Both (same as default) |
