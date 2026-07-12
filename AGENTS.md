# openfsd — Agent operational criteria

This file is **operational and checkable**. Agents and humans must follow it when changing this repository.

**Primary track:** Track A (core FSD) is primary. Full completion is through **PR17**. **PR11 is an e2e checkpoint only** — not permission to stop FSD finish work (db move, `cmd/openfsd`, stress, coverage floor) or Track B (web PE).

**In-repo contract:** this file is the operational source of truth for agents working from the git tree.

**Design reference (may be local-only / not in this repo):**  
`~/.grok/docs/designs/openfsd-agentic-refactor-3bd159cb.md` — package layout, phased coverage, PR sequence. If that path is unavailable, follow this `AGENTS.md` alone.

---

## 1. Package ownership

Target layout (packages may not all exist yet; do not invent imports that violate the table).

| Package | Owns | Notes |
|---------|------|-------|
| `pkg/protocol` | Pure wire format (parse/serialize/validate) | No I/O; **stdlib only** (no third-party, no module-internal) |
| `pkg/fsdclient` | Public mock/real FSD client | Imports `protocol` only (+ stdlib) |
| `internal/geo` | Pure haversine / bounding box | **stdlib only** (no third-party, no module-internal) |
| `internal/auth` | JWT + VATSIM auth state | No TCP |
| `internal/session` | Per-connection state + send worker | Does not import postoffice/server |
| `internal/postoffice` | Registry (map/tree of participants) | Depends on session ports, not server |
| `internal/metar` | Worker pool + injectable HTTP | Side-effect boundary |
| `internal/server` | TCP accept, login, handlers, service HTTP | DI construction (`server.New`) |
| `internal/db` | Repositories + migrations | From root `db/` |
| `internal/web` | Importable Gin MPA + progressive enhancement | From root `web/` |
| `cmd/openfsd` | FSD process entry | Builds operator binary `fsd` |
| `cmd/openfsd-web` | Web process entry | Builds operator binary `fsdweb` |

**Legacy (moving):** `fsd/`, `db/`, `web/` at module root are the current tree. They will move into `internal/*` / `cmd/*` over PRs. New code should follow the target ownership above; do not grow new coupling that blocks the move.

**Wire format owner:** `pkg/protocol` only. Handlers and clients must not invent alternate field order or marshaling.

**Registry owner:** `internal/postoffice`. **Web PE owner:** `internal/web` (boring-web mandatory).

### Allowed import direction (summary)

```
cmd/openfsd     → internal/server, internal/db, …
cmd/openfsd-web → internal/web, internal/db, …
internal/server → session, postoffice, protocol, auth, metar, db
internal/postoffice → geo, session
internal/metar  → protocol, session
internal/web    → auth, db, protocol
pkg/fsdclient   → protocol
internal/auth   → protocol
internal/session → protocol
```

---

## 2. Forbidden import edges

Enforce with `scripts/check-import-graph.sh` (and later `TestImportGraph` when packages exist).

| From | Must not import |
|------|-----------------|
| `pkg/protocol` | **Any non-stdlib import** (no third-party; no module packages) |
| `pkg/fsdclient` | `internal/*` |
| `internal/session` | `postoffice`, `server`, `web`, `metar` |
| `internal/geo` | **Any non-stdlib import** (no third-party; no module packages) |
| `internal/web` | `server`, `session`, `postoffice`, `metar` |
| `internal/db` | `server`, `session`, `web`, `fsdclient` |
| `internal/auth` | `server`, `session`, `web` |

Enforcement (`scripts/check-import-graph.sh`): walks **every package** under each root (`…/...`), checks **direct** imports. Stdlib heuristic: first path element contains no `.` (e.g. `fmt`, `net/http`). Third-party is **never** allowed in `pkg/protocol` or `internal/geo`.

**Cycle prevention:** `session` never imports `postoffice`. Postoffice depends on a narrow participant/send port. Shared errors like `ErrCallsignInUse` live next to the registry, not in `pkg/protocol`.

---

## 3. Wire-format invariance checklist

Done for a packet / wire change means:

- [ ] Golden fixture added/updated under `testdata/packets` (when package exists)
- [ ] Unit tests in `pkg/protocol`
- [ ] After **PR11**: e2e scenario green if behavior is user-visible
- [ ] No marshal rewrite without fixture comparison (differential vs previous bytes)
- [ ] Protocol docs (`docs/protocol.md`) consulted; no silent field reordering

---

## 4. Handler change “done means”

- [ ] Table-driven unit test with fake Registry (and other fakes as needed)
- [ ] No growth of a single `handler.go` beyond split files
- [ ] `slog` on unexpected paths; `$ER` or silent per existing protocol semantics (document which)
- [ ] Race-clean (`go test -race`)
- [ ] After **PR11**: e2e green if path is user-visible

---

## 5. Deadlock rule

```text
Never hold a postoffice map/tree lock across Session.Send if Send may block
(e.g. full send channel).
```

Prefer: copy recipients under lock, unlock, then send; or non-blocking enqueue only while locked.

Pointers: `internal/postoffice` comments + `session` send path (when present). Today’s legacy code lives under `fsd/` until the split.

---

## 6. Boring-web (mandatory for web PRs)

When touching `internal/web`, root `web/`, templates, or static JS:

| Resource | Path |
|----------|------|
| Skill | `~/.grok/skills/boring-web/SKILL.md` |
| Full rules | `~/.grok/skills/boring-web/references/house-standard.md` |
| Pre-merge checklist | `~/.grok/skills/boring-web/references/checklist.md` |
| Complexity gate | `~/.grok/skills/boring-web/references/decision-test.md` |

Checklist:

- [ ] Follow boring-web skill + checklist
- [ ] Primary task works with JS disabled (or exception noted in PR)
- [ ] No new client router / global store / hydration
- [ ] CSRF + server authz on privileged routes
- [ ] First-party JS budget or documented exception (e.g. map)
- [ ] `checklist.md` filled in PR body

---

## 7. PR checklist (all PRs)

- [ ] `go test -race ./...`
- [ ] `golangci-lint run` (advisory in early PRs; hard fail later)
- [ ] Coverage floors for this milestone (see §8); report via `scripts/coverage.sh`
- [ ] No `panic(` in non-test `pkg/` / `internal/` (`scripts/check-hygiene.sh`)
- [ ] No `fmt.Print` in library `pkg/` / `internal/` (use `slog`)
- [ ] No `reflect` in `pkg/protocol`
- [ ] If web: boring-web checklist (§6)
- [ ] If protocol/handler after **PR11**: e2e green
- [ ] Import graph: no forbidden edges (§2)

---

## 8. Coverage floors (phased)

### By package

| Package | Target coverage (phase) |
|---------|-------------------------|
| `pkg/protocol` | ≥98% after PR2 |
| `internal/geo` | ≥98% after PR3 |
| `internal/auth` | ≥95% after PR4 |
| `internal/postoffice` | ≥90% after PR6 |
| `internal/db` | ≥85% |
| `internal/server` handlers | ≥90% of handler pkgs after PR9 |
| `internal/metar` | ≥90% |
| `internal/web` | ≥80% after PE + API tests |
| `pkg/fsdclient` | ≥85% |
| `cmd/*` | Soft / excluded from hard gate |

### Milestone gates

| Milestone | Gate |
|-----------|------|
| PR0–PR1 | Report coverage only; **no fail** on floor |
| After PR2 | `pkg/protocol` ≥98% hard fail |
| After PR3–PR4 | `geo` ≥98%, `auth` ≥95% |
| After PR11 (MVP checkpoint) | Overall **≥70%** of packages under test; exclude `cmd/` |
| After PR16 (PE complete) | Web package floor ≥80% |
| After **PR17** | Overall **≥80%** hard fail; pure pkgs keep high floors; aspirational 90% overall tracked but not blocking |

**Measurement:**

```bash
# Race gate (CI Test step) — run separately:
go test -race ./...

# Soft coverage (no -race; avoids double race suite in CI):
./scripts/coverage.sh
# equivalent:
go test -coverprofile=cover.out ./...
go tool cover -func=cover.out
# script also prints an in-scope total with cmd/* filtered out
```

`cmd/*` is soft-excluded from floor measurement. Hard package floors (PR2+) will be enforced later via env/milestone gates; this PR only reports.

---

## 9. How to run tests

### Unit / package tests

```bash
go test -race ./...
go test -race ./fsd/... ./db/...   # current legacy packages
# later:
go test -race ./pkg/... ./internal/...
```

### Coverage report (soft)

```bash
./scripts/coverage.sh
```

Does **not** pass `-race` (race is a separate gate). Exits non-zero only if tests fail, not if coverage is below a floor (until floors are enforced in CI). Prints full-tree total plus an in-scope summary excluding `cmd/*`.

### Hygiene / import graph

```bash
./scripts/check-hygiene.sh
./scripts/check-import-graph.sh
```

### Format

```bash
gofmt -l .
# CI fails if any file is listed
```

### Lint

```bash
golangci-lint run
```

Lint is **advisory** in CI (`continue-on-error: true`) until a dedicated cleanup PR removes legacy findings and hard-fails the gate.
### E2E (after PR11)

```bash
# StartTestServer (test helper) seeds DB/config/users, injects JWT secret,
# and uses fake METAR transport — mirrors NewDefaultServer essentials.
go test -race ./internal/server/... -run E2E   # paths may refine when package lands
go test -race ./... -run E2E
```

Any PR that changes handlers or `pkg/protocol` after PR11 must keep e2e green.

### Stress / benchmarks (PR17)

```bash
go test -race -tags=stress ./... -count=1
go test -bench=. -benchmem ./internal/postoffice/ ./pkg/protocol/
```

Stress records baselines; no absolute latency fail on cold CI in PR17.

### Manual client smoke

See README / wiki for connecting a VATSIM-compatible client to a local Docker compose stack.

---

## 10. Complexity decision test

Before adding a new frontend framework, client router, global store, or hydration layer:

1. Read `~/.grok/skills/boring-web/references/decision-test.md`
2. Prefer server-rendered MPA + progressive enhancement
3. Document any exception in the PR with the decision-test outcome

---

## Hygiene rules (CI-enforced)

| Rule | Scope | Enforcement |
|------|-------|-------------|
| No `panic(` | non-test `.go` under `pkg/`, `internal/` | `scripts/check-hygiene.sh` |
| No `reflect` | `pkg/protocol` | `scripts/check-hygiene.sh` |
| No `fmt.Print` / `log.Print` / `log.Fatal` / `log.Panic` | `pkg/`, `internal/` (non-test) | `scripts/check-hygiene.sh` |
| Forbidden imports | See §2 | `scripts/check-import-graph.sh` |
| gofmt | all `.go` | CI `gofmt -l .` |

Legacy `fsd/` will move into `internal/` / `pkg/`. Hygiene greps target the **target** trees so early PRs stay green while the split lands; do not add new panics/prints in new packages.

**False positives:** greps skip pure `//` and `*` comment lines but may still match string literals. Prefer restructuring strings over weakening the script.

---

## Logging and errors

- Libraries: `log/slog` only (no `fmt.Print` / `log.Print` / `log.Fatal` / `log.Panic` in `pkg/` / `internal/`)
- Wrap errors with `%w`; sentinels + `errors.Is` / `errors.As`
- Panic forbidden in `pkg/*` and `internal/*` (tests exempt)
- Reflection forbidden in `pkg/protocol`

---

## Program of work (reminder)

| Phase | PRs | Focus |
|-------|-----|--------|
| Foundations | 0–1 | Green tests, CI, this file, lint/coverage tooling |
| Protocol / pure pkgs | 2–4 | `pkg/protocol`, `geo`, `auth` |
| Session / postoffice / server | 5–9 | DI, handlers |
| Client + e2e | 10–11 | `pkg/fsdclient`, e2e **checkpoint** |
| Finish Track A layout | 12, 14 | `internal/db`, `cmd/openfsd` |
| Track B web PE | 13, 15–16 | Importable web + PE |
| Stress + coverage | 17 | Baselines, overall ≥80%, drop empty `fsd/` shim |

**Do not treat the plan as complete after web changes alone or after PR11.**
