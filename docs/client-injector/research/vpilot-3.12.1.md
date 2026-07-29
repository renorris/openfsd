# vPilot 3.12.1 — reverse-engineering research notes

**Date:** 2026-07-28  
**Installer:** `vPilot-Setup-3.12.1.exe` (NSIS)  
**Legal posture:** Research notes + hashes only. **Never commit or redistribute** vPilot binaries. Users install from [vpilot.rosscarlson.dev](https://vpilot.rosscarlson.dev/Download).

Tracked profile (embed canonical): `internal/clientinject/profiles/vpilot-3.12.1.yaml`  
Mirror metadata: `third_party/client-profiles/vpilot-3.12.1.yaml`  
**Gate status (R1–R5):** [`vpilot-3.12.1-gates.md`](./vpilot-3.12.1-gates.md)

## Fingerprints

| Artifact | SHA-1 | SHA-256 |
|----------|-------|---------|
| Installer | `48820cb593c6cef8325a331c763316a8b33a501b` | `528a51bf0e71ada11103314d18a9fc0321afac94e2706f2fb13e0fbf5bb4beaa` |
| `vPilot.exe` | `7d95a7110392c15728143cc30e1f00899c686eb5` | `c743f204929309db6f49e8b543db2d661ad09c32a2a7bc71fe419c685d244bff` |

Default install directory: `%LOCALAPPDATA%\vPilot\` (per official docs — no admin required). Size of primary PE: **1 236 416** bytes.

## Stack

- WinForms/.NET Framework 4.7.2, PE32 (x86) managed assembly
- Network/Common assemblies Dotfuscator-obfuscated (`RossCarlson.Vatsim.Network*`)
- AFV via **GeoVR.*** (GeoVR.Client ~15MB Costura-packed NAudio; GeoVR.Connection REST+UDP; GeoVR.Shared DTOs)
- Plugin API: `RossCarlson.Vatsim.Vpilot.Plugins` (events for aircraft/network; **not** a supported open server switch)
- SimConnect for MSFS/FSX/P3D

## Connection surfaces (what must be redirected for openfsd)

| Surface | Stock value (3.12.1) | openfsd target | Notes |
|---------|----------------------|----------------|-------|
| FSD JWT auth | `https://auth.vatsim.net/api/fsd-jwt` | `https://{host}/api/v1/fsd-jwt` (or short A8 path) | Hardcoded #US string; TLS required |
| Network status / server list | Config `NetworkStatusURL` → `http://status.vatsim.net/` | `https://{host}/api/v1/data/status.txt` | 3DES-obfuscated in `vPilotConfig.xml` |
| Cached servers | `AUTOMATIC\|fsd.connect.vatsim.net` | e.g. `OPENFSD\|fsd.example.com` | Same config crypto; `NAME\|host` |
| Automatic server HTTP | `http://fsd.vatsim.net/` | optional / unused if cached list set | "best server" HTTP endpoint (changelog 3.4.10+) |
| AFV REST base | `https://voice1.vatsim.net` | openfsd `AFV_API_PUBLIC_BASE_URL` | Hardcoded #US in **vPilot.exe only** (R5); prior patch utilities often **disabled** AFV instead |
| Flight plan browser | `https://my.vatsim.net/pilots/flightplan` | optional openfsd/web | Cosmetic / UX |
| Model matching CDN | `vpilot.rosscarlson.dev/ModelMatchingData/*` | leave alone | Not openfsd-related |
| Updates | `vpilot.rosscarlson.dev` VersionCheck | leave alone | |

### CLI (discovered)

- `-serveraddressoverride` — override FSD server address (positional override; still need JWT + status for full openfsd path)
- `-novoice` / `/novoice` — disable voice connection (standalone AFV client path)

### Config crypto (`vPilotConfig.xml`)

Fields `NetworkStatusURL`, `CachedServers` (and login fields) are Base64(3DES-ECB-PKCS7).

```
key = MD5("5575ac09-f2de-4a1e-808b-e3398e17f8bf")  // 16 bytes
3des_key = key || key[0:8]                       // 24 bytes
```

Source of truth for this algorithm: archived `renorris/vpilot-patch-utility` (`config/obfuscator.go`) and `openfsd-client-patch-utility` (`patch/vpilot_config.go`). Confirmed by decrypting shipping defaults.

`Public.key` in install tree is an EC P-256 SPKI (licensing/update verify — not the config field cipher).

### Config path resolution (R4 — closed)

Ordered candidates:

1. `{installRoot}/vPilotConfig.xml` (default install root = `%LOCALAPPDATA%\vPilot`)
2. `%LOCALAPPDATA%\vPilot\vPilotConfig.xml` when install root ≠ default
3. Previous inject manifest path
4. No separate Roaming path observed in PE strings

Rewrite **existing** files only; if none exist, require user to run vPilot once. See gate file for adapter rules.

## CLR #US / CIL ldstr map (3.12.1 `vPilot.exe`)

`#US` stream file offset: `0xA22E8` (size `0x15E04`). PE section: `.text`.

| Logical string | #US heap offset (header) | Body file offset | ldstr CIL file offset(s) | Payload byte budget (UTF-16+term) |
|----------------|--------------------------|------------------|--------------------------|----------------------------------|
| `https://auth.vatsim.net/api/fsd-jwt` | `0x1569F` | `0xB7988` | `0x4BDB5` | 71 (odd ECMA-335) |
| `https://voice1.vatsim.net` | `0x6D8D` | `0xA9076` | `0x1F0A7` | 51 |
| `http://fsd.vatsim.net/` | `0x15751` | `0xB7A3A` | `0x4C0B2` | 45 |

### Second UTF-16 fsd-jwt site (R3 — closed: not live)

Full-PE UTF-16 scan finds **exactly two** copies of stock `https://auth.vatsim.net/api/fsd-jwt`:

| File offset | In #US? | Terminal after UTF-16 | Verdict |
|-------------|---------|----------------------|---------|
| `0xB7988` | Yes | `0x01` (valid #US) | **Live** — only patch site |
| `0xBA44A` | No | `0x04` (invalid #US) | **Dead** embedded data; do **not** dual-write |

No LE references to `0xBA44A` as file offset/RVA/VA. Residual full-PE scanners will still hit the dead copy after a successful primary patch — treat as allowlisted dead hit (see gates doc).

**3.11.1 offsets are obsolete** (e.g. old fsd-jwt heap `0xD2A2` / ldstr `0x4B3C5` do not match 3.12.1). Profiles must be **version-pinned by binary hash**.

### Free #US slots / ldstr remap (R1 — open)

- ~2040 decode-OK `#US` entries; **no** long slot (budget ≥71) is unreferenced by CIL `ldstr`.
- Remap therefore means **sacrificing** a live cosmetic (or other) string, then rewriting JWT `ldstr` at `0x4BDB5` to the sacrifice heap token.
- Candidate catalog lives in [`vpilot-3.12.1-gates.md`](./vpilot-3.12.1-gates.md). Profile `us_free_slots` remains **empty** until a sacrifice is runtime-validated.
- Production-length JWT hostnames need **R1 and/or A8** (short JWT path on openfsd). Phase 0 short-host lab only otherwise (max host length **12** on default `/api/v1/fsd-jwt` with body budget 71).

### Prior-art AFV strategy (R2 — open)

`openfsd-client-patch-utility` example for vPilot **3.11.1** disables AFV (`ret` `0x2A` at file `0x4BA54`) rather than rewriting voice URL. On **3.12.1** that file offset is **not** a valid disable site.

openfsd prefers **retarget** voice base URL when space allows, with fallback **`-novoice`**. PE `ret` disable stays `file_offset: null` until R2 closes.

fsd-jwt stock URL length limits padded #US overwrite; longer openfsd URLs need either:

1. shorter public base host, or
2. server short-path alias (**A8**, e.g. fixed `POST /j`), or
3. ldstr remap to a longer sacrifice slot (R1), or
4. later-phase runtime/string hook strategies.

## GeoVR inventory (R5 — closed)

| Binary | Contains `voice1.vatsim.net`? |
|--------|-------------------------------|
| `vPilot.exe` | Yes (UTF-16) |
| `GeoVR.Client.dll` | No |
| `GeoVR.Connection.dll` | No |
| `GeoVR.Shared.dll` | No |
| Other extract DLLs | No |

AFV base retarget of `vPilot.exe` alone is sufficient for the stock voice URL. GeoVR still hosts `ConnectToVoiceServer` / `ApiServerConnection` machinery; URL is host-supplied.

## Prior art (same author ecosystem)

| Repo | Role | Status |
|------|------|--------|
| [renorris/vpilot-patch-utility](https://github.com/renorris/vpilot-patch-utility) | On-disk PE #US + config obfuscation for 3.11.1 | Archived |
| [renorris/openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility) | Multi-client static patcher (vPilot/xPilot/Euroscope/vatSys) | Active reference |
| openfsd wiki `Client-Connection.md` | Points operators at those tools | Exists |

**Gap:** static on-disk patchers are CLI-centric, version-brittle, AFV-hostile (disable), and not a first-class openfsd module with a multi-client GUI.

## Injection strategy spectrum (for design)

1. **Config-only rewrite** — status URL + cached servers (3DES). Insufficient alone (JWT + AFV hardcoded).
2. **On-disk binary patch** — prior art; reversible backups; antivirus noise; requires quit vPilot.
3. **Launch-time ephemeral patch** — copy install tree or patch temp shadow; launch; optional restore. Borderline "runtime".
4. **True runtime** — process spawn under injector; rewrite PE before image map / or post-load memory #US / detour `HttpClient`/`Socket.connect` / hosts+local reverse proxy.
5. **Local control-plane proxy** — no PE touch: hosts or system proxy to fake `auth.vatsim.net`, `voice1.vatsim.net`, status; FSD via cached server IP. TLS MITM needs local CA (heavy UX).
6. **Plugin** — plugin surface is events, not network endpoint config; unlikely sufficient.

Recommended design direction: **adapter interface per client**, default vPilot adapter combining (a) config rewrite, (b) versioned #US/CIL profile for JWT+AFV, (c) optional AFV disable fallback; GUI is **client-agnostic**.

## Legal / product constraints

- Do not redistribute vPilot or derivative redistributable binaries.
- Track **hashes + profiles + research** in git.
- Assume user-owned licensed install on disk.
- Do not facilitate connecting patched clients **to the public VATSIM network** in a deceptive way — product framing is **private openfsd networks**.
- Prefer reversible operations; never delete user model-matching data.

## Research method used

- Downloaded official NSIS installer; extracted with 7-Zip into gitignored `.research/vpilot/`.
- PE/CLR metadata walk for `#US`; UTF-16 string inventory; ldstr token search (`72` + `70` table).
- openssl / Go 3DES-ECB decrypt of default config fields.
- Cross-check with archived Go patch utilities and openfsd AFV/FSD docs.
- GeoVR DLLs scanned for voice host (ASCII + UTF-16).
- Full ILSpy decompilation not run in this environment (no `dotnet`/ilspycmd); method names recovered via managed metadata strings where present.
- Optional CI-local revalidation: `go test -tags=research ./internal/clientinject/...` when extract PE is present (see `research_pe_test.go`).

## Follow-ups

- [x] R3: classify second fsd-jwt site (`0xBA44A`) — **not live**; primary only.
- [x] R4: config path candidates — install root + LOCALAPPDATA fallback.
- [x] R5: GeoVR re-hardcode check — **none**.
- [ ] R1: runtime-validate a sacrifice free slot; populate `us_free_slots`.
- [ ] R2: locate AFV connect handler CIL for 3.12.1 `ret` disable.
- [ ] Residual HealthCheck allowlist for dead `0xBA44A` when implementing adapter-complete policy.
- [ ] Antivirus / code-signing interaction when rewriting signed `vPilot.exe`.
- [ ] R6 (separate): CachedServers `host:port` acceptance on 3.12.1.
