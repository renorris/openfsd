# openfsd — Agent operational criteria

Operational rules for agents and humans changing this repository. This file is the in-repo source of truth.

**Layout is settled:** `pkg/*`, `internal/*`, single binary `cmd/openfsd` (FSD + web via CLI flags). Do not reintroduce dual entrypoints, top-level `fsd/` / `db/` / `web/` packages, or split Docker images.

---

## 1. Package ownership

| Package | Owns | Notes |
|---------|------|-------|
| `pkg/protocol` | Pure wire format (parse/serialize/validate) | No I/O; **stdlib only** |
| `pkg/twrfiles` | TWRTrainer `.apt`/`.air` types + Parse + Format | No I/O beyond text parse/serialize; **stdlib only** |
| `pkg/fsdclient` | Public mock/real FSD client | Imports `protocol` only (+ stdlib) |
| `internal/geo` | Pure haversine / bounding box | **stdlib only** |
| `internal/auth` | JWT + VATSIM auth state | No TCP |
| `internal/session` | Per-connection state + send worker | Does not import postoffice/server |
| `internal/postoffice` | Registry (map/tree of participants) | Depends on session ports, not server |
| `internal/metar` | Worker pool + injectable HTTP | Side-effect boundary |
| `internal/sweatbox` | Pure sim (taxi, engine, kinematics; apt/air via twrfiles) | **stdlib + `internal/geo` + `pkg/twrfiles`**; no protocol/session/server |
| `internal/server` | TCP accept, login, handlers, service HTTP | DI via `server.New` / `server.NewDefault` |
| `internal/db` | Shared repositories + migrations | Used by FSD and web |
| `internal/serviceapi` | Pure JSON DTOs for FSD service-HTTP (online users, sweatbox) | No I/O; shared by server + web |
| `internal/web` | Gin MPA + progressive enhancement + `/api/v1` | boring-web mandatory |
| `internal/cluster` | Mesh framing, claim, directory, interest, HomeRPC, MemoryMesh/TCP | stdlib + `internal/geo` + uuid; **no** server/web/db/postoffice |
| `cmd/openfsd` | Process entry | Binary `openfsd`; `-fsd` / `-web` (default both) |
| `cmd/openfsd-migrate-to-rqlite` | SQLite file → rqlite HTTP row copy | Depends on `internal/db` |

**Wire format:** `pkg/protocol` (FSD) and `pkg/twrfiles` (.apt/.air) — no alternate field order or marshaling in handlers/clients.

**Registry:** `internal/postoffice`. **Web PE:** `internal/web`.

### Allowed import direction

```
cmd/openfsd       → internal/server, internal/web, …
internal/server   → session, postoffice, protocol, auth, metar, db, sweatbox, serviceapi, cluster
internal/postoffice → geo, session
internal/metar    → protocol, session
internal/sweatbox → geo, pkg/twrfiles
internal/cluster  → geo (+ stdlib, google/uuid); never server/web/db/postoffice/session
internal/serviceapi → (stdlib only; no session/postoffice/server/web/cluster)
internal/web      → auth, db, protocol, serviceapi
                    (never server/session/postoffice/metar/sweatbox/cluster —
                     control plane is service HTTP + serviceapi DTOs;
                     may import pkg/twrfiles for optional APT/AIR validate)
pkg/fsdclient     → protocol
pkg/twrfiles      → (stdlib only)
internal/auth     → protocol
internal/session  → protocol
```

---

## 2. Forbidden import edges

Enforce with `scripts/check-import-graph.sh`.

| From | Must not import |
|------|-----------------|
| `pkg/protocol` | Any non-stdlib import |
| `pkg/twrfiles` | Any non-stdlib import |
| `pkg/fsdclient` | `internal/*` |
| `internal/session` | `postoffice`, `server`, `web`, `metar` |
| `internal/geo` | Any non-stdlib import |
| `internal/web` | `session`, `postoffice`, `metar`, `sweatbox`, `server`, `cluster` |
| `internal/serviceapi` | `server`, `session`, `postoffice`, `web`, `sweatbox`, `cluster` |
| `internal/db` | `server`, `session`, `web`, `fsdclient`, `cluster` |
| `internal/cluster` | `server`, `web`, `db`, `postoffice`, `session`, `sweatbox`, `metar`, `serviceapi` (allow: `geo`, `google/uuid`) |
| `internal/auth` | `server`, `session`, `web` |
| `internal/sweatbox` | `server`, `web`, `postoffice`, `session`, `db`, `auth`, `metar`, `fsdclient`, `protocol` |

Stdlib heuristic: first path element contains no `.` (e.g. `fmt`, `net/http`). Third-party is never allowed in `pkg/protocol`, `pkg/twrfiles`, or `internal/geo`.

**Cycle rule:** `session` never imports `postoffice`. Postoffice depends on a narrow participant/send port. Shared errors like `ErrCallsignInUse` live next to the registry, not in `pkg/protocol`.

**Service-HTTP contract:** JSON DTOs for `GET /online_users`, kick, and sweatbox control plane live in `internal/serviceapi`. `internal/web` must not import `internal/server` (hard-fail in `scripts/check-import-graph.sh`).

---

## 3. Wire-format change checklist

**FSD packets (`pkg/protocol`):**
- [ ] Golden fixture added/updated under `pkg/protocol/testdata/packets`
- [ ] Unit tests in `pkg/protocol`
- [ ] E2E green if behavior is user-visible (`internal/server` e2e)
- [ ] No marshal rewrite without fixture comparison (bytes must not silently change)
- [ ] Consult `docs/protocol.md`; no silent field reordering

**TWRTrainer `.apt`/`.air` (`pkg/twrfiles`):**
- [ ] Golden fixture added/updated under `pkg/twrfiles/testdata` (and Format goldens when Format exists)
- [ ] Unit tests in `pkg/twrfiles`
- [ ] Sweatbox + server sweatbox paths green if load/parse behavior is user-visible
- [ ] No parse/serialize rewrite without fixture comparison (bytes/messages must not silently change)
- [ ] Consult `docs/design/apt-air-editor.md` Format contract when adding FormatAPT/FormatAIR

---

## 4. Handler change checklist

- [ ] Table-driven unit test with fake Registry (and other fakes as needed)
- [ ] Handlers stay in split files under `internal/server` (no mega-`handler.go`)
- [ ] `slog` on unexpected paths; `$ER` or silent per existing protocol semantics
- [ ] Race-clean (`go test -race`)
- [ ] E2E green if path is user-visible

---

## 5. Deadlock rule

```text
Never hold a postoffice map/tree lock across Session.Send if Send may block
(e.g. full send channel).
```

Prefer: copy recipients under lock, unlock, then send; or non-blocking enqueue only while locked. See `internal/postoffice` and `internal/session` send path.

---

## 6. Boring-web (mandatory for web changes)

When touching `internal/web`, templates, or static JS:

| Resource | Path |
|----------|------|
| Skill | `~/.grok/skills/boring-web/SKILL.md` |
| Full rules | `~/.grok/skills/boring-web/references/house-standard.md` |
| Pre-merge checklist | `~/.grok/skills/boring-web/references/checklist.md` |
| Complexity gate | `~/.grok/skills/boring-web/references/decision-test.md` |

- [ ] Follow boring-web skill + checklist
- [ ] Primary task works with JS disabled (or exception noted in PR)
- [ ] No new client router / global store / hydration
- [ ] CSRF + server authz on privileged routes
- [ ] First-party JS budget or documented exception (e.g. map)

Before adding a frontend framework, client router, global store, or hydration layer: read the complexity decision test and prefer server-rendered MPA + progressive enhancement. Document exceptions in the PR.

---

## 7. PR checklist

- [ ] `go test -race ./...`
- [ ] `gofmt -l .` clean
- [ ] `bash scripts/check-coverage.sh 80` (or rely on CI)
- [ ] `bash scripts/check-hygiene.sh`
- [ ] `bash scripts/check-import-graph.sh`
- [ ] No `reflect` in `pkg/protocol`
- [ ] If web: boring-web checklist (§6)
- [ ] If protocol/handler: e2e green
- [ ] Single binary still builds: `go build -o openfsd ./cmd/openfsd`
- [ ] Docker image still builds when packaging changes: `docker build -t openfsd:local .`

`golangci-lint run` is useful locally; treat as advisory unless CI hard-fails it.

---

## 8. Coverage floors

Enforced by `scripts/check-coverage.sh` (CI).

| Scope | Floor | Enforcement |
|-------|-------|-------------|
| Overall (exclude `cmd/`) | ≥80% | **Hard** |
| `pkg/protocol` | ≥98% | **Hard** |
| `pkg/twrfiles` | ≥98% | **Hard** |
| `internal/geo` | ≥98% | **Hard** |
| `internal/auth` | ≥95% | **Hard** |
| `internal/postoffice` | ≥90% | **Hard** |
| `internal/sweatbox` | ≥95% | **Hard** |
| `internal/cluster` | ≥90% | **Hard** |
| `internal/web` | ≥80% | Soft (report only) |
| Overall aspirational | 90% | Soft (report only) |
| `cmd/*` | — | Excluded from measurement |

```bash
go test -race ./...                          # race gate
bash scripts/check-coverage.sh 80            # floors (no -race)
./scripts/coverage.sh                        # soft report only
```

---

## 9. How to run tests

```bash
# Unit + e2e (race)
go test -race ./...
go test -race ./internal/server/... -run E2E

# Coverage floors
bash scripts/check-coverage.sh 80

# Hygiene / import graph
bash scripts/check-hygiene.sh
bash scripts/check-import-graph.sh

# Format
gofmt -l .

# Web JS pure modules (Node ≥20; airport-editor parse/format parity)
bash scripts/check-webjs.sh
# or: cd webjs && npm test
# optional coverage: cd webjs && npm run test:coverage

# Benchmarks
go test -bench=. -benchmem ./internal/postoffice/ ./pkg/protocol/

# Stress (optional; CI schedule / workflow_dispatch)
go test -tags=stress -count=1 -timeout=120s ./internal/server/ -run TestStress -v
```

E2E uses `pkg/fsdclient` against `StartTestServer` in `internal/server`. Stress records baselines; no absolute latency fail on cold CI.

**Web JS:** pure modules live under `internal/web/static/js/openfsd/` (go:embed). Tests live under `webjs/` and import via relative path — do not duplicate sources under `webjs/src/`.

Manual smoke: see root `README.md` (Docker compose / single binary).

---

## 10. Hygiene rules

| Rule | Scope | Enforcement |
|------|-------|-------------|
| No `panic(` | non-test `.go` under `pkg/`, `internal/` | `scripts/check-hygiene.sh` |
| No `reflect` | `pkg/protocol` | `scripts/check-hygiene.sh` |
| No `fmt.Print` / `log.Print` / `log.Fatal` / `log.Panic` | `pkg/`, `internal/` (non-test) | `scripts/check-hygiene.sh` |
| Forbidden imports | §2 | `scripts/check-import-graph.sh` |
| gofmt | all `.go` | CI / local |

**False positives:** greps skip pure `//` and `*` comment lines but may still match string literals. Prefer restructuring strings over weakening the script.

### Logging and errors

- Libraries: `log/slog` only in `pkg/` / `internal/`
- Wrap errors with `%w`; sentinels + `errors.Is` / `errors.As`
- Panic forbidden in `pkg/*` and `internal/*` (tests exempt)
- Reflection forbidden in `pkg/protocol`
