# openfsd Client Setup — operator guide

**Product name:** openfsd Client Setup  
**Binary:** `openfsd-client`  
**Audience:** operators of a private openfsd network who need third-party pilot/ATC clients to connect to that network.

This guide is the operator-facing summary. Design detail lives in
[docs/design/client-runtime-injector.md](../design/client-runtime-injector.md).
Version-pinned reverse-engineering notes live under
[docs/client-injector/research/](research/).

---

## What it is (and is not)

openfsd Client Setup configures a **user-installed Windows vPilot** install so it
talks to **your** openfsd deployment instead of the public VATSIM endpoints it
ships with. Other pilot/ATC clients are out of scope for this tool for now.

| Phase 0 (shipped intent) | Not Phase 0 |
|--------------------------|-------------|
| On-disk PE string / config **apply** with reversible backups | In-memory process inject |
| Config rewrite (status URL, cached servers) | Shadow / ephemeral PE launch (**PR-9**) |
| Reversible **Revert** + **Health** | PE AFV `ret` disable (research gate **R2**) |
| Headless `launch` **prints** planned client args (`-novoice`, server override) — does not spawn the PE | Real process spawn / Fyne GUI (later PRs) |
| — | Redistributing client installers (forbidden; see Legal) |

**Honest naming:** Phase 0 is an **on-disk apply + config** tool. Do not market it as
a “runtime-only injector.” Runtime strategies are later roadmap phases only.

---

## Legal

- **Never redistribute** vPilot or any other proprietary client binary,
  installer, or patched derivative.
- Users must install the client **themselves** from the vendor (e.g.
  [vpilot.rosscarlson.dev](https://vpilot.rosscarlson.dev/Download)).
- openfsd tracks **hashes, YAML profiles, research notes, and our code** —
  never third-party binaries (local extracts go under gitignored `.research/`).
- Product framing: **private openfsd networks you are authorized to operate**.
  Do not use Client Setup to deceptively connect patched clients to the public
  VATSIM network.

---

## Prerequisites

1. A running openfsd deployment with web (JWT + status feed) and FSD TCP.
2. Optional: openfsd started with **`-afv`** if you want voice retarget (see AFV below).
3. A **user-owned** install of a supported client version (today: **vPilot 3.12.1**,
   hash-pinned in the profile).
4. **Quit the client completely** before **Apply** or **Revert**. The tool refuses
   when the primary PE is locked / the process is running.

### Build (when the binary is in-tree)

```bash
go build -o openfsd-client ./cmd/openfsd-client
```

`openfsd-client` is an **auxiliary** cmd (like `openfsd-migrate-*`). It is **not**
a flag on the main `openfsd` server binary.

### Installers (Windows / macOS / Linux)

CI builds traditional OS installers that install the **plain binary** (not
Fyne-specific packages). See [packaging/openfsd-client/README.md](../../packaging/openfsd-client/README.md).

| Platform | Installer | Binary location | Settings |
|----------|-----------|-----------------|----------|
| Windows | Inno Setup `.exe` | `%ProgramFiles%\openfsd-client\` | `%AppData%\openfsd-client\` |
| macOS | `.pkg` / `.dmg` | `/Applications/openfsd Client Setup.app` | `~/Library/Application Support/openfsd-client/` |
| Linux | `.deb` | `/usr/bin/openfsd-client` | `~/.config/openfsd-client/` |

Local: `./packaging/openfsd-client/build.sh` (on the target OS).

---

## Phase 0 limits (read this before production hostnames)

vPilot 3.12.1 embeds stock JWT and AFV base URLs in the managed PE (`#US` heap).
In-place overwrite has a **fixed UTF-16 payload budget**. Longer URLs use
**free-slot remap (R1)**: write into a large cosmetic `#US` entry and rewrite the
CIL `ldstr` token. Server short JWT paths (**A8**) remain optional to keep
JWT in-place when preferred.

### JWT host length

| Strategy | Max **host** (approx.) | Notes |
|----------|------------------------:|-------|
| In-place default `…/api/v1/fsd-jwt` | **12** chars | Stock `#US` budget 71 body bytes |
| In-place + `--prefer-short-jwt` → `POST /j` | **25** chars | Needs server A8: `/j` = **getFsdJwt**, not a bare 302 |
| **Free-slot remap (R1)** | **~500+** chars | Cosmetic UI string sacrificed; profile `us_free_slots` |

**Default without `--prefer-short-jwt`:** planner uses `/api/v1/fsd-jwt`; if the
full URL exceeds the stock slot, it remaps into a free slot (no plan blocker).

**With `--prefer-short-jwt`:** planner prefers short paths first (order typically
`/j`, then other A8 aliases) so JWT can stay in-place when the server’s
`POST /j` invokes the JWT handler **directly**.

| Research / server work | Status (honest) |
|------------------------|-----------------|
| **R1** free-slot `#US` + ldstr remap | **CLOSED** — long hosts remap into large cosmetic slots (budget 1053 / 421) |
| **A8** short JWT paths (fix `/j` + aliases) | Optional; keeps JWT in-place. **Stock openfsd may still 302 `POST /j` until A8/PR-5** |
| **R2** PE AFV `ret` disable site | **OPEN** — not used in Phase 0 |

Example hosts that fit default **in-place** path (≤12): `fsd.ex.co` (9).  
Typical FQDNs like `openfsd.example.com` (19) use **free-slot remap** automatically
(or `--prefer-short-jwt` + server `/j` to stay in-place).

### AFV (voice)

| Situation | Phase 0 behavior |
|-----------|------------------|
| AFV base URL length **≤ 25** chars total | Prefer **retarget** `#US` to your `AFV_API_PUBLIC_BASE_URL` |
| AFV base URL **> 25** chars | Free-slot remap when a slot remains; else **`-novoice`** |
| Operator chooses no voice | `--force-disable-afv` → launch with **`-novoice`** |
| PE connect-handler `ret` disable | **Not until R2** — do not claim Phase 0 does this |

Examples: `https://v.ex.co` (15) fits in-place; `https://voice.example.com` (25) is at
in-place budget; longer bases remap when free slots are available.

openfsd must be running with **`-afv`** (and correct advertise/public base URL)
for retargeted voice to work. Without AFV on the server, use Discord/etc. and
launch with voice disabled.

### status.txt (VATSIM-shaped subset)

Client Setup points the client’s network status URL at openfsd:

```text
GET {web-base}/api/v1/data/status.txt
```

That feed is a **VATSIM-shaped subset**, not a byte-identical VATSIM status file.
Operators should know it advertises (among other keys):

| Key | Role |
|-----|------|
| `json3` | JSON data feed URL |
| `url1` | Servers list URL |
| `servers.live` | Live servers list URL |

Exact template: `internal/web/data_templates/status.txt`. Cached server list is
usually also written directly into the client config so auto-download is less
critical after Apply.

---

## Safety: quit before Apply

1. Fully **quit** the client (not minimize).
2. Confirm Client Setup / CLI preflight is green (process not running, PE not locked).
3. Run **plan** (dry) and read blockers / constraints.
4. **apply** only when plan has no blockers you have not accepted.
5. **health** after apply; **revert** if something is wrong.

Half-applied installs should not happen if the engine auto-reverts on health
failure; keep the `.openfsd-bak` / manifest tree intact until you are sure.

---

## CLI examples

The headless binary is **CLI-only today**. No subcommand prints help (Fyne GUI is
a later PR — design PR-7):

```bash
openfsd-client
# same as: openfsd-client help
```

List embedded client profiles:

```bash
openfsd-client list-profiles
```

Detect install candidates (Discover only — **no** `--install` flag):

```bash
openfsd-client detect --client vpilot
# Prints root=… pe=… for each candidate, or "no vpilot installs found (pass --install DIR)"
# Use --install on plan|apply|health|launch|revert when auto-detect is wrong or empty.
```

Dry-run plan (always do this first on a new hostname):

```bash
openfsd-client plan \
  --client vpilot \
  --install "%LOCALAPPDATA%\vPilot" \
  --web-base "https://fsd.ex.co" \
  --fsd-host "fsd.ex.co" \
  --fsd-port 6809 \
  --afv-base "https://v.ex.co"
```

Production-style host with short JWT path preference (server must support A8 `/j`
with a **direct** JWT handler — not a bare 302):

```bash
openfsd-client plan \
  --client vpilot \
  --install "%LOCALAPPDATA%\vPilot" \
  --web-base "https://openfsd.example.com" \
  --fsd-host "openfsd.example.com" \
  --prefer-short-jwt \
  --afv-base "https://voice.example.com"
```

Apply / health / revert (quit client first). **Health must use the same endpoint
flags as apply** — empty `--web-base` / `--afv-base` skips or weakens JWT/AFV
assertions and can print `health ok` without verifying patches:

```bash
openfsd-client apply  --client vpilot --install "%LOCALAPPDATA%\vPilot" \
  --web-base "https://fsd.ex.co" --fsd-host "fsd.ex.co" \
  --afv-base "https://v.ex.co"

openfsd-client health --client vpilot --install "%LOCALAPPDATA%\vPilot" \
  --web-base "https://fsd.ex.co" --fsd-host "fsd.ex.co" \
  --afv-base "https://v.ex.co"

openfsd-client revert --client vpilot --install "%LOCALAPPDATA%\vPilot"
```

### Launch (dry-print only)

Headless `launch` **does not start** the client. It recomputes planned args from
the same endpoint flags and prints an `exec: …` line for you to run from the
install directory (or your own launcher). Real process spawn / ephemeral shadow
PE is **PR-9+**; GUI spawn is **PR-7+**.

Pass the **same** `--web-base` / `--fsd-host` / `--afv-base` / `--force-disable-afv`
(and friends) used at apply so printed flags match the plan (`-serveraddressoverride`,
`-novoice` when over-budget or forced):

```bash
openfsd-client launch --client vpilot --install "%LOCALAPPDATA%\vPilot" \
  --web-base "https://fsd.ex.co" --fsd-host "fsd.ex.co" \
  --afv-base "https://v.ex.co"
# stdout example:
#   exec: C:\…\vPilot.exe -serveraddressoverride fsd.ex.co
#   (launch is dry-print only in headless CLI; start the client with these args from the install directory)

# Force no voice in the printed args (and plan AFV skip on apply):
openfsd-client apply  ... --force-disable-afv
openfsd-client launch ... --force-disable-afv
# printed args include -novoice
```

Common flags (`plan` | `apply` | `health` | `launch` share the endpoint set;
`revert` needs only `--install`; `detect` only `--client`):

| Flag | Meaning |
|------|---------|
| `--client` | Adapter id (`vpilot`, …); `detect` default `vpilot` |
| `--install` | Client install directory (**required** for plan/apply/health/launch/revert) |
| `--web-base` | openfsd web base (`https://host`) |
| `--fsd-host` / `--fsd-port` | FSD TCP (default port **6809**) |
| `--afv-base` | AFV REST public base (must be ≤25 chars for retarget) |
| `--prefer-short-jwt` | Prefer server short JWT paths (`/j` → max host **25** if A8 fixed) |
| `--force-disable-afv` | Skip AFV retarget; include **`-novoice`** in launch args |

Optional (advanced): `--fsd-server-name` (CachedServers label, default `OPENFSD`),
`--profiles-dir` (override embedded YAML).

Exit codes (`cmd/openfsd-client`):

| Code | Meaning |
|-----:|---------|
| 0 | ok |
| 1 | usage |
| 2 | hash mismatch (unknown / wrong PE) |
| 3 | apply failed (auto-reverted when possible); also used when health fails |
| 4 | revert failed |
| 5 | client running / file locked |
| 6 | plan blockers |

---

## Operator checklist

- [ ] Client installed legally; version hash matches a known profile.
- [ ] Client **fully quit** before Apply / Revert.
- [ ] JWT host fits budget: ≤**12** default, or ≤**25** with `--prefer-short-jwt` + fixed server `/j`.
- [ ] Long hosts: free-slot remap is automatic; optional A8 short paths keep JWT in-place.
- [ ] AFV URL ≤**25** chars for retarget; else accept **`-novoice`**.
- [ ] Do not expect PE AFV disable until **R2**.
- [ ] Never ship or mirror vPilot binaries in git, Docker images, or operator dropboxes.
- [ ] After Apply: `health` with the **same** endpoint flags as apply, then one real connect smoke test (start client yourself with printed `launch` args or manually).

---

## Related documents

| Document | Role |
|----------|------|
| [docs/design/client-runtime-injector.md](../design/client-runtime-injector.md) | Full design (phases, budgets, A8, research gates) |
| [docs/client-injector/research/vpilot-3.12.1.md](research/vpilot-3.12.1.md) | vPilot 3.12.1 RE notes / fingerprints |
| [wiki/Client-Connection.md](../../wiki/Client-Connection.md) | Operator wiki: connect paths per client |
| [docs/authentication-token.md](../authentication-token.md) | FSD JWT request/response shape |
| [docs/design/afv-server.md](../design/afv-server.md) | openfsd AFV (`-afv`) |

External historical tools (superseded for openfsd docs by `openfsd-client`):

- [renorris/vpilot-patch-utility](https://github.com/renorris/vpilot-patch-utility) (archived)
- [renorris/openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility)
