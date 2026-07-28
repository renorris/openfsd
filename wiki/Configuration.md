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
| `DATABASE_SOURCE_NAME` | SQLite DSN (file path preferred) **or** rqlite HTTP base URL when `DATABASE_DRIVER=rqlite` (e.g. `http://rqlite:4001`). Bare `:memory:` is private per connection; when both services run in one process it is rewritten to a shared in-memory DSN. | `openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` |
| `DATABASE_DRIVER` | `sqlite` (default) or `rqlite`. Legacy `postgres` fails startup — see [Migrating from PostgreSQL](Migrating-from-PostgreSQL.md). | `sqlite` |
| `DATABASE_AUTO_MIGRATE` | When `true`, apply migrations on startup. For rqlite, only the process with `DATABASE_MIGRATE_LEADER=true` migrates. | `true` |
| `DATABASE_MIGRATE_LEADER` | When `true` with rqlite, this process runs schema migrations (exactly one leader). | `false` |
| `DATABASE_MAX_CONNS` | Max open SQL connections (sqlite). `1` is fine for small servers. | `1` |
| `AUTH_READ_LEVEL` | rqlite read consistency for login `GetUserByCID`: `weak` / `strong` / `none`. | `weak` |

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

### Cluster (optional multi-node FSD mesh)

Default is **off**. See [Deployment](Deployment.md#optional-multi-node-cluster) and `docs/design/distributed-openfsd.md`.

| Name | Description | Default |
|------|-------------|---------|
| `CLUSTER_ENABLED` | Enable inter-node mesh | `false` |
| `CLUSTER_NODE_ID` | Stable node id (e.g. `us-east-1`) | *(required when enabled)* |
| `CLUSTER_LISTEN` | Mesh listen `host:port` | *(required when enabled)* |
| `CLUSTER_PEERS` | `id=host:port,...` static peers (≤8), trust root | *(required when enabled)* |
| `CLUSTER_MESH_PSK` | **Required** shared secret for mesh Hello | *(required when enabled)* |
| `CLUSTER_CLAIM_TIMEOUT` | Owner claim RPC timeout | `400ms` |
| `CLUSTER_PEER_DEATH_GRACE` | Peer hard-death grace before synthetic leave | `15s` |

### Web server

| Name | Description | Default |
|------|-------------|---------|
| `LISTEN_ADDR` | HTTP listen address for UI + `/api/v1` | `:8000` |
| `FSD_HTTP_SERVICE_ADDRESS` | Base URL of the FSD internal HTTP API. Default assumes colocated FSD. | `http://127.0.0.1:13618` |
| `FSD_HTTP_SERVICE_ADDRESSES` | Multi-FSD list: bare URLs or `nodeID=http://host:port` pairs (kick routing + online_users aggregate) | *(empty → single address)* |
| `COOKIE_SECURE` | `true` / `false` force Secure cookies; empty derives from TLS / `X-Forwarded-Proto` | *(empty)* |

### Logging

| Name | Description | Default |
|------|-------------|---------|
| `LOG_DEBUG` | Set to `true` for slog debug logging (FSD + web). Default is info / release. | *(unset)* |
| `GIN_MODE` | Gin mode for the web UI and FSD service HTTP. openfsd defaults to `release` when unset (Gin’s own default is `debug`). | `release` (when unset) |
| `GIN_LOGGER` | When set (any non-empty value), enables Gin’s request logger middleware on the web server. | *(unset)* |

## CLI flags

| Flag | Effect |
|------|--------|
| *(none)* | Both FSD and web |
| `-fsd` | FSD only |
| `-web` | Web only |
| `-fsd -web` | Both (same as default) |
