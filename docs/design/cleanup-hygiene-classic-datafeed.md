# openfsd Cleanup Program — Hygiene, Classic FSD Removal, Design-Doc Closeout, Datafeed Honesty

| Field | Value |
|-------|--------|
| **Document** | Multi-part cleanup: hygiene/CI gates, panic removal, classic FSD dual-path elimination, design-doc closeout, datafeed field population |
| **Author** | design-doc-writer |
| **Date** | 2026-07-27 |
| **Status** | Draft |
| **Target repo** | `/Users/rnorris/scratch/openfsd` (`github.com/renorris/openfsd`) |
| **Related** | `Agents.md` §2/§7/§10, `scripts/check-hygiene.sh`, `internal/server/{conn,gnet_fsd,server,deps}.go`, `internal/web/{auth,data}.go`, `internal/serviceapi/online_users.go`, `docs/design/sweatbox-integrated-simulator.md`, `docs/design/apt-air-editor.md` |

---

## Overview

This program implements **all** recommended operations from a prior architecture review. openfsd’s layout is settled (`pkg/*`, `internal/*`, single binary `cmd/openfsd`). Sweatbox, airport editor, and `pkg/twrfiles` have landed. Remaining cleanup is operational debt that still taxes everyday changes:

1. **Hygiene enforcement is broken** (script exits 0 on panic hits) and **two real panics** remain in non-test library code.
2. **CI does not run** hygiene, import-graph, or gofmt despite AGENTS.md §7.
3. **Classic FSD** (`handleConn` / `eventLoop` / accept loop) is still a dual path gated by `Deps.Listen != nil || ForceClassicFSD`, even though production and `StartTestServer` use gnet.
4. **Design docs** for sweatbox and airport editor still say **Draft** with stale “current state” tables.
5. **Public datafeed** invents pilot/military ratings and QNH, and never exports flight plans that sessions already hold.

The work is split into independently mergeable PRs. Classic FSD removal is the riskiest slice and is isolated after hygiene/CI are green so regressions are easier to diagnose.

---

## Goals & Non-Goals

### Goals

1. Make `scripts/check-hygiene.sh` **exit non-zero** when it prints violations; wire it into CI.
2. **Eliminate all `panic(`** in non-test `pkg/` and `internal/` code (today: `getJwtContext`, `NewCoalesceOutbound`) **and** ensure JWT handlers fail closed without nil-deref panics (all non-optional call sites use `requireJwtContext` in the same PR).
3. Add **import-graph** and **gofmt** gates to CI so AGENTS.md §7 matches reality.
4. **Remove classic FSD as a production dual-path tax**: one I/O model (gnet) for everyday development and default tests; no `Listen != nil ⇒ classic` coupling.
5. **Close out** sweatbox + apt/air design docs (status, current-state, PR plan truth) without deleting design history.
6. **Populate datafeed honestly** via extended service-HTTP online-users fields (web must not invent FSD state). Remaining zeros documented.

### Non-Goals

- Changing FSD wire bytes (`pkg/protocol`) or VATSIM datafeed schema version.
- Adding a weather / QNH model, military ratings, or multi-line ATIS persistence as a hard requirement of this program (honest zeros / empty arrays are acceptable when no source exists).
- Reintroducing browser automation (Playwright etc.) for design-doc closeout.
- Splitting the single binary or reopening package layout.
- Performance A/B of gnet vs classic as a long-term product feature (verifyperf is optional historical tooling).

---

## Problem Statement (grounded)

### A. Hygiene script subshell bug

`scripts/check-hygiene.sh` sets `failed=0` in the parent shell, then pipes file lists into `scan_files`:

```bash
printf '%s\n' "$files" | scan_files '\bpanic\s*\('
```

`scan_files` assigns `failed=1` when it finds hits. Under bash, the right-hand side of a pipeline runs in a **subshell**, so the parent’s `failed` stays `0`. The script prints panic locations (e.g. the two real sites below) and still exits 0:

```bash
if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
```

That is why hygiene can “fail” visually while CI/local automation thinks it passed.

**Real panic sites** (non-test; confirmed by workspace grep):

| Location | Behavior |
|----------|----------|
| `internal/web/auth.go:321–325` `getJwtContext` | `panic("attempted to load non-existent jwt context")` when gin key missing |
| `internal/session/outbound.go:98–100` `NewCoalesceOutbound` | `panic("session: NewCoalesceOutbound nil writeAsync")` |

**CI gap** (`.github/workflows/ci.yml` `test` job today):

| Step | Present? |
|------|----------|
| `go test -race ./...` | Yes |
| `bash scripts/check-coverage.sh 80` | Yes |
| `bash scripts/check-webjs.sh` | Yes |
| `go build ./cmd/openfsd` | Yes |
| `bash scripts/check-hygiene.sh` | **No** |
| `bash scripts/check-import-graph.sh` | **No** |
| `gofmt -l .` clean | **No** |

AGENTS.md §7 lists hygiene + import-graph + gofmt.

### B. Classic FSD dual path

Production default is gnet (`Server.Run` → `runGnetFSD` when `!useClassicFSD`). Classic is selected in `server.New`:

```go
// internal/server/server.go
useClassic := d.ForceClassicFSD || d.Listen != nil
```

| Consumer | Path |
|----------|------|
| Production / `NewDefault` | gnet (`Listen` nil, `ForceClassicFSD` false) |
| `StartTestServer` (`testserver.go`) | gnet (`Listen` nil, `FSDBound` for bound addr) |
| `TestRunServiceHTTPAndListen` (`bootstrap_test.go`) | classic via injected `Deps.Listen` returning pre-bound `fsdLn` |
| `io_ab_verify_test.go` (`//go:build verifyperf`) | A/B via `ForceClassicFSD` true/false |

Shared post-login / auth helpers live in `conn.go` and are used by **both** paths and by sweatbox:

- `attemptAuthentication`, `enforcePilotPPLRequirement`
- `broadcastAddPacket`, `broadcastDisconnectPacket`
- `sendMotd`, `sendServerTextMessage`
- `sendError`, `sendServerIdent`

Gnet-specific: `gnet_fsd.go` (`fsdEngine`, `attemptAuthGnet` + `gnetLoginConn` adapter, `NewCoalesceOutbound`).

Classic-only:

- `handleConn`, `eventLoop`, `readLoginPackets` (scanner-driven login on `net.Conn`) in `conn.go`
- `runClassicFSD`, `listenLoop` in `server.go`
- `Deps.Listen` meaning “force classic” (today’s coupling)
- `ForceClassicFSD`

`session.SenderWorker` is **not** classic-only: sweatbox synthetic sessions still `go s.SenderWorker()` for sendChan drain (`sweatbox_host.go`).

### C. Stale design docs

| Doc | Header status | Stale claims (examples) |
|-----|---------------|-------------------------|
| `docs/design/sweatbox-integrated-simulator.md` | Draft (rev 7) | Background tables describe pre-implementation integration surfaces; PR plan not marked complete |
| `docs/design/apt-air-editor.md` | Draft (rev 2) | “Format missing”, parsers under `internal/sweatbox`, “JS tests: none” — all obsolete (`pkg/twrfiles` Format*, airport-editor JS + `webjs/` + CI webjs step exist) |

### D. Datafeed INOP / dishonest defaults

`internal/web/data.go` embeds `serviceapi.OnlineUserPilot` / `OnlineUserATC` and adds:

```go
// DatafeedPilot extras (comments say INOP):
PilotRating, MilitaryRating, QnhIHg, QnhMb, FlightPlan *DatafeedFlightplan

// DatafeedATC extras:
TextATIS []string
```

`generateDatafeed` currently hardcodes:

- `PilotRating: 1`, `MilitaryRating: 1` (not “unknown”; **fabricated**)
- `QnhIHg: 29.92`, `QnhMb: 1013` (standard atmosphere, not measured)
- `FlightPlan` never set (always omitted)
- `TextATIS: []string{}` (empty — honest only if no ATIS source)

Service HTTP `GET /online_users` (`handleGetOnlineUsers` in `http_service.go`) populates position/identity/synthetic but **does not** export `FlightPlan`, pilot rating, assigned beacon, or ATIS lines.

Session already has:

- `FlightPlan atomic.String` — **info section only** (`rules:type:tas:dep:…:route`, see `encodeFlightPlanInfo` / `extractFlightplanInfoSection`)
- `AssignedBeaconCode atomic.String` — set by `$CQ` BC / `#PC` paths
- **No** `PilotRating` on `LoginData` / `Session` (DB `User.PilotRating` is checked at login for PPL gate then discarded)
- **No** stored multi-line controller ATIS (`NEWATIS` / `NEWINFO` are forwarded only)

Import graph constraint: `internal/web` → `serviceapi` OK; web must **not** import `server` / `session`.

---

## Proposed Design

### Part A — Hygiene, panics, CI gates

#### A.1 Fix `scripts/check-hygiene.sh`

**Root cause:** pipeline subshell mutates a copy of `failed`.

**Normative fix:** convert **every** `scan_files` invocation that today uses a pipeline (`printf … | scan_files` or equivalent). The script currently has **five** such call sites:

1. `panic(` scan over `pkg/` + `internal/`
2. `reflect` scan under `pkg/protocol`
3. `fmt.Print*` scan
4. `log.Print*` scan
5. `log.Fatal*` / `log.Panic*` scan

Leaving any `… | scan_files` intact reintroduces the same subshell bug for that pattern class (e.g. a future `fmt.Print` would print and still exit 0).

**Recommended form (process substitution)** — apply the same shape to all five:

```bash
if scan_files '\bpanic\s*\(' < <(printf '%s\n' "$files"); then
  echo "    OK"
fi
```

Under bash, process substitution feeds stdin **without** putting the function in a pipeline subshell, so `failed=1` sticks in the parent.

**Alternatives** (also acceptable if applied to all sites): temp file / path array with parent-owned iteration; `scan_files` only echoes hits and returns 1 while the parent sets `failed`.

Keep `set -euo pipefail` and the existing `if scan_files …; then` pattern so a non-zero return does not abort early before other pattern classes run (or until the final `failed` check).

**Manual self-check (once after the fix, not a permanent poison file):** introduce a temporary non-test `panic(` under `internal/`, run the script, expect exit 1 and a printed hit; revert the poison. Comment in the script may point maintainers at this check.

Do **not** weaken patterns to ignore real panics.

#### A.2 `getJwtContext` — fail closed without panic

**File:** `internal/web/auth.go` + **every** consumer under `internal/web/`

**Current:**

```go
func getJwtContext(c *gin.Context) (claims *auth.CustomClaims) {
	val, exists := c.Get(jwtContextKey)
	if !exists {
		panic("attempted to load non-existent jwt context")
	}
	claims = val.(*auth.CustomClaims)
	return
}
```

**Binding requirement for PR1:** removing the `panic(` token alone is **not** sufficient. Call sites that do `claims := getJwtContext(c)` then `claims.NetworkRating` / `claims.CID` would become **nil pointer panics** if middleware is mis-wired. Hygiene greps only for `panic(`, so CI would be green while handlers remain unsafe. **All non-optional call sites must be updated in PR1** — no follow-up PR for “the rest of the call sites.”

**Recommended API:**

```go
// getJwtContext returns session/bearer claims set by requireSessionHTML /
// jwtBearerMiddleware. Returns nil if missing or wrong type (never panics).
func getJwtContext(c *gin.Context) *auth.CustomClaims

// requireJwtContext is the non-optional handler path: aborts with 500 and
// returns false when claims are missing. HTML and JSON handlers both use it;
// API handlers may map the abort to existing JSON error helpers if needed.
func requireJwtContext(c *gin.Context) (*auth.CustomClaims, bool) {
	claims := getJwtContext(c)
	if claims == nil {
		// Prefer slog.Error once per request path (not per field access).
		c.AbortWithStatus(http.StatusInternalServerError)
		return nil, false
	}
	return claims, true
}
```

On missing/wrong type in `getJwtContext`: return `nil` only (used by optional/debug paths).

**Call-site contract (normative for PR1):**

| Context | Required change |
|---------|-----------------|
| Handlers behind `requireSessionHTML` / `jwtBearerMiddleware` that need claims | `claims, ok := requireJwtContext(c); if !ok { return }` — **do not** continue with nil |
| `requireMinRatingHTML` | Same — use `requireJwtContext` (or abort 500 if nil) before comparing rating |
| Already nil-tolerant optional paths (`pages_airport_editor.go` echo-download: `if claims := getJwtContext(c); claims != nil`) | May keep `getJwtContext` + explicit nil check (logging CID only) |
| API handlers that already check `claims == nil` | Keep explicit check **or** migrate to `requireJwtContext` for one style |

**Grep-driven checklist (touch every site in PR1):** `rg 'getJwtContext' internal/web` — expect ~24 call sites across `auth.go` (`requireMinRatingHTML`), `pages_*.go`, `config.go`, `user.go`, `api_*.go`, `fsdconn.go`, etc. After PR1, every non-optional site uses `requireJwtContext` (or equivalent abort + bool). No bare `getJwtContext` followed by unconditional field access.

**Tests (PR1, required):**

1. Empty gin context → `getJwtContext` returns nil (no panic).
2. Handler/middleware path without claims set → 500/abort **without** panic (table or direct `requireJwtContext` / thin handler).
3. Existing PE/API tests with middleware stay green.

#### A.3 `NewCoalesceOutbound` — no panic

**File:** `internal/session/outbound.go`  
**Call site:** `internal/server/gnet_fsd.go` (~292) always passes non-nil `writeAsync`.

**Recommended API** (explicit, test-friendly, no production panic):

```go
func NewCoalesceOutbound(writeAsync AsyncWriteFunc, closeFn func() error, cfg CoalesceOutboundConfig) (*CoalesceOutbound, error)
```

- `writeAsync == nil` → `return nil, errors.New("session: NewCoalesceOutbound nil writeAsync")` (or a package-level sentinel `ErrNilWriteAsync`).
- Call site: if err != nil, log + close connection (`gnet.Close`) — defensive; should never fire in production.
- All tests/benchmarks updated for two-value return.

**Rejected alternatives:**

| Option | Why not preferred |
|--------|-------------------|
| No-op sink on nil | Silent data loss; hides wiring bugs |
| Keep panic | Violates AGENTS.md §10; hygiene must fail until gone |
| `MustNewCoalesceOutbound` + panic only in test helpers | Extra surface; production constructor still needs safe form |

#### A.4 CI steps

Add to `.github/workflows/ci.yml` `test` job **before** or **after** race tests (order preference: gofmt → hygiene → import-graph → race → coverage → webjs → build — cheap checks first):

```yaml
- name: gofmt
  run: test -z "$(gofmt -l .)"
- name: Hygiene (no panic/reflect/fmt.Print in library code)
  run: bash scripts/check-hygiene.sh
- name: Import graph
  run: bash scripts/check-import-graph.sh
```

No new tools. Stress job remains optional.

---

### Part B — Classic FSD dual-path cleanup

#### Recommendation: **full removal of classic accept/event path** (lowest ongoing tax)

Everyday development and default CI already exercise **gnet only** via `StartTestServer`. Classic survives for:

1. One bootstrap inject test (`TestRunServiceHTTPAndListen`)
2. Optional verifyperf A/B (`ForceClassicFSD`)

Keeping a “narrow classic” forever still means every login/lifecycle change risks classic/gnet drift (`handleConn` vs `finishLogin`). Full removal is lower long-term risk if the two consumers are rewritten/deleted carefully.

**Do not** remove shared helpers that gnet + sweatbox need.

#### What to delete

| Symbol / surface | File | Notes |
|------------------|------|-------|
| `handleConn` | `conn.go` | Classic accept worker |
| `eventLoop` | `conn.go` | Classic post-login read loop |
| `readLoginPackets` | `conn.go` | Scanner login; gnet uses `parseLoginPackets` on buffered lines |
| `runClassicFSD` | `server.go` | |
| `listenLoop` | `server.go` | |
| `Server.useClassicFSD` field + branch in `Run` | `server.go` | Always `runGnetFSD` |
| `Server.listen` field | `server.go` | Only classic accept loop used it; gnet does not |
| `Deps.Listen` | `deps.go` | Only classic used it for FSD bind inject |
| `ForceClassicFSD` | `deps.go` | |
| `defaultListen` | `deps.go` | Classic-only helper; delete with `Deps.Listen` |
| Classic arm of verifyperf | `io_ab_verify_test.go` | See below |
| Stale classic godoc | `server.go`, `config.go`, `gnet_fsd.go`, kick/sweatbox comments | e.g. `FsdNumEventLoop` “only when Deps.Listen is nil” in `config.go` |

#### What to keep (possibly rename/move for clarity)

| Symbol | Consumers |
|--------|-----------|
| `parseLoginPackets` (`login_parse.go`) | gnet `finishLogin`, unit tests |
| `attemptAuthentication`, `enforcePilotPPLRequirement` | gnet `attemptAuthGnet`, unit tests |
| `broadcastAddPacket`, `broadcastDisconnectPacket` | gnet, handlers, sweatbox |
| `sendMotd`, `sendServerTextMessage`, `sendError`, `sendServerIdent` | gnet login + MOTD |
| `gnetLoginConn` / `attemptAuthGnet` | gnet only — keep |
| `session.SenderWorker` | **sweatbox synthetics** + any channel-path tests |
| `session.NewCoalesceOutbound` | gnet only |
| `Deps.HTTPListen`, `Deps.FSDBound` | tests + service HTTP inject |

Optional cleanup (same PR or follow-up): rename `conn.go` → `login_lifecycle.go` (or split auth/broadcast) so the file is not named for a deleted path. Not required for correctness.

#### Docs / comments

- Update `Server` godoc in `server.go` (“classic when Listen injected…” → gnet-only description).
- Update `internal/server/config.go` godoc that still says event-loop count applies “only when Deps.Listen is nil” (or similar) — gnet is always on.
- Update comments in `gnet_fsd.go`, `http_service.go` kick, sweatbox that say “handleConn defer” to “gnet disconnect / synthetic cleanup”.
- AGENTS.md does not mandate classic path; no package table change required.

#### Test rewiring

**`TestRunServiceHTTPAndListen` (`bootstrap_test.go`):**

Today injects `Listen` → classic, sleeps 50ms, dials FSD TCP briefly, cancels.

Rewrite pattern to match `StartTestServer` (no classic, **no sleep-only readiness**):

- `Listen` / `ForceClassicFSD` absent from `Deps`
- `FSDBound: make(chan string, N)` to learn bound gnet address (including `:0`)
- Keep `HTTPListen` pre-bound listener inject (unchanged; service HTTP is independent of classic)
- **Readiness:** `select` on `FSDBound` receive, `Run` error channel, and a timeout — same style as `StartTestServer` (including any `0.0.0.0` → `127.0.0.1` rewrite used there). **Forbidden:** sole readiness gate of `time.Sleep(50 * time.Millisecond)` before dial.
- Dial bound FSD addr to prove accept path; cancel context; wait for `Run` return

**`StartTestServer`:** already gnet — no change required.

**`io_ab_verify_test.go` (build tag `verifyperf`):**

Options (pick one in PR description; recommend **1**):

1. **Delete classic arm** — rename to gnet-only perf smoke (`TestIO_GnetBaseline`) or delete the file if the A/B has served its purpose.
2. **Delete entire verifyperf test** if maintainers no longer run it.
3. **Keep file but only gnet** — remove `ForceClassicFSD` parameter from helper.

Do **not** keep classic solely for verifyperf after production dual-path is gone.

**Handler / security tests** that call `attemptAuthentication` / `broadcast*` directly: unchanged (no TCP classic loop).

#### Narrow-classic intermediate (optional micro-PR)

If full removal is too large for one review:

1. **PR B0:** `useClassic := d.ForceClassicFSD` only (`Listen != nil` no longer forces classic). Document that `Listen` is unused/ignored unless `ForceClassicFSD`. Update bootstrap test to use gnet + `FSDBound` (or set `ForceClassicFSD: true` temporarily).
2. **PR B1:** delete classic code + `ForceClassicFSD` + verifyperf classic arm.

Prefer **single full-removal PR** after A is green if the diff stays reviewable (mostly deletions + one bootstrap rewrite).

#### Risk controls for Part B

- Race-clean `go test -race ./internal/server/...` including e2e.
- Manual smoke: `openfsd -fsd` accepts pilot login (gnet).
- Confirm sweatbox e2e still registers synthetics + `SenderWorker` drain.
- Confirm `Deps.Listen` removal does not break external forks (in-tree only consumer is tests).

---

### Part C — Design-doc closeout

Do **not** delete design history. Edit headers, add an **Implementation status** section near the top (after Overview or after the metadata table), and retcon “current state” tables.

#### C.1 `docs/design/sweatbox-integrated-simulator.md`

| Edit | Content |
|------|---------|
| **Status** | `Implemented` (note ship date or “landed P0+P1; see Implementation status”) |
| **Implementation status** | New section: pure engine in `internal/sweatbox`; host/HTTP in `internal/server/sweatbox_*.go`; web instructor UI + PE; Synthetic sessions; service HTTP control plane; coverage floor ≥95% on sweatbox; e2e present |
| **Background “current state”** | Prefixed as historical pre-implementation snapshot, **or** updated to match tree (prefer short “As of implementation” table + leave long motivation) |
| **PR Plan** | Mark each planned PR **Done** / **Cancelled** with one-line notes (P0a–c, P1 pattern, etc.) |
| **Open Questions** | Already mostly resolved; ensure product decisions (Admin-only, CID 900001, max aircraft, datafeed includes synthetics) stay marked resolved |

#### C.2 `docs/design/apt-air-editor.md`

| Edit | Content |
|------|---------|
| **Status** | `Implemented` |
| **Implementation status** | `pkg/twrfiles` Parse+Format+fixtures; sweatbox aliases/wrappers; `/airport-editor` MPA + Leaflet PE; `webjs/` Node tests + `scripts/check-webjs.sh` in CI; validate API via `pkg/twrfiles`; no durable server persistence; Playwright cancelled |
| **Background current-state table** | Replace stale rows: Format **present** (`format_apt.go` / `format_air.go`); parsers live in `pkg/twrfiles` (not only sweatbox); JS tests under `webjs/` |
| **PR Plan** | PR1–8 **Done**; PR9 Playwright **Cancelled** (already noted) |
| **Open Questions** | Mark Supervisor access / undo as deferred v1.1 or product-default (Admin-only shipped) |

No code changes required for Part C unless a doc references a wrong path that confuses implementers — fix those paths in the same PR.

---

### Part D — Datafeed honest population

#### Design principle

**FSD process is source of truth.** Web builds the public datafeed from `GET /online_users` (+ local config for server ident). Web must not invent ratings, QNH, or flight plans.

#### D.1 Extend `internal/serviceapi` DTOs

**File:** `internal/serviceapi/online_users.go`

```go
type OnlineUserPilot struct {
	OnlineUserGeneralData
	Altitude    int    `json:"altitude"`
	Groundspeed int    `json:"groundspeed"`
	Heading     int    `json:"heading"`
	Transponder string `json:"transponder"`
	Synthetic   bool   `json:"synthetic,omitempty"`

	// PilotRating is the VATSIM pilot rating wire ID from the certificate
	// at login (0,1,3,7,15,31,63). 0 if unknown (e.g. synthetic without DB).
	PilotRating int `json:"pilot_rating"`

	// FlightPlan is the session info-section string (no $FP source/dest),
	// empty if none filed. Same layout as session.FlightPlan / encodeFlightPlanInfo.
	FlightPlan string `json:"flight_plan,omitempty"`

	// AssignedBeaconCode is the ATC-assigned squawk if set; may be empty.
	AssignedBeaconCode string `json:"assigned_beacon_code,omitempty"`
}

type OnlineUserATC struct {
	OnlineUserGeneralData
	Frequency string `json:"frequency"`
	Facility  int    `json:"facility"`
	VisRange  int    `json:"visual_range"`

	// TextATIS is multi-line controller ATIS when the server stores it.
	// Empty when not available (openfsd does not persist NEWINFO by default).
	TextATIS []string `json:"text_atis,omitempty"`
}
```

Omitempty on empty flight plan / ATIS keeps dashboard/online_users payloads compact. `pilot_rating` should always be present as a number (0 is valid P0).

#### D.1b Datafeed embed / JSON shadowing rule (normative for PR5)

`DatafeedPilot` / `DatafeedATC` today **embed** `serviceapi.OnlineUserPilot` / `OnlineUserATC` and declare outer fields with the **same JSON tags** as the embed (`pilot_rating`, `flight_plan`, `text_atis`). Go’s `encoding/json` prefers the **outer** field for a given name; the embedded value is ignored for that tag.

Verified footguns if left careless:

| Outer field left at zero-value | Resulting JSON |
|--------------------------------|----------------|
| Outer `PilotRating` = 0 | `"pilot_rating":0` even when embed has real rating |
| Outer `FlightPlan` = nil + omitempty | `flight_plan` **omitted** even when embed has a non-empty **string** |

**Required `DatafeedPilot` / `DatafeedATC` shape after PR5:**

```go
type DatafeedPilot struct {
	serviceapi.OnlineUserPilot // promotes general/position/synthetic/pilot_rating/assigned_beacon_code
	// Do NOT redeclare PilotRating — embed owns json:"pilot_rating".

	Server         string              `json:"server"`
	MilitaryRating int                 `json:"military_rating"` // always 0: not stored
	QnhIHg         float64             `json:"qnh_i_hg"`        // always 0: no weather model
	QnhMb          int                 `json:"qnh_mb"`          // always 0: no weather model

	// Intentional shadow: service-HTTP carries a string info section;
	// public datafeed wants a VATSIM-shaped object (or omit when nil).
	FlightPlan *DatafeedFlightplan `json:"flight_plan,omitempty"`
}

type DatafeedATC struct {
	serviceapi.OnlineUserATC // promotes general + text_atis from serviceapi
	// Do NOT redeclare TextATIS — embed owns json:"text_atis".

	Server string `json:"server"`
}
```

**Reviewer reject rule:** any PR that “simplifies” by only embedding and forgetting to map `FlightPlan`, or that reintroduces an outer `PilotRating` / outer `TextATIS` that can zero-out honest embed values, is incorrect.

**Alternative (also acceptable, more verbose):** stop embedding; copy general/position fields explicitly into `DatafeedPilot` so service-HTTP string vs datafeed object never share a tag path. Prefer the embed + intentional `FlightPlan` shadow above (less churn).

#### D.2 Session: carry pilot rating after login

**Problem:** `User.PilotRating` is loaded in `attemptAuthentication` for the PPL gate but not stored on the session.

**Change:**

1. Add `PilotRating int` to `session.LoginData` (immutable after login; document in field-ownership comment).
2. In `attemptAuthentication` (both password and JWT branches after user load), set `client.PilotRating = user.PilotRating` (validate with `protocol.IsValidPilotRating`; if invalid, store `0`).
3. Sweatbox `buildSession`: leave `0` (or document synthetic as P0) — no DB user for default sweatbox CID.
4. `handleGetOnlineUsers`: copy `client.PilotRating`, `client.FlightPlan.Load()`, `client.AssignedBeaconCode.Load()` into DTO.

No new DB query on the online_users hot path.

#### D.3 Flight plan → `DatafeedFlightplan` mapping (web)

**File:** `internal/web/data.go` (pure mapping helper + tests)

Session / serviceapi info section field order (from `encodeFlightPlanInfo` / protocol Flight Plan body after SOURCE:DEST):

| Index | Field | DatafeedFlightplan |
|------:|-------|--------------------|
| 0 | rules | `FlightRules` |
| 1 | type/equipment | `Aircraft`, also seed `AircraftShort` / `AircraftFAA` as best-effort (same string if no FAA expansion) |
| 2 | TAS | not a first-class datafeed field — omit or ignore |
| 3 | dep | `Departure` |
| 4 | ETD | `DepTime` |
| 5 | ATD | unused in datafeed (leave out) |
| 6 | cruise alt | not separate in DatafeedFlightplan — omit (VATSIM feed often has no cruise field in this struct) |
| 7 | dest | `Arrival` |
| 8–9 | enroute h/m | `EnrouteTime` via **`formatHHMM(hre, mre)`** (below) |
| 10–11 | fuel h/m | `FuelTime` via **`formatHHMM(hfuel, mfuel)`** |
| 12 | alternate | `Alternate` |
| 13 | remarks | `Remarks` |
| 14 | route | `Route` |
| — | assigned beacon | `AssignedTransponder` from `AssignedBeaconCode` (fallback empty) |
| — | revision | `RevisionID` = **0** (not stored) |

**`formatHHMM` contract (normative):** sweatbox/`encodeFlightPlanInfo` emits single-digit `"0"` for hours/minutes, not `"00"`. Do **not** string-concatenate raw fields.

```go
// parseNonNegIntField: empty or non-numeric → 0; negative → 0.
// formatHHMM(hoursField, minutesField) → fmt.Sprintf("%02d%02d", h, m)
```

| hours field | minutes field | `formatHHMM` result |
|-------------|---------------|---------------------|
| `"0"` | `"0"` | `"0000"` |
| `"2"` | `"5"` | `"0205"` |
| `"2"` | `"40"` | `"0240"` |
| `""` / missing | `""` / missing | `"0000"` |
| `"x"` | `"3"` | `"0003"` (non-numeric hours → 0) |

Rules:

- If serviceapi `FlightPlan == ""`, set outer `DatafeedPilot.FlightPlan` **nil** (omitempty) — intentional shadow means the string is **not** marshaled as `flight_plan`.
- If non-empty but fewer fields than expected, map what is present; do not invent routes/airports.
- Mapping is **best-effort** for VATSIM-shaped consumers; openfsd does not claim full VATSIM datafeed parity.

Place parser in **web** (pure helper + table tests). Keep `serviceapi` DTO-only.

#### D.4 QNH, military rating, TextATIS

| Field | Source today | Honest value |
|-------|--------------|--------------|
| `QnhIHg` / `QnhMb` | None (no weather model on session) | **0** / **0** — stop shipping 29.92/1013 as if real |
| `MilitaryRating` | Not stored anywhere | **0** |
| `PilotRating` | DB at login → session → online_users | Real ID |
| `FlightPlan` | session atomic | Mapped object or omit |
| `TextATIS` | Not stored (NEWINFO forward-only) | **`[]string{}`** or omit empty |

Optional **follow-up** (not required for this program’s honesty goal): store last NEWINFO payload lines on ATC `Session` when handling `$CQ` NEWINFO, export via `OnlineUserATC.TextATIS`. That is a product enhancement with protocol semantics; out of scope unless PO expands scope.

#### D.5 `generateDatafeed` changes

Resolve server ident once (same source as servers.json — already in-tree via `getFsdServerInfo` / `ConfigFsdServerIdent`). Fallback `"OPENFSD"` only on config error so datafeed is not the outlier.

```go
serverIdent, _, _, err := s.getFsdServerInfo()
if err != nil || serverIdent == "" {
	serverIdent = "OPENFSD"
}

for _, pilot := range onlineUsers.Pilots {
	dp := DatafeedPilot{
		OnlineUserPilot: pilot, // pilot_rating comes from embed — do not set outer PilotRating
		Server:          serverIdent,
		MilitaryRating:  0,
		QnhIHg:          0,
		QnhMb:           0,
		// Intentional shadow: string → object (nil when pilot.FlightPlan == "").
		FlightPlan: mapInfoSectionToDatafeedFP(pilot.FlightPlan, pilot.AssignedBeaconCode),
	}
	// ...
}
for _, atc := range onlineUsers.ATC {
	dataFeed.ATC = append(dataFeed.ATC, DatafeedATC{
		OnlineUserATC: atc, // text_atis from embed when present
		Server:        serverIdent,
		// no outer TextATIS
	})
}
```

Remove comments that say “INOP placeholder” once fields are honest (replace with “always 0: no model” where applicable).

#### D.6 Tests

| Layer | Cases |
|-------|--------|
| `session` / server auth unit | After successful auth, `client.PilotRating` matches user |
| `handleGetOnlineUsers` unit | Snapshot includes flight plan **string** + pilot rating + beacon |
| `web` pure map helper | Table-driven info section → `DatafeedFlightplan`; empty → nil; `formatHHMM` cases in D.3 |
| `web` **JSON marshal** of `DatafeedPilot` | Build from `OnlineUserPilot` with non-zero `PilotRating` + non-empty plan string; assert marshaled JSON has real `"pilot_rating"` and object `"flight_plan"` (not omitted, not a JSON string). Also assert no outer zero-rating when embed is set. |
| `web` generateDatafeed | Stubbed online users: no fabricated QNH 29.92; `server` matches config ident when available |
| e2e (optional but valuable) | File `$FP`, poll online_users or datafeed path if test harness allows |

Dashboard consumers of online_users should ignore unknown JSON fields; adding fields is backward compatible.

---

## Alternatives Considered

### Hygiene failed flag

| Alternative | Trade-off |
|-------------|-----------|
| Process substitution (chosen) | Minimal diff; keeps scan_files |
| Rewrite without pipeline | Clearer; more churn |
| `PIPESTATUS` after pipe | Fragile with `set -o pipefail` + multiple greps inside |

### CoalesceOutbound constructor

| Alternative | Trade-off |
|-------------|-----------|
| `(*T, error)` (chosen) | Explicit; race-clean tests update easily |
| No-op sink | Silent loss |
| Panic in `Must*` only | Still need safe path for library |

### Classic FSD

| Alternative | Trade-off |
|-------------|-----------|
| **Full removal** (chosen) | One I/O model; deletes dual maintenance; rewires one test + verifyperf |
| Narrow: `ForceClassicFSD` only | Smaller first PR; still pays dual tax until B1 |
| Keep classic forever for inject Listen | Continuous drift risk; gnet already has `FSDBound` for `:0` |

### Datafeed flight plans via DB

| Alternative | Trade-off |
|-------------|-----------|
| **Session → service HTTP → web** (chosen) | Live wire state; ATC `$AM` already updates session |
| Web re-queries DB | No FP in DB; violates “don’t invent FSD state” |
| New dedicated `/datafeed_snapshot` HTTP | Extra surface; online_users already exists |

### TextATIS persistence

| Alternative | Trade-off |
|-------------|-----------|
| Empty until future PR (chosen for honesty now) | No protocol inventing |
| Parse NEWINFO into session in this program | Larger server change; defer |

---

## Key Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | Fix hygiene via **process substitution on all five `scan_files` call sites** (or equivalent non-pipeline parent mutation) | Parent must see `failed`; partial conversion reintroduces the bug |
| 2 | **`getJwtContext` returns nil**; **`requireJwtContext` at every non-optional call site in PR1** (not a follow-up) | Avoid replacing `panic(` with nil-deref panics; AGENTS.md §10 |
| 3 | **`NewCoalesceOutbound` → `(*CoalesceOutbound, error)`** | Loud in tests, no production panic |
| 4 | CI gains **gofmt + hygiene + import-graph** | AGENTS.md §7 parity |
| 5 | **Full classic FSD removal** (not perpetual narrow dual path) | Lowest ongoing maintenance; tests already gnet-default |
| 6 | Keep **shared login/broadcast helpers** and **SenderWorker for synthetics** | Not classic-only |
| 7 | Delete **`Deps.Listen` / `ForceClassicFSD` / `Server.listen` / `Server.useClassicFSD` / `defaultListen`** with classic code | Remove coupling entirely |
| 8 | verifyperf **drops classic arm** (or whole file) | No reason to keep classic for A/B alone |
| 9 | Design docs → **Implemented** + Implementation status; history retained | Closeout without amnesia |
| 10 | Datafeed fields from **extended online_users**, not web invention | Import graph + SoT |
| 11 | Store **`PilotRating` on LoginData** at auth | Avoid online_users DB hits; already loaded |
| 12 | Export **FlightPlan info section** + map in web | Session already stores it |
| 13 | **QNH / military = 0**; **TextATIS empty** unless stored later | Honesty over fake standards |
| 14 | Stop hardcoding **PilotRating/MilitaryRating = 1** | Those were false signals |
| 15 | Classic removal is its **own PR** after hygiene/CI | Isolate highest-risk change |
| 16 | No Playwright / no SPA / no package layout change | House rules |
| 17 | **Datafeed embed:** drop outer `PilotRating` and outer `TextATIS`; keep intentional outer `FlightPlan *DatafeedFlightplan` shadow; marshal unit test required | `encoding/json` outer-field preference otherwise zeros/omits honest embed values |
| 18 | Datafeed `server` field from **`ConfigFsdServerIdent`** via `getFsdServerInfo` (fallback `"OPENFSD"`) in PR5 | Match servers.json honesty; stop hardcoding |

---

## Open Questions

None that block implementation. Product defaults already taken:

- Classic path: full removal (this doc).
- Datafeed synthetics: remain included when sweatbox is enabled (sweatbox design decision).
- TextATIS persistence: deferred; empty is honest.
- Datafeed `server` field: **`ConfigFsdServerIdent`** in PR5 (fallback `"OPENFSD"`) — resolved; not polish-deferred.

---

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Hygiene fix suddenly fails CI on residual violations | Low | Only two panics; fix panics in PR1, then CI in PR2 |
| Missed nil check after `getJwtContext` change → nil deref panics | **High** | PR1 **must** migrate every non-optional call site to `requireJwtContext`; grep checklist; empty-context + no-middleware abort tests |
| Partial hygiene pipeline conversion | Med | Convert **all five** `scan_files` sites; manual poison-file self-check |
| Classic removal leaves dead `Server.listen` / config godoc | Low | Explicit delete checklist includes fields + `config.go` comments |
| gnet-only bootstrap test flakes on bind race | Med | Select on `FSDBound` / Run error / timeout (like `StartTestServer`); **forbid sleep-only readiness** |
| online_users JSON growth | Low | omitempty on empty FP/ATIS |
| Datafeed consumers assume QNH always 1013 | Low | Document 0; fake 29.92 was never real METAR |
| Embed shadowing reintroduces zero `pilot_rating` / omitted FP | **High** | D.1b shape + marshal unit test; PR review reject rule |
| FP field mapping mismatches VATSIM clients | Med | `formatHHMM` contract + table tests; omit rather than invent |
| Design-doc edit conflicts with ongoing work | Low | Docs-only PR |

---

## Implementation notes (file-level checklist)

### Part A files

- `scripts/check-hygiene.sh` — convert **all five** `scan_files` pipelines to process substitution (or parent-owned iteration)
- `internal/web/auth.go` — `getJwtContext` (nil) + `requireJwtContext` (abort + bool)
- **All** `internal/web` consumers of `getJwtContext` — grep-driven; non-optional → `requireJwtContext`
- `internal/web/auth_test.go` (or existing auth/PE tests) — empty context nil; requireJwtContext aborts without panic
- `internal/session/outbound.go` + `outbound_test.go` + `outbound_bench_test.go` + `session_test.go`
- `internal/server/gnet_fsd.go` — handle constructor error
- `.github/workflows/ci.yml` — three gates (PR2)

### Part B files

- `internal/server/server.go` — delete classic run path; delete `Server.listen`, `Server.useClassicFSD`; always `runGnetFSD`
- `internal/server/conn.go` — delete handleConn/eventLoop/readLoginPackets; keep shared helpers
- `internal/server/deps.go` — remove `Listen`, `ForceClassicFSD`, `defaultListen`
- `internal/server/config.go` — fix godoc that references classic / `Deps.Listen` for event loops
- `internal/server/bootstrap_test.go` — gnet rewrite with `FSDBound` select (no sleep-only readiness)
- `internal/server/io_ab_verify_test.go` — classic arm or file removal
- Comment touch-ups: `gnet_fsd.go`, `http_service.go`, sweatbox host

### Part C files

- `docs/design/sweatbox-integrated-simulator.md`
- `docs/design/apt-air-editor.md`

### Part D files

- `internal/session/session.go` — LoginData.PilotRating
- `internal/server/conn.go` — set rating in attemptAuthentication
- `internal/server/http_service.go` — populate extended fields
- `internal/serviceapi/online_users.go` — DTO fields
- `internal/web/data.go` — **embed shadowing shape (D.1b)**; `formatHHMM` + mapper; `generateDatafeed` uses config server ident
- Tests: server online_users; web mapper + **JSON marshal** of `DatafeedPilot`; auth rating sticky
- Sweatbox host: explicit PilotRating 0 on synthetic LoginData if field added

**PR5 reviewer reject:** outer `PilotRating` or outer `TextATIS` reintroduced; outer `FlightPlan` not mapped when serviceapi string non-empty; hardcoded `"OPENFSD"` when config ident is available.

---

## PR Plan

Independently mergeable; order matters where noted. Each PR: `gofmt`, `go test -race ./...` (or package-scoped + full before merge), coverage floors when touching measured packages.

### PR1 — Remove library panics + fail-closed JWT call sites

| | |
|--|--|
| **Title** | `hygiene: remove panics from web JWT helper and CoalesceOutbound` |
| **Depends on** | — |
| **Files** | `internal/web/auth.go`; **every** `internal/web` file that calls `getJwtContext` (pages, API, config, user, fsdconn, `requireMinRatingHTML`); JWT/auth unit tests; `internal/session/outbound.go`, `outbound_*test.go`, `session_test.go`; `internal/server/gnet_fsd.go` |
| **Description** | `getJwtContext` returns nil; **`requireJwtContext` at every non-optional call site in this PR** (success criterion is fail-closed without any panic, not merely deleting the `panic(` token). `NewCoalesceOutbound` returns `(*CoalesceOutbound, error)`. Grep checklist: no bare `getJwtContext` + unconditional field access. Tests: empty context → nil; missing claims → 500/abort without panic; existing middleware paths green. Zero `panic(` hits under non-test pkg/internal after this PR. |

### PR2 — Fix hygiene script + CI gates (gofmt, hygiene, import-graph)

| | |
|--|--|
| **Title** | `ci: fix check-hygiene.sh exit status; gate gofmt, hygiene, import-graph` |
| **Depends on** | PR1 (so hygiene is green) |
| **Files** | `scripts/check-hygiene.sh`, `.github/workflows/ci.yml` |
| **Description** | Convert **all five** `scan_files` invocations to process substitution (or equivalent parent-owned iteration). CI runs `test -z "$(gofmt -l .)"`, `bash scripts/check-hygiene.sh`, `bash scripts/check-import-graph.sh`. Cheap checks before race suite. Manual once: poison non-test panic → script exit 1. |

### PR3 — Classic FSD full removal

| | |
|--|--|
| **Title** | `server: remove classic FSD accept/event path; gnet only` |
| **Depends on** | PR2 recommended (CI hygiene green makes review safer); can follow PR1 if needed |
| **Files** | `internal/server/server.go` (incl. `listen` / `useClassicFSD` fields), `conn.go`, `deps.go` (`Listen`, `ForceClassicFSD`, `defaultListen`), `config.go` godoc, `bootstrap_test.go`, `io_ab_verify_test.go` (or delete), comment fixes in `gnet_fsd.go` / kick / sweatbox; optional rename of `conn.go` |
| **Description** | Delete classic accept/event path and all related Server/Deps fields. Always `runGnetFSD`. Rewrite `TestRunServiceHTTPAndListen` to gnet + `FSDBound` select readiness (**no sleep-only**) + `HTTPListen`. Drop verifyperf classic arm. Keep auth/broadcast/MOTD helpers and synthetic `SenderWorker`. Full server e2e + sweatbox e2e green. |

### PR4 — Design-doc closeout (docs only)

| | |
|--|--|
| **Title** | `docs: mark sweatbox and apt/air editor designs Implemented` |
| **Depends on** | — (parallelizable anytime; ideally after PR3 if docs mention classic/handleConn) |
| **Files** | `docs/design/sweatbox-integrated-simulator.md`, `docs/design/apt-air-editor.md` |
| **Description** | Status → Implemented; Implementation status section; fix stale current-state tables; mark PR plans Done/Cancelled; retain design history. No code. |

### PR5 — Datafeed honesty via online_users extension

| | |
|--|--|
| **Title** | `datafeed: export pilot rating and flight plan via online_users; stop fabricating QNH` |
| **Depends on** | — (independent of classic removal; can parallel PR3/PR4) |
| **Files** | `internal/session/session.go`, `internal/server/conn.go` (auth set rating), `internal/server/http_service.go`, `internal/serviceapi/online_users.go`, `internal/web/data.go` (embed shape D.1b, `formatHHMM`, generateDatafeed), tests under server/web; sweatbox `buildSession` zero rating |
| **Description** | Extend serviceapi pilot/ATC DTOs; populate from session snapshot. Datafeed: **drop outer PilotRating / TextATIS**; intentional `FlightPlan *object` shadow; MilitaryRating/QNH 0; `Server` from `ConfigFsdServerIdent` (fallback OPENFSD). Unit tests for mapper, `formatHHMM`, and **JSON marshal** (real pilot_rating + object flight_plan). Remove fake defaults of 1 and 29.92/1013. |

### Optional PR6 — ATC TextATIS capture (defer)

| | |
|--|--|
| **Title** | `server: persist NEWINFO lines for datafeed text_atis` |
| **Depends on** | PR5 |
| **Files** | session field or atomic, `handler_query.go`, online_users population, tests |
| **Description** | Only if product wants non-empty `text_atis` on the **serviceapi** DTO (embed path — still no outer DatafeedATC field). Out of core program unless PO prioritizes. |

---

## Success criteria

1. `bash scripts/check-hygiene.sh` exits **1** if a non-test `panic(` is introduced; exits **0** on clean tree; **all** pattern classes honor `failed` in the parent shell.
2. Zero `panic(` under non-test `pkg/` + `internal/`; JWT handlers fail closed without nil-deref panics.
3. CI fails on gofmt drift, hygiene hits, or forbidden imports.
4. No classic FSD code path; no dead `Server.listen` / `useClassicFSD` / `Deps.Listen`; `Server.Run` always gnet; tests green under `-race`; bootstrap readiness is not sleep-only.
5. Design docs Status **Implemented** with accurate implementation status.
6. Datafeed JSON: real `pilot_rating` when known (via embed, not outer zero); `flight_plan` **object** when session has FP; QNH/military not falsely non-zero; `server` from config ident when available; web still does not import `server`/`session`.
