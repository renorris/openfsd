# Implementer prompt: AFV multi-node mesh (PR-10)

**You are implementing Audio for VATSIM (AFV) distributed cluster mode for openfsd.**

Work only from this prompt, the design doc, and the existing codebase. Do not re-litigate package layout or P0 protocol choices.

---

## Mission

Implement **AFV multi-node mesh** so two (or more) openfsd AFV nodes can:

1. Keep each voice client **sticky** on one home node (REST + UDP CryptoDTO stay local).
2. **Sync transceiver directory** over a separate AFV mesh (not FSD `internal/cluster`).
3. **Relay AT frames** (opaque Opus + TX radio metadata) to peers that advertised interest.
4. On the remote node, run the **local range model** and encrypt **AR** with **local** session keys only.

**AEAD keys never leave the home node. Opus never goes through rqlite.**

This is design **PR-10 / KD-17** from `docs/design/afv-server.md`. P0 single-node AFV already exists.

---

## Mandatory reading (in order)

1. `docs/design/afv-server.md` — full doc; focus on:
   - Package layout / import rules
   - Concurrency (KD-16)
   - Radio router + range model
   - **Clustering** section (sticky + mesh, KD-17 frame table)
   - Key Decisions KD-1–KD-20 (especially KD-8, KD-9, KD-16, KD-17)
   - PR-10 acceptance criteria
2. Existing P0 code (do not break):
   - `pkg/afvprotocol/**` — pure wire; do not put mesh I/O here
   - `internal/afv/**` — registry, router, udp, server, config, bootstrap
   - `cmd/openfsd/main.go` — `-afv` wiring
   - `Agents.md` — ownership + import graph
   - `scripts/check-import-graph.sh`, `scripts/check-coverage.sh`
3. Patterns to **mirror conceptually** (do **not** import):
   - `internal/cluster/**` — framing/PSK/interest ideas only; AFV mesh is a **separate** subsystem under `internal/afv/`

---

## Non-goals (do not implement)

- Global callsign claim / race-safe multi-region uniqueness
- Dynamic peer discovery / membership
- Putting Opus or sessions into rqlite/SQLite
- Importing `internal/cluster`, `internal/server`, `internal/session`, `internal/postoffice`, `internal/web`
- Changing CryptoDTO client wire format
- ATIS / terrain / boring-web station UI
- Production multi-region ops claims beyond 2-node e2e + documented sticky LB
- Cross-coupling XC (PR-9) unless already present — mesh must carry `isXC` field for future use even if XC is off

---

## Import / package rules

| Package | May import |
|---------|------------|
| `pkg/afvprotocol` | stdlib + `golang.org/x/crypto` only — **no mesh** |
| `internal/afv` | `pkg/afvprotocol`, `internal/geo`, `internal/db`, `internal/auth`, `internal/serviceapi` (DTOs), stdlib, envconfig, etc. |
| `internal/afv` **must not** import | `internal/server`, `session`, `postoffice`, `web`, **`cluster`**, `sweatbox`, `metar` |

Put mesh under e.g.:

```text
internal/afv/mesh.go          # interfaces, types, Run lifecycle hook
internal/afv/mesh_frame.go    # encode/decode frames
internal/afv/mesh_memory.go   # in-process two-node mesh for tests
internal/afv/mesh_tcp.go      # TCP peer dial/listen (first cut OK if tests use memory)
internal/afv/mesh_*_test.go
```

Update `Agents.md` and import-graph script only if new edges are needed (prefer no new packages outside `internal/afv`).

---

## Architecture (normative)

```text
Client A ──REST+UDP──► AFV Node1 (home A)
                          │ local AT→AR
                          │ mesh: TrxDelta / Interest / AudioRelay
                          ▼
                       AFV Node2 (home B)
                          │ range model local only
Client B ◄──UDP AR───────┘ encrypt with B's keys
```

### Session / crypto

- Home node owns `ChannelTag`, `ClientTxKey`, `ClientRxKey`, UDP bind.
- Mesh **AudioRelay** carries: originNode, callsign, audioSeq, Opus bytes, lastPacket, isXC, TX radio fields (freq, lat, lon, alt, txID) — **never** client AEAD keys.
- Remote node treats AudioRelay as a synthetic TX, runs existing range/router logic against **local bound sessions only**, emits AR with recipient `ClientRxKey`.

### Sticky LB (ops requirement)

- Each node’s PostCallsignResponse `addressIpV4` remains **that node’s** `AFV_UDP_ADVERTISE_IPV4`.
- Document: clients/LB must stick REST+UDP to one node; first cut does **not** implement cluster-wide callsign claim.

---

## Config (already partially stubbed)

Honor existing env in `internal/afv/config.go` (extend as needed, keep names):

| Env | Role |
|-----|------|
| `AFV_CLUSTER_ENABLED` | default false; when false, zero mesh behavior (today) |
| `AFV_CLUSTER_NODE_ID` | required if enabled |
| `AFV_CLUSTER_LISTEN` | host:port for mesh TCP listen |
| `AFV_CLUSTER_PEERS` | `id=host:port,id2=host:port2` static peers |
| `AFV_CLUSTER_PSK` | shared secret for Hello |

If `AFV_CLUSTER_ENABLED=true` and required fields missing → **fail startup** with clear error (do not silent no-op).

Optional knobs (reasonable defaults OK):

- Peer heartbeat interval 2s, death after 15s
- Outbound queue depth **256**, **drop oldest AudioRelay**
- Interest max **4096** entries, rate ≤ ~2 Hz
- Max peers **≤ 4** (document / soft cap)

---

## Mesh framing (KD-17)

Transport: TCP between static peers (plus **memory mesh** for unit/e2e tests).

```text
Frame = [u32 BE length][u8 type][payload]
length = 1 + len(payload)   // type byte included in length, or document clearly and test both ends
max payload = 1 MiB
```

| Type | Name | Purpose |
|------|------|---------|
| 1 | Hello | nodeID + auth (constant-time PSK compare **or** HMAC-SHA256(PSK, nodeID\|\|nonce) — pick one, document in code comment + tests) |
| 2 | Heartbeat | liveness |
| 10 | TrxSnapshot | full remote directory for a node (optional on connect) |
| 11 | TrxDelta | incremental transceiver upserts |
| 12 | SessionLeave | callsign/tag left home node |
| 20 | AudioRelay | voice relay payload |
| 30 | Interest | set of (freqHz, cellKey) this node wants |

**Peer death:** no heartbeat for 15s → drop all directory entries attributed to that peer; local sessions unaffected.

**Deadlock rule:** never hold registry/index lock across mesh TCP send or UDP `WriteTo`.

**UDP hot path:** enqueue AudioRelay non-blocking; if queue full, drop oldest (or drop newest — design says **drop oldest**); never block `handleAudioTx` on mesh.

---

## Integration points in existing code

You must wire mesh into P0 paths without breaking single-node tests:

1. **`Server.Run` / `NewDefault`**
   - If cluster enabled, start mesh listener + dial peers + heartbeat.
   - Cancel mesh with same sibling-cancel pattern as HTTP/UDP.

2. **Transceiver POST / session create/replace/delete**
   - After local registry update, publish TrxDelta / SessionLeave to peers.

3. **`routeAT` / `handleAudioTx`**
   - After (or alongside) local recipients, if mesh enabled:
     - Determine peers interested in TX freq/cells (from peer Interest).
     - Enqueue AudioRelay (copy Opus bytes; do not retain packet buffer after return).

4. **Inbound AudioRelay**
   - Validate peer authenticated.
   - If `isXC` and you don't implement XC yet, still route as primary TX (do not XC-again).
   - Build synthetic TX geometry from payload; find **local** RX candidates; encrypt AR; `WriteTo` outside locks.

5. **Interest advertisement**
   - From local bound sessions’ transceiver freqs + geo cells (`internal/geo.CellCover` / `DefaultGridCellDeg`).
   - Recompute on transceiver update and periodically; send Interest ≤ 2 Hz.

6. **Reaper / session remove**
   - SessionLeave to peers when local session dies.

---

## Tests (required)

Race-clean: `go test -race ./internal/afv/... ./pkg/afvprotocol/...`

Minimum:

1. **Frame encode/decode** golden for each type (at least Hello, TrxDelta, AudioRelay, Interest).
2. **Memory mesh two-node e2e:**
   - Two `internal/afv` servers (or registries+mesh) in one process via memory transport.
   - Two fake clients with distinct keys/tags (reuse patterns from `api_test.go` UDP H/HA A2A).
   - Client A on node1, B on node2, same frequency, in range → B receives AR after A’s AT.
   - Out of range → no AR.
3. **Keys never on mesh:** assert AudioRelay payload has no key fields; optional fuzz that frame decoder rejects oversized frames.
4. **Queue drop-oldest** under artificial backpressure (unit).
5. **Cluster disabled** path: existing P0 tests still pass with no mesh.
6. **Startup fail** if enabled without PSK/node ID/peers as appropriate.

Do **not** require real multi-host TCP in CI; memory mesh is enough for PR-10. TCP can be implemented and lightly tested with localhost if time allows.

---

## Acceptance checklist

- [ ] `AFV_CLUSTER_ENABLED=false` (default): behavior identical to P0 single-node
- [ ] `AFV_CLUSTER_ENABLED=true` + valid config: mesh starts; 2-node memory e2e A2A green under `-race`
- [ ] No import of `internal/cluster` / server / session / postoffice / web
- [ ] `bash scripts/check-import-graph.sh` passes
- [ ] `bash scripts/check-hygiene.sh` passes
- [ ] `gofmt -l` clean on touched files
- [ ] `go build -o openfsd ./cmd/openfsd` succeeds
- [ ] `pkg/afvprotocol` coverage remains ≥98%
- [ ] `internal/afv` coverage does not regress badly; aim ≥80% soft, prefer ≥85% if PR-6 floor already hard
- [ ] Short ops note in design doc or `wiki/` / comment: sticky LB required; keys home-local
- [ ] Commit message(s) clear: `feat(afv): multi-node mesh directory + AT relay (PR-10)`

---

## Implementation order (suggested)

1. Frame types + encode/decode + unit tests  
2. Mesh interface + MemoryMesh (two endpoints)  
3. Directory merge (remote trx refs tagged with origin node) + SessionLeave  
4. Interest advertise + filter for relay  
5. Wire into transceiver updates + handleAudioTx + inbound AudioRelay → local AR  
6. Heartbeat / peer death  
7. TCP transport optional  
8. Config validation + Server.Run lifecycle  
9. Full e2e + hygiene  

---

## Code quality bar (openfsd)

- No `panic` in non-test `pkg/` / `internal/`
- `log/slog` only (no `fmt.Print` / `log.Print`)
- Wrap errors with `%w`
- Prefer table-driven tests
- Smallest change that meets PR-10; no drive-by refactors of P0 CryptoDTO

---

## Definition of done

An engineer can run two in-process AFV nodes with memory mesh, connect two protocol-level clients on different nodes, and hear AT→AR across the mesh with range applied — without rqlite, without FSD mesh, without shipping client keys off-node.

If something in the design is ambiguous, prefer the **conservative** interpretation (less flooding, drop under load, fail closed on auth) and note the choice in the commit message or a short `## Deviations` section in your summary.
