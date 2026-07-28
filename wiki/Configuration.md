# Configuration

## Persistent configuration (database)

Some settings are stored in the SQLite (or rqlite) database and shared by FSD, web, and AFV when they use the same DSN.

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
| `DATABASE_SOURCE_NAME` | SQLite DSN (file path preferred) **or** rqlite HTTP base URL when `DATABASE_DRIVER=rqlite` (e.g. `http://rqlite:4001`). Bare `:memory:` is private per connection; when ≥2 of FSD/web/AFV run in one process it is rewritten to a shared in-memory DSN. | `openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` |
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
| `ALLOW_PERMANENT_ACCOUNT_DELETE` | When `true`, account self-service may hard-delete; default soft-delete only | `false` |

### AFV voice (optional)

Enabled only when the process is started with **`-afv`**. Shares `DATABASE_*` with FSD/web for certificate auth. Design: `docs/design/afv-server.md`.

| Name | Description | Default |
|------|-------------|---------|
| `AFV_API_LISTEN` | REST API listen address | `127.0.0.1:8080` |
| `AFV_API_PUBLIC_BASE_URL` | Public base URL clients use for REST (scheme + host) | *(empty)* |
| `AFV_UDP_LISTEN` | UDP CryptoDTO listen address | `0.0.0.0:50000` |
| `AFV_UDP_ADVERTISE_IPV4` | **Required to start UDP** — host:port embedded in channel config for clients | *(empty)* |
| `AFV_UDP_ADVERTISE_IPV6` | Optional IPv6 advertise host:port | *(empty)* |
| `AFV_TLS_CERT_FILE` / `AFV_TLS_KEY_FILE` | TLS for REST (prefer in production) | *(empty → HTTP)* |
| `AFV_JWT_SECRET` | Override JWT secret; else shared config KV `JWT_SECRET_KEY` | *(empty)* |
| `AFV_JWT_TTL` | AFV bearer token lifetime | `1h` |
| `AFV_HEARTBEAT_TIMEOUT` | UDP heartbeat timeout | `10s` |
| `AFV_SESSION_IDLE_TIMEOUT` | Idle session reaper | `30s` |
| `AFV_MAX_SESSIONS` | Global session cap | `5000` |
| `AFV_MAX_SESSIONS_PER_CID` | Per-CID session cap | `5` |
| `AFV_AUTH_FAIL_MAX` / `AFV_AUTH_FAIL_WINDOW` | Auth brute-force window | `20` / `1m` |
| `AFV_MAX_DATAGRAM` | Reject larger UDP frames before decrypt | `8192` |
| `AFV_REQUIRE_FSD_ONLINE` | Gate voice on FSD online_users | `false` |
| `AFV_FSD_HTTP_SERVICE_ADDRESS` | Service HTTP for online gate | `http://127.0.0.1:13618` |
| `AFV_CALLSIGN_STRICT` | Reject replace on callsign conflict (default allows replace) | `false` |
| `AFV_RANGE_UNICOM_NM` | Unicom max range (NM) | `15` |
| `AFV_RANGE_DEFAULT_NM` | Default pilot max range (NM) | `40` |
| `AFV_RANGE_ATC_NM` | ATC max range (NM) | `150` |
| `AFV_RANGE_EDGE_RATIO` | Range edge ratio for volume falloff | `0.1` |
| `AFV_STATIONS_FILE` | Optional station alias file (empty → `[]` aliases) | *(empty)* |
| `AFV_CROSS_COUPLE` | Multi-freq ATC cross-coupling | `true` |
| `AFV_CLUSTER_ENABLED` | AFV multi-node mesh | `false` |
| `AFV_CLUSTER_NODE_ID` | AFV mesh node id | *(required when enabled)* |
| `AFV_CLUSTER_LISTEN` | AFV mesh listen | *(required when enabled)* |
| `AFV_CLUSTER_PEERS` | `id=host:port,...` (max 4 remote) | *(required when enabled)* |
| `AFV_CLUSTER_PSK` | AFV mesh shared secret | *(required when enabled)* |

**Note:** `AFV_CLUSTER_ENABLED=true` currently **fails closed** without production TCP mesh in the binary (MemoryMesh is tests-only). See [Deployment](Deployment.md#optional-afv-voice).

Minimal single-node AFV example:

```text
AFV_API_LISTEN=0.0.0.0:8080
AFV_API_PUBLIC_BASE_URL=https://voice.example.com
AFV_UDP_LISTEN=0.0.0.0:50000
AFV_UDP_ADVERTISE_IPV4=voice.example.com:50000
DATABASE_SOURCE_NAME=/db/openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
```

### Logging

| Name | Description | Default |
|------|-------------|---------|
| `LOG_DEBUG` | Set to `true` for slog debug logging (FSD + web + AFV). Default is info / release. | *(unset)* |
| `GIN_MODE` | Gin mode for the web UI and FSD service HTTP. openfsd defaults to `release` when unset (Gin’s own default is `debug`). | `release` (when unset) |
| `GIN_LOGGER` | When set (any non-empty value), enables Gin’s request logger middleware on the web server. | *(unset)* |

## CLI flags

| Flag | Effect |
|------|--------|
| *(none)* | FSD + web (AFV off) |
| `-fsd` | FSD only |
| `-web` | Web only |
| `-afv` | AFV voice only |
| `-fsd -web` | FSD + web (same as default) |
| `-fsd -web -afv` | All three services in one process |
