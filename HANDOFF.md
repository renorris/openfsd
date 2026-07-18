# HANDOFF — FSD server efficiency pass (gnet + write path)

**First action for the next agent:** read this entire file carefully, then **delete `HANDOFF.md` immediately** (do not leave it in the tree; remove it in your first commit or before you start coding). Do not commit this file as permanent docs.

---

## Context

Branch: **`dev`** (should be up to date with `origin/dev` after the postoffice slab rewrite).

### Just landed (postoffice)

`internal/postoffice` was rewritten off the dual linear/R-tree path:

- Lock-free **atomic live slabs** + free-list tombstones (not O(N) COW snapshots).
- **`UpdatePosition`** is O(1): session `SetGeo` only (no postoffice lock / no index rewrite).
- **Search / SearchATC / All** scan slabs lock-free with **cached VisBox** AABBs.
- `github.com/tidwall/rtree` removed from `go.mod`.
- Session coords: non-boxing float atomics + VisBox cache (`internal/session/session.go`).
- Real-world hub benches: `internal/postoffice/bench_realworld_test.go`.

Measured on Apple M4 Pro vs previous R-tree HEAD: UpdatePosition ~4–25× faster (0 alloc); large-N BroadcastRanged ~1.4× faster with far less memory. Unit + race tests green.

**Known leftover from that pass (cleanup if you touch nearby code):**

- Dead spatial-hash API in `internal/geo/geo.go` (`CellCover`, etc.) — unused; comments claim postoffice uses it.
- Stale comments in benches/tests mentioning “spatial hash” / “tree” / “cell cover”.
- Package doc for `All` says callbacks after collect; `All` streams during slab scan.
- Raw `VisRange.Store` desyncs VisBox — prefer `SetGeo` / `SetVisRange` only.

### Current concurrency model (baseline you will replace)

**Two goroutines per FSD connection after login**, not one:

1. Accept: `listenLoop` → `go s.handleConn(ctx, conn)` (`internal/server/server.go`).
2. Read/dispatch: same goroutine runs `eventLoop` (`internal/server/conn.go`).
3. Write: `go client.SenderWorker()` — only post-login path allowed to `Conn.Write`.
4. Shared: METAR worker pool, HTTP admin, optional sweatbox tick.

Outbound: `sendChan chan string` (cap 32). `Send` blocks; `SendPosition` is non-blocking latest-wins.

Hot path after postoffice work (hub density @ N≈10k ≈ 800–900 recipients):

```text
@ / ^ packet → parse → UpdatePosition (cheap) → Search → SendPosition × R
  → each peer SenderWorker → Conn.Write (one syscall per packet today)
```

Fan-out + write path dominate Search cost. That is what this handoff targets.

---

## Goals (in priority order)

### Goal A — Switch accept/read/write to **gnet** with a **static worker pool**

Product intent: **maximize efficiency / minimize context switching** vs `2N` goroutines.

Requirements:

1. Replace (or tightly wrap) the net.Listener + per-conn goroutine model for the **FSD TCP plane** with **[gnet](https://github.com/panjf2000/gnet)** (or the current maintained gnet/v2 API as appropriate for Go version in `go.mod`).
2. Use a **fixed number of event-loop / worker goroutines** (configurable; default sensible for GOMAXPROCS), **not** one reader+writer pair per connection.
3. Preserve **protocol and semantics**:
   - Login phase (server ident, client ident, add, auth, MOTD, register).
   - Packet framing (line-oriented FSD; today `bufio.Scanner` with 4KiB buffer).
   - Callsign registry via `postoffice` / `Registry` interface.
   - Post-login outbound still must not race concurrent writes on the same conn (single-writer per connection equivalent).
   - `Send` / `SendPosition` contracts (blocking vs latest-wins) from the point of view of handlers and fan-out.
   - Cancel / disconnect lifecycle: registry Release, disconnect broadcast, sweatbox synthetic sessions if affected.
4. Keep **HTTP admin / web** on standard `net/http` unless there is a hard reason not to (out of scope).
5. Keep **tests green**: unit, race where feasible, e2e under `internal/server`, stress tags if present. Update test harnesses that assume `net.Conn` + dual goroutines.
6. Document the new model (short package comment or design note in code): loops, who owns buffering, how outbound is scheduled.

Design constraints / guidance:

- Prefer **embedding gnet** behind a small internal interface so `Server` orchestration stays testable (fake transport in tests).
- Map gnet events → existing `handleConn` / `eventLoop` / handler dispatch with **minimal** handler rewrites; move I/O ownership, not protocol logic.
- Per-connection state must still hang off `*session.Session` (or a thin conn context pointing at it).
- Outbound: either async write queue per conn drained on the event loop, or a **fixed write-worker pool** — still **O(workers)** not O(N) dedicated writer goroutines. Do **not** reintroduce unbounded `go write(...)` per packet.
- Be careful with gnet’s buffer lifecycle: copy packet bytes before async work if the buffer is reused on return from the callback.
- Linux is primary production target; verify macOS still works for local dev/tests (gnet support matrix).

### Goal B — Position pipeline efficiency (still required)

Even with gnet, implement the **write + alloc** improvements on the position path:

1. **Coalesce outbound writes** for high-frequency traffic:
   - Buffer small FSD packets; flush on size (e.g. 4–16 KiB), short idle (e.g. 0.5–2 ms), or reliable/control packets (`#TM`, `$ER`, `$SF`, etc.).
   - Prefer writev / batched flush where it fits the gnet API cleanly.
2. **Near-zero-alloc hot path** for `@` / `^` / `#SL` / `#ST`:
   - Reduce `string([]byte)` thrash in `handlePilotPosition` / field parse (`internal/server/handler_position.go`, `util.go`).
   - Avoid scanner-buffer alias bugs in `eventLoop` (`append(packet, '\r','\n')` then fan-out).
   - One immutable payload for fan-out (string or refcounted bytes) shared across recipients is fine.
3. **Optional but valuable:** separate **position** queue (latest-wins, droppable) from **reliable** queue (text, errors, `$SF`) so storms do not drop control traffic and control does not block position.
4. Keep `SendPosition` non-blocking from the broadcaster’s perspective (no stall on slow peers).

### Goal C — Prove it

1. Extend or add benchmarks / stress:
   - Handler → enqueue → (simulated or loopback) write path at N=1k/10k hub geometry.
   - Compare goroutine count and CPU under load vs old dual-goroutine model (before/after notes in PR description).
2. Run `go test ./internal/postoffice/ ./internal/session/ ./internal/server/ ...` (and race on critical packages).
3. Do **not** claim victory on postoffice Search alone — show end-to-end position path improvement.

---

## Explicit non-goals

- Do **not** reintroduce R-tree / dual-threshold geo index as the default path.
- Do **not** rewrite the FSD wire protocol.
- Do **not** convert admin HTTP/UI into an SPA; web stays boring progressive enhancement if touched.
- Do **not** leave dead gnet experiments half-wired; either finish the FSD plane cutover or feature-flag cleanly with one default path.

---

## Key files to read first

| Path | Why |
|------|-----|
| `internal/server/conn.go` | accept → login → `SenderWorker` + `eventLoop` |
| `internal/server/server.go` | `listenLoop`, `Run`, deps |
| `internal/server/handler_position.go` | hot path parse + broadcast |
| `internal/server/util.go` | `broadcastRanged*`, field helpers |
| `internal/session/session.go` | `Send` / `SendPosition` / `SenderWorker` / geo atomics |
| `internal/postoffice/postoffice.go` | registry; already optimized — call, don’t regress |
| `internal/server/stress_test.go` / e2e | load and integration expectations |
| `internal/server/deps.go` | `Registry` and injection surface |

---

## Suggested implementation order

1. **Delete this file (`HANDOFF.md`)** once ingested.
2. Spike gnet listen + echo/line framing in a branch; confirm test strategy on macOS/Linux.
3. Port login + `eventLoop` dispatch onto gnet conn lifecycle; keep handlers.
4. Replace `SenderWorker` with event-loop / pooled outbound + write coalescing.
5. Alloc hygiene on position parse/broadcast.
6. Stress + race + e2e; fix fallout (sweatbox host, synthetic sessions, METAR `Sender`).
7. PR description: architecture diagram (workers, queues), goroutine model, bench numbers, risk notes.

---

## Success criteria

- [ ] FSD connections no longer use **2 dedicated goroutines per conn** as the steady-state model.
- [ ] Static/bounded worker (or gnet event-loop) count; configurable.
- [ ] Position fan-out does not block on slow peers; coalesced writes under load.
- [ ] Existing protocol e2e and registry semantics preserved.
- [ ] Tests pass; race-clean on session/postoffice/server critical paths where practical.
- [ ] Measurable improvement in CPU and/or max connections under hub-like position storms vs pre-change `dev` baseline.
- [ ] `HANDOFF.md` is gone from the tree.

---

## Owner intent (one line)

**Ship a gnet (or equivalent) fixed-worker FSD I/O plane plus a tighter position write/alloc path — postoffice is done; finish the rest of the server data path for scale.**
