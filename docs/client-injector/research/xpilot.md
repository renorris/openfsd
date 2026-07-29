# xPilot 3.0.1 — reverse-engineering research notes

**Date:** 2026-07-28  
**Client:** xPilot 3.0.1 (`xPilot.exe`)  
**Legal posture:** Research notes + hashes only. **Never commit or redistribute** xPilot binaries. Users install from the official xPilot distribution.

Tracked profile: `third_party/client-profiles/xpilot-3.0.1.yaml`  
Embed plan profile: `internal/clientinject/profiles/xpilot-3.0.1.yaml`

## Fingerprints

| Artifact | SHA-1 |
|----------|-------|
| `xPilot.exe` (3.0.1) | `1ae61e1d4a624751124a49cd992c90f948c31d37` |

Default install path (prior art): `C:\Program Files\xPilot\xPilot.exe`.

**SHA-256 / installer size:** not recorded in prior art; leave blank until a maintainer re-hashes a user-owned install.

## Stack

- Native Windows PE64 (not CLR / not #US). Prior-art patches target `.text` / `.idata` / `.data` section VAs.
- Built-in AFV voice path (separate from FSD). **Prior-art 3.0.1 patchfile does not retarget AFV** — openfsd AFV requires a future research pass or an external AFV client (TrackAudio, etc.).
- Network surfaces of interest: status JSON URL, fsd-jwt URL, residual `fsd.vatsim.net` string.

## Connection surfaces (what the 3.0.1 prior-art path redirects)

| Surface | Stock role | openfsd target | Patch family |
|---------|------------|----------------|--------------|
| Network status | VATSIM status JSON | `https://{host}/api/v1/data/status.json` | padded_string (UTF-8) + LEA RIP + length imm |
| FSD JWT auth | VATSIM fsd-jwt | `https://{host}/api/v1/fsd-jwt` | padded_string (UTF-16LE) + LEA RIP×2 + length imm×2 |
| FSD auto host | `fsd.vatsim.net` (or related) | break / neutralise | raw_overwrite 3 bytes (`foo`) in `.idata` |
| AFV REST | (not in prior art) | — | **out of scope** for 3.0.1 adapter |

Unlike vPilot, there is **no obfuscated config XML** in the 3.0.1 path: server list comes from the retargeted status JSON feed.

## Section map (3.0.1 prior art)

| Section | Raw file offset | Virtual start |
|---------|-----------------|---------------|
| `.text` | `0x400` | `0x140001000` |
| `.idata` | `0x01A5A200` | `0x141A5B000` |
| `.data` | `0x027F9000` | `0x1427FA000` |

File offset conversion (same as openfsd-client-patch-utility):

```text
file_offset = section.raw_offset + (section_address_va - section.virtual_start)
```

### Computed file offsets (ported)

| Logical site | Section | VA | File offset |
|--------------|---------|----|-------------|
| status.json LEA RIP | `.text` | `0x140028D0E` | `0x2810E` |
| status.json length imm | `.text` | `0x140028D07` | `0x28107` |
| fsd-jwt LEA RIP (site 1) | `.text` | `0x140035BFE` | `0x34FFE` |
| fsd-jwt length (site 1) | `.text` | `0x140035C0A` | `0x3500A` |
| fsd-jwt LEA RIP (site 2) | `.text` | `0x14006BE08` | `0x6B208` |
| fsd-jwt length (site 2) | `.text` | `0x14006BE14` | `0x6B214` |
| break `fsd.vatsim.net` | `.idata` | `0x141CBF240` | `0x1CBE440` |
| status.json string slot | `.idata` | `0x141AAF5AE` | `0x1AAE7AE` |
| fsd-jwt string slot | `.idata` | `0x141A9662C` | `0x1A9582C` |

### Slot budgets

| Slot | Encoding | `available_bytes` | Notes |
|------|----------|-------------------|-------|
| status.json URL | UTF-8 (+ NUL pad) | `0x3B5` (949) | Length imm is **single byte** (max URL length 255) |
| fsd-jwt URL | UTF-16LE (+ U+0000 pad) | `0x3B4` (948) | Length imm is **character count**, single byte |

LEA RIP displacements in prior art point at the **relocated** `.idata` slots (not the stock string sites). They are **content-independent** for a fixed slot address. Length immediates **must** match the new URL length at Apply time.

Example prior-art URLs (length reference only):

- `https://yourfsdserver.com/api/v1/data/status.json` → length `49`
- `https://yourfsdserver.com/api/v1/fsd-jwt` → length `40`

## Prior art source

| Repo / path | Role |
|-------------|------|
| `renorris/openfsd-client-patch-utility` `example_patchfiles/xpilot/xpilot-3.0.1.yaml` | Section overwrite + padded string patchfile for sum `1ae61e1d…` |
| Same repo `patch/section_*.go` | VA→raw conversion + padded write semantics |
| openfsd wiki `Client-Connection.md` | Points operators at that utility for xPilot |

**This PR ports those offsets into schema v2 + `internal/clientinject` adapter.** Offsets were **derived** from the published prior-art YAML (VA + section map → file offset). They were **not re-verified against a live `xPilot.exe` binary in this environment** (binaries are never committed). The adapter **refuses** any PE whose SHA-1 ≠ profile stock.

## Adapter strategy (openfsd Client Setup)

1. **Discover** — `Program Files\xPilot`, `Program Files (x86)\xPilot`, Wine equivalents.
2. **Verify** — primary PE SHA-1 must match profile (or stock bak after re-apply).
3. **Plan / Apply** — `padded_string` for status + JWT; `raw_overwrite` for LEA RIP fixups, dynamic length bytes, and break-fsd; transactional `.openfsd-bak` via engine.
4. **HealthCheck** — decode padded slots; assert URLs match planned endpoints; assert break site is not stock prefix when patched.
5. **LaunchArgs** — empty (no documented xPilot CLI server override in prior art).
6. **AFV** — warn only; not patched. Operators may set AFV base in an external client or await a future profile.

## Honesty / known gaps

- [ ] Re-hash a maintainer-owned 3.0.1 install; add SHA-256 + `size_bytes` when confirmed.
- [ ] Confirm live connect path after status.json + fsd-jwt retarget (server list fields xPilot expects).
- [ ] Inventory AFV / voice base URL sites in the same PE (and companion DLLs if any).
- [ ] Confirm single-byte length immediates for non-ASCII hosts (openfsd URLs are ASCII).
- [ ] Antivirus / code-signing interaction when rewriting signed `xPilot.exe`.
- [ ] Newer xPilot versions need **new** version-pinned profiles — do not reuse 3.0.1 offsets.

## Legal / product constraints

- Do not redistribute xPilot or derivative binaries.
- Track **hashes + profiles + research** in git only.
- Product framing: **private openfsd networks** the operator is authorized to use.
- Prefer reversible Apply/Revert; never delete user model/aircraft data outside the PE/config targets.
