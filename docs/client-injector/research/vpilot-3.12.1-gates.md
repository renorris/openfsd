# vPilot 3.12.1 — research gate status (R1–R5)

**Date:** 2026-07-28  
**PE:** `vPilot.exe` SHA-1 `7d95a7110392c15728143cc30e1f00899c686eb5` (size 1 236 416)  
**Method:** local extract under gitignored `.research/vpilot/extracted/` (never committed); #US heap walk; UTF-16 residual scan; GeoVR DLL inventory; prior-art cross-check (`vpilot-patch-utility` 3.11.1, `openfsd-client-patch-utility`).  
**Full notes:** [`vpilot-3.12.1.md`](./vpilot-3.12.1.md)

| ID | Gate | Status | Profile impact |
|----|------|--------|----------------|
| **R1** | Free `#US` slot catalog + ldstr remap | **CLOSED** (cosmetic sacrifice slots; no zero-ldstr long entries) | `us_free_slots` populated (config_updated_msg 1053, sim_not_found_cmd 421) |
| **R2** | AFV connect `ret` CIL offset | **OPEN** | `afv_disable_pe.file_offset: null` |
| **R3** | Second fsd-jwt body `0xBA44A` liveness | **CLOSED — not live** | Keep primary only: `body_file_offsets: [0xB7988]` |
| **R4** | Config path resolution | **CLOSED** (candidates documented) | `config_files` still install-relative; multi-candidate is adapter discovery |
| **R5** | GeoVR voice base inventory | **CLOSED — no re-hardcode** | No extra DLL mutations required for AFV base URL |

## Phase 0 readiness (honest)

| Claim | OK? |
|-------|-----|
| Short-host lab JWT (in-place #US budget ≤71 body bytes; host ≤12 on default `/api/v1/fsd-jwt`) | **Yes** (profile offsets confirmed) |
| Production-length JWT hostnames without A8 | **Yes** — free-slot remap into large cosmetic #US (budget 1053 / 421) |
| AFV base retarget when URL fits #US budget 51 | **Yes** if host short enough; URL only in `vPilot.exe` (R5) |
| AFV PE `ret` disable fallback | **No** until R2 |
| Residual HealthCheck post-R3 | **Yes** when allowlisting dead `0xBA44A` and failing only on **unexpected** hits (design rev 3.3 / KD-20). Whole-PE residual-zero is **not** required or achievable without dual-write |
| Config rewrite path | **Yes** for default install (`%LOCALAPPDATA%\vPilot\`); multi-candidate ordered list in R4 |

**R1 free-slot remap is profile-enabled** for production-length hosts (sacrifices cosmetic UI strings). A8 short JWT paths remain useful to keep JWT in-place when preferred.

---

## R1 — Free `#US` slot catalog (**CLOSED**)

### Findings

- `#US` stream: file `0xA22E8`, size `0x15E04`; ~2040 decode-OK entries.
- **Zero** entries with body budget ≥71 have **ldstr reference count 0**. Every long slot is referenced at least once in CIL (`ldstr` token `0x72` + little-endian `(0x70<<24)|heap_offset`).
- 3.11.1 prior art remapped JWT `ldstr` to heap `0xD2A2` (sacrificing whatever string lived there). On **3.12.1**, `0xD2A2` is **mid-body** of another entry (`heap=0xD254`, message about remote events) — **not** a valid free entry start. **3.11.1 free-slot offsets are obsolete.**
- Largest single-ldstr non-critical string: **METAR regex** at heap `0x13C8C` (budget 1097) — **do not sacrifice** (breaks METAR parse).

### Decision (profile-enabled)

Remap overwrites a **live cosmetic** string and points JWT/AFV `ldstr` at that heap offset. Accepted UX: rare dialog text may show a URL string if that code path is hit.

| id | heap | body file | budget | ldstr | Stock (preview) |
|----|------|-----------|--------|-------|-----------------|
| `config_updated_msg` | `0x12EEE` | `0xB51D8` | **1053** | `0x3763C` | Config file updated to latest version… |
| `sim_not_found_cmd` | `0x0DE5A` | `0xB0144` | **421** | `0x32F94` | Command failed. The active simulator was not found… |

**JWT ldstr to rewrite when remapping:** file `0x4BDB5` (`72 9F 56 01 70` → stock heap `0x1569F`).  
**AFV ldstr:** file `0x1F0A7` → heap `0x6D8D`.  
Remap bytes: `72 <heap_le24> 70` with new heap offset. Adapter writes free-slot `#US` entry (variable length-prefix width OK) then overwrites ldstr token(s).

### Close criteria

1. ~~Pick ≥1 sacrifice slot; document accepted UX breakage.~~  
2. Runtime smoke: connect openfsd with long JWT URL via remap (operator / lab).  
3. ~~Populate `us_free_slots` in embed profile + plan/apply tests.~~

---

## R2 — AFV connect `ret` CIL offset (**OPEN**)

### Findings

| Item | 3.11.1 prior art | 3.12.1 |
|------|------------------|--------|
| AFV disable | `ret` (`0x2A`) at file `0x4BA54` | **Obsolete** — bytes at `0x4BA54` are unrelated (`DD …` leave / other CIL) |
| Voice base `ldstr` | (different heap) | **Confirmed** file `0x1F0A7` → heap `0x6D8D` (`https://voice1.vatsim.net`) |
| CLI | — | `-novoice` / `/novoice` present as #US CLI help strings |

CIL around `0x1F0A7` loads the voice base URL into connection setup (`ldstr` + `ldftn`/`newobj` pattern) — **not** identified as a simple event-handler entry suitable for a one-byte `ret` patch without method-boundary confirmation.

No ILSpy/ilspycmd run in this environment; **ConnectToVoiceServer** symbols live in **GeoVR.Connection.dll**, not as a clear single `ret` site in `vPilot.exe` matching 3.11.1.

### Phase 0 fallback

- Prefer AFV #US retarget when budget allows.  
- Else launch flags `-novoice` / `/novoice`.  
- **Do not** set `mutations.afv_disable_pe.file_offset` until a confirmed 3.12.1 handler start is known.

### Close criteria

Decompile or symbol-map the AFV connect path on 3.12.1; set `file_offset` + fixture test; only then enable PE disable mutation.

---

## R3 — Second fsd-jwt body `0xBA44A` (**CLOSED — not live**)

### Findings

| Check | Primary `0xB7988` | Second `0xBA44A` |
|-------|-------------------|------------------|
| Inside `#US` heap (`0xA22E8`–`0xB80EC`) | **Yes** | **No** |
| Valid #US body terminal after UTF-16 | `0x01` (matches ECMA-335 for `fsd-jwt`) | **`0x04`** (not a #US terminal) |
| Compressed length prefix at body−1/2/4 | Yes (prefix at heap entry) | No consistent live #US header |
| PE section | `.text` | `.text` (not `.rsrc`) |
| LE refs to file offset / RVA / VA | none (expected; CLR uses tokens) | **none** |
| UTF-16 residual scan hits | 1 of 2 | 1 of 2 |

Pre-context at `0xBA44A` is binary framing (`… 04 88 13 00 00 04 98 3A … 08 2D 43 1C EB E2 36 0A 3F 46` then UTF-16 URL) — consistent with **embedded managed resource / serialized constant data**, not the live `#US` entry consumed by `ldstr 0x4BDB5`.

### Profile decision

- **Do not** add `0xBA44A` to `strings.fsd_jwt.body_file_offsets`.  
- Patching only the live `#US` body (+ length prefix) is correct for runtime JWT.  
- Full-PE residual scan will still report `0xBA44A` after a successful primary patch → HealthCheck **allowlists** that offset as a documented dead hit (design KD-20). This is **not** a reason to dual-write.

### Residual-scan test policy

Synthetic fixtures use **two UTF-16 copies (live-style terminal `0x01` + dead residual framing terminal `0x04`)** to validate the scanner and lock the R3 live/dead distinction. Real PE research test (optional `research` tag) asserts exactly two hits at `0xB7988` and `0xBA44A` on stock 3.12.1.

**HealthCheck residual rule (aligned with design rev 3.3 / KD-20):** after patching profile-listed live bodies, **allowlist** documented dead residuals (`0xBA44A`); **fail only on unexpected** residual stock JWT hits. Do **not** require whole-PE residual-zero or dual-write of dead sites.

---

## R4 — Config path resolution (**CLOSED**)

### Candidates (Windows, ordered)

1. `{installRoot}/vPilotConfig.xml` — **default**: install root is `%LOCALAPPDATA%\vPilot\` (official non-admin layout); shipping extract places config beside `vPilot.exe`.  
2. `%LOCALAPPDATA%\vPilot\vPilotConfig.xml` — same as (1) when install is default; still probe when user selected a **custom** install root.  
3. Path recorded in a previous `.openfsd-client` / inject manifest (adapter/engine).  
4. **Not observed** as a separate roaming `AppData\Roaming\vPilot` path in PE string inventory; PE #US includes `vPilotConfig.xml` and “configuration file was not found” messaging only (no alternate absolute path string).

### Stock defaults (installer extract)

Decrypt (3DES profile crypto) of extract `vPilotConfig.xml`:

- `NetworkStatusURL` → `http://status.vatsim.net/`  
- `CachedServers` → `AUTOMATIC|fsd.connect.vatsim.net`

### Adapter behavior (normative from design)

- Discover all existing candidates; rewrite existing only.  
- If **zero** config files exist: blocker — run vPilot once to create config (do not invent a create path without further evidence).  
- Profile `config_files[0].relative_path` remains `vPilotConfig.xml` (relative to install root). Multi-candidate logic is discovery code, not additional YAML rows, until a second distinct relative layout is proven.

---

## R5 — GeoVR string inventory (**CLOSED**)

Scanned install-tree managed binaries for `voice1.vatsim.net` / `https://voice1.vatsim.net` (ASCII + UTF-16):

| Binary | `voice1.vatsim.net` |
|--------|---------------------|
| `vPilot.exe` | **Yes** (UTF-16; live #US + residual forms as above) |
| `GeoVR.Client.dll` | **No** |
| `GeoVR.Connection.dll` | **No** |
| `GeoVR.Shared.dll` | **No** |
| Other `*.dll` in extract | **No** |

GeoVR.Connection exposes `ApiServerConnection`, `ConnectToVoiceServer`, etc. — base URL is supplied by the host (`vPilot.exe`), not re-hardcoded in GeoVR packages for 3.12.1 extract.

**Conclusion:** AFV base retarget of the `vPilot.exe` #US slot is **sufficient** for stock voice URL redirection. No GeoVR DLL mutations required for R5. (Does not close R2 PE-disable.)

---

## Fingerprint re-check (this gate pass)

| Artifact | SHA-1 |
|----------|-------|
| `vPilot.exe` | `7d95a7110392c15728143cc30e1f00899c686eb5` |
| Installer (recorded) | `48820cb593c6cef8325a331c763316a8b33a501b` |

Confirmed live #US map unchanged from research seed:

| String | heap | body | ldstr | budget |
|--------|------|------|-------|--------|
| fsd-jwt | `0x1569F` | `0xB7988` | `0x4BDB5` | 71 |
| AFV base | `0x6D8D` | `0xA9076` | `0x1F0A7` | 51 |
| fsd auto HTTP | `0x15751` | `0xB7A3A` | `0x4C0B2` | 45 |

---

## What was intentionally not committed

- Any `vPilot.exe` / GeoVR / installer PE or DLL  
- Local analysis scripts under `/tmp` or `.research/`  
- AFV `ret` offset without confirmation (R2 still open)
