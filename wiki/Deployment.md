# Deployment

openfsd is a **single binary** (`openfsd`) that can run:

1. The FSD server (TCP protocol + internal service HTTP)
2. The web UI and `/api/v1`
3. Optionally, the **AFV** voice API + UDP voice server (`-afv`)

By default **FSD + web** run in one process (AFV stays off). Use CLI flags to select services.

Storage is **SQLite only** by default (a file on disk, or `:memory:` for ephemeral use). Optional multi-node uses **rqlite** for durable users/config. If you are upgrading from a PostgreSQL deployment, see [Migrating from PostgreSQL](Migrating-from-PostgreSQL.md).

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

Images: **`ghcr.io/renorris/openfsd`** — CI publishes `:latest` from **`main` only**, `:dev` from **`dev`** (unstable), plus `sha-*` / branch tags.

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

# AFV only (set advertise + public URL for real clients)
docker run --rm -p 8080:8080 -p 50000:50000/udp \
  -e AFV_API_LISTEN=0.0.0.0:8080 \
  -e AFV_UDP_ADVERTISE_IPV4=your.public.host:50000 \
  -e AFV_API_PUBLIC_BASE_URL=https://voice.example \
  -e DATABASE_SOURCE_NAME=/path/to/shared.db?... \
  ghcr.io/renorris/openfsd:latest /openfsd -afv
```

Back up the SQLite volume (or the `.db` file) regularly.

## Manual / bare metal

```bash
go build -o openfsd ./cmd/openfsd

export DATABASE_SOURCE_NAME=./openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
export DATABASE_AUTO_MIGRATE=true

./openfsd              # FSD + web
./openfsd -fsd         # FSD only
./openfsd -web         # web only
./openfsd -afv         # AFV only
./openfsd -fsd -web -afv
```

See [Configuration](Configuration.md) for the full environment list (including `AFV_*`).

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
- Multi-FSD web: set `FSD_HTTP_SERVICE_ADDRESSES` (`nodeID=http://host:port,...`) for online_users aggregation and kick routing.
- Do not put two writers on the same SQLite file over NFS without understanding SQLite locking limits; local disk or a block volume is preferred.
- AFV may run colocated (`-fsd -web -afv`) or as a separate process sharing the same user database DSN.

## Optional multi-node cluster

Single-node SQLite remains the **default** forever (`CLUSTER_ENABLED=false`, `DATABASE_DRIVER=sqlite`). Multi-node is opt-in.

See also `docs/design/distributed-openfsd.md` and the sample compose file `docker-compose.cluster.yml`.

### Topology (v1)

| Plane | Technology | Notes |
|-------|------------|--------|
| Durable users/config | **rqlite** (Raft SQLite) | `DATABASE_DRIVER=rqlite`, HTTP DSN |
| Live callsigns/positions | In-process postoffice + **TCP mesh** | Never stored in rqlite |
| Control plane | Web ×N → service HTTP on each FSD | `FSD_HTTP_SERVICE_ADDRESSES` |
| Voice (optional) | AFV process(es) | Separate from FSD mesh; see AFV section |

### Required env (FSD edges)

```text
CLUSTER_ENABLED=true
CLUSTER_NODE_ID=us-east-1
CLUSTER_LISTEN=0.0.0.0:7600
CLUSTER_PEERS=us-east-1=fsd-us:7600,eu-west-1=fsd-eu:7600
CLUSTER_MESH_PSK=change-me
CLUSTER_CLAIM_TIMEOUT=400ms
DATABASE_DRIVER=rqlite
DATABASE_SOURCE_NAME=http://rqlite:4001
DATABASE_MIGRATE_LEADER=true   # exactly one process
AUTH_READ_LEVEL=weak
```

Mesh trust root is **static `CLUSTER_PEERS` + PSK** (or future mTLS sidecars). Rows in rqlite `cluster_nodes` are publish-only for datafeed/UI — not mesh authorization.

**Mesh security notes (v1):**
- `CLUSTER_MESH_PSK` is required when `CLUSTER_ENABLED=true`. Hello carries PSK in cleartext — **mesh traffic must stay on a private network** (VPC/VPN). Prefer mTLS sidecars for hostile networks.
- Peers are a trusted admin domain: `ForceDisconnect` HomeRPC trusts the origin edge’s supervisor gate + peer-asserted rating. Kill is authorized on the origin node before mesh hop.
- RemoteAddr vs `CLUSTER_PEERS` host mismatches are logged (NAT may differ); spoofed node IDs not in the allowlist are rejected.

### Geo sticky routing (mandatory for multi-region wins)

Without sticky geo-nearest client attachment, multi-region mesh can **hurt** (cross-ocean position fan-out). Before marketing multi-region:

1. Publish multi-entry `servers.txt` / JSON (web builders when multi-FSD configured).
2. Point clients at the nearest edge (geo-DNS, Anycast, or LB sticky by client region).
3. Keep regional events on a shared edge when possible.

### HA honesty

- **Connected sessions:** node death disconnects only that node’s clients; survivors inject synthetic `#DP`/`#DA` after grace for map cleanup.
- **New logins:** owner-node claim — a down owner blocks only its **hash slice** of callsigns (fail-closed for that slice), not the whole cluster.
- **Per-CID limits** remain per-node (accepted v1 hole).

### Data migration SQLite → rqlite

1. Start rqlite; run schema migrate once (`DATABASE_MIGRATE_LEADER=true` or `MigrateRqlite`).
2. Copy rows: `openfsd-migrate-to-rqlite -sqlite openfsd.db -rqlite http://127.0.0.1:4001`
3. Point FSD/web at rqlite; keep a SQLite backup.

### Compose example

```bash
docker compose -f docker-compose.cluster.yml up -d --build
```

Default `docker compose up` remains **one container + SQLite**.

### CI note

Optional multi-node compose is **manual / ops** (`docker compose -f docker-compose.cluster.yml`). Default CI remains single-node `go test` + MemoryMesh unit/e2e tests (no required cluster compose job in v1).

## Optional AFV voice

AFV is **opt-in** (`-afv`). It is not started by the default Docker CMD.

| Plane | Port (defaults) | Notes |
|-------|-----------------|--------|
| AFV REST | `AFV_API_LISTEN` default `127.0.0.1:8080` | Auth, callsign, transceivers; prefer TLS in production |
| AFV UDP | `AFV_UDP_LISTEN` default `0.0.0.0:50000` | CryptoDTO Opus relay (opaque; no decode) |

**Required for clients to connect UDP:** `AFV_UDP_ADVERTISE_IPV4` (and optionally `AFV_UDP_ADVERTISE_IPV6`) — the address embedded in channel config for clients. Behind NAT, this must be the **public** host:port.

Share the same `DATABASE_*` as FSD so AFV authenticates against the same certificates. Optional `AFV_REQUIRE_FSD_ONLINE=true` gates voice sessions on the FSD online_users service.

Multi-node AFV mesh (`AFV_CLUSTER_*`) directory + AT relay is implemented for in-process/MemoryMesh testing; **production TCP mesh is not enabled** in the binary yet (`AFV_CLUSTER_ENABLED=true` fails closed until PR-10b). See `docs/design/afv-server.md` and `docs/design/afv-mesh-pr10.md`.

Full env list: [Configuration](Configuration.md#afv-voice-optional).
