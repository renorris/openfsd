# vPilot 3.12.1 — reverse-engineering research notes

**Date:** 2026-07-28  
**Installer:** `vPilot-Setup-3.12.1.exe` (NSIS)  
**Legal posture:** Research notes + hashes only. **Never commit or redistribute** vPilot binaries. Users install from [vpilot.rosscarlson.dev](https://vpilot.rosscarlson.dev/Download).

Tracked profile: `third_party/client-profiles/vpilot-3.12.1.yaml`

## Fingerprints

| Artifact | SHA-1 | SHA-256 |
|----------|-------|---------|
| Installer | `48820cb593c6cef8325a331c763316a8b33a501b` | `528a51bf0e71ada11103314d18a9fc0321afac94e2706f2fb13e0fbf5bb4beaa` |
| `vPilot.exe` | `7d95a7110392c15728143cc30e1f00899c686eb5` | `c743f204929309db6f49e8b543db2d661ad09c32a2a7bc71fe419c685d244bff` |

Default install directory: `%LOCALAPPDATA%\vPilot\` (per official docs — no admin required).

## Stack

- WinForms/.NET Framework 4.7.2, PE32 (x86) managed assembly
- Network/Common assemblies Dotfuscator-obfuscated (`RossCarlson.Vatsim.Network*`)
- AFV via **GeoVR.*** (GeoVR.Client ~15MB Costura-packed NAudio; GeoVR.Connection REST+UDP; GeoVR.Shared DTOs)
- Plugin API: `RossCarlson.Vatsim.Vpilot.Plugins` (events for aircraft/network; **not** a supported open server switch)
- SimConnect for MSFS/FSX/P3D

## Connection surfaces (what must be redirected for openfsd)

| Surface | Stock value (3.12.1) | openfsd target | Notes |
|---------|----------------------|----------------|-------|
| FSD JWT auth | `https://auth.vatsim.net/api/fsd-jwt` | `https://{host}/api/v1/fsd-jwt` | Hardcoded #US string; TLS required |
| Network status / server list | Config `NetworkStatusURL` → `http://status.vatsim.net/` | `https://{host}/api/v1/data/status.txt` | 3DES-obfuscated in `vPilotConfig.xml` |
| Cached servers | `AUTOMATIC\|fsd.connect.vatsim.net` | e.g. `OPENFSD\|fsd.example.com` | Same config crypto; `NAME\|host` |
| Automatic server HTTP | `http://fsd.vatsim.net/` | optional / unused if cached list set | "best server" HTTP endpoint (changelog 3.4.10+) |
| AFV REST base | `https://voice1.vatsim.net` | openfsd `AFV_API_PUBLIC_BASE_URL` | Hardcoded #US; prior patch utilities **disabled** AFV instead of retargeting |
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

## CLR #US / CIL ldstr map (3.12.1 `vPilot.exe`)

`#US` stream file offset: `0xA22E8` (size `0x15E04`).

| Logical string | #US heap offset (header) | Body file offset | ldstr CIL file offset(s) | Payload byte budget (UTF-16+term) |
|----------------|--------------------------|------------------|--------------------------|----------------------------------|
| `https://auth.vatsim.net/api/fsd-jwt` | `0x1569F` | `0xB7988` | `0x4BDB5` | 71 (odd ECMA-335) |
| `https://voice1.vatsim.net` | `0x6D8D` | `0xA9076` | `0x1F0A7` | 51 |
| `http://fsd.vatsim.net/` | `0x15751` | `0xB7A3A` | `0x4C0B2` | 45 |

Second copy of fsd-jwt UTF-16 body at `0xBA44A` — header walk failed (likely resource/non-#US); treat primary #US entry + ldstr `0x4BDB5` as authoritative until second site proven live.

**3.11.1 offsets are obsolete** (e.g. old fsd-jwt heap `0xD2A2` / ldstr `0x4B3C5` do not match 3.12.1). Profiles must be **version-pinned by binary hash**.

### Prior-art AFV strategy

`openfsd-client-patch-utility` example for vPilot 3.11.1 **disables AFV** (`ret` at voice connect handler) rather than rewriting voice URL. openfsd now has native `-afv`; design should prefer **retarget voice base URL** when space allows, with fallback disable.

fsd-jwt stock URL length limits padded #US overwrite; longer openfsd URLs need either:
1. shorter public base host, or
2. ldstr remap to a longer unused #US slot (3.11.1 technique: `0x72 XX XX XX 70`), or
3. runtime string/hook injection without shrinking constraint.

## Prior art (same author ecosystem)

| Repo | Role | Status |
|------|------|--------|
| [renorris/vpilot-patch-utility](https://github.com/renorris/vpilot-patch-utility) | On-disk PE #US + config obfuscation for 3.11.1 | Archived |
| [renorris/openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility) | Multi-client static patcher (vPilot/xPilot/Euroscope/vatSys) | Active reference |
| openfsd wiki `Client-Connection.md` | Points operators at those tools | Exists |

**Gap:** static on-disk patchers are CLI-centric, version-brittle, AFV-hostile (disable), and not a first-class openfsd module with a multi-client GUI. User request: **runtime injector** + **generic client picker GUI**.

## Injection strategy spectrum (for design)

1. **Config-only rewrite** — status URL + cached servers (3DES). Insufficient alone (JWT + AFV hardcoded).
2. **On-disk binary patch** — prior art; reversible backups; antivirus noise; requires quit vPilot.
3. **Launch-time ephemeral patch** — copy install tree or patch temp shadow; launch; optional restore. Borderline "runtime".
4. **True runtime** — process spawn under injector; rewrite PE before image map / or post-load memory #US / detour `HttpClient`/`Socket.connect` / hosts+local reverse proxy.
5. **Local control-plane proxy** — no PE touch: hosts or system proxy to fake `auth.vatsim.net`, `voice1.vatsim.net`, status; FSD via cached server IP. TLS MITM needs local CA (heavy UX).
6. **Plugin** — plugin surface is events, not network endpoint config; unlikely sufficient.

Recommended design direction (to be confirmed in design doc): **adapter interface per client**, default vPilot adapter combining (a) config rewrite, (b) versioned #US/CIL profile for JWT+AFV, (c) optional AFV disable fallback; GUI is **client-agnostic** (select client → locate install → bind openfsd endpoints → Apply/Revert/Launch).

## Legal / product constraints

- Do not redistribute vPilot or derivative redistributable binaries.
- Track **hashes + profiles + research** in git.
- Assume user-owned licensed install on disk.
- Do not facilitate connecting patched clients **to the public VATSIM network** in a deceptive way — product framing is **private openfsd networks**.
- Prefer reversible operations; never delete user model-matching data.

## Research method used

- Downloaded official NSIS installer; extracted with 7-Zip.
- PE/CLR metadata walk for `#US`; UTF-16 string inventory; ldstr token search.
- openssl 3DES-ECB decrypt of default config fields.
- Cross-check with archived Go patch utilities and openfsd AFV/FSD docs.
- Full ILSpy decompilation not run in this environment (no `dotnet`/ilspycmd); GeoVR method names recovered via managed metadata strings (`ApiServerConnection`, `AddCallsign`, `ConnectToVoiceServer`, etc.).

## Follow-ups for implementer

- [ ] Confirm second fsd-jwt site and automatic-server HTTP usage path on live connect.
- [ ] Measure max openfsd URL length vs #US budgets; document remap free-slot catalog per version.
- [ ] Locate AFV connect handler CIL for 3.12.1 disable-fallback (`ret` site was 3.11.1-specific).
- [ ] User-settings store path beyond install dir (`%LOCALAPPDATA%` may hold runtime config copy).
- [ ] Antivirus / code-signing interaction when rewriting signed `vPilot.exe`.
