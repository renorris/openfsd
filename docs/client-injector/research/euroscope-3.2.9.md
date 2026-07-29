# Euroscope 3.2.9 — reverse-engineering research notes

**Date:** 2026-07-28  
**Prior art:** `openfsd-client-patch-utility` example  
`example_patchfiles/euroscope/euroscope-3.2.9.yaml`  
**Legal posture:** Research notes + hashes only. **Never commit or redistribute**
Euroscope binaries. Users install from the vendor.

Tracked fingerprint mirror: `third_party/client-profiles/euroscope-3.2.9.yaml`

## Apply readiness (honest)

| Gate | Status |
|------|--------|
| Prior-art hash + section map | **Present** (not invented) |
| Monorepo re-verify of stock PE SHA-1 against vendor binary | **OPEN** — no PE in tree (legal) |
| Schema-v2 embed profile + enabled adapter | **Not shipped** (PR-11 scaffold) |
| GUI | **Coming soon** (`euroscope` in `FutureClientCatalog`) |

**Decision for PR-11:** document offsets and keep **Apply disabled**. Prior art is
as solid as a hash-pinned `padded_string` +
`raw_overwrite`), but shipping Apply without re-verifying the stock PE digest
against a user/vendor binary would risk silent wrong-offset writes on a
different build. When re-verify passes, implement a hash-pinned adapter the
same way as a native PE client: `MutRawOverwrite` + `MutPaddedString` via
`internal/clientinject/pepatch`.

## Fingerprints (from prior art)

| Artifact | SHA-1 |
|----------|-------|
| `Euroscope.exe` | `dfb1caf3d73e897b2a04964dc35867b4059bc537` |

| Field | Value |
|-------|--------|
| Default install | `C:\Program Files (x86)\Euroscope\Euroscope.exe` |
| Stack (expected) | Native Win32 PE (not CLR); version-check + JWT URL in PE image |
| Prior-art profile name | `Default Patchfile for Euroscope 3.2.9` |

Installer SHA-256 / size are **unknown** in prior art (not recorded). Capture
them under a local `.research/` extract before enabling Apply.

## Connection surfaces (what must be redirected for openfsd)

| Surface | Stock (VATSIM-era) | openfsd target | Prior-art strategy |
|---------|--------------------|----------------|--------------------|
| FSD JWT auth URL | hardcoded in PE | `https://{host}/api/v1/fsd-jwt` (or short `/j`) | `padded_string` into `.rdata`-class section |
| FSD version check | blocks non-VATSIM protocol revisions | disable / bypass | single-byte short JMP (`0xEB`) |
| Status / server list | (client-specific) | openfsd status / manual servers | **not** in this prior-art patchfile |
| AFV | N/A for classic ES FSD path | optional separate AFV client | out of scope for ES PE patch |

Euroscope is primarily an **ATC** client. Operators still need JWT retarget for
modern FSD auth when using protocol rev ≥100.

## Prior-art section map

Virtual address (VA) → file offset:

```text
file_offset = section.raw_offset + (section_address - section.virtual_start)
```

| Section | `raw_offset` | `virtual_start` |
|---------|-------------:|----------------:|
| `section1` (code / image) | `0x400` | `0x401000` |
| `section2` (string data) | `0x25AA00` | `0x65C000` |

## Prior-art mutations (with computed file offsets)

### 1. Disable FSD version check — `raw_overwrite`

| Field | Value |
|-------|--------|
| Section | `section1` |
| VA `section_address` | `0x5AE01A` |
| **File offset** | `0x400 + (0x5AE01A - 0x401000) = 0x1AD41A` |
| `new_bytes` | `[0xEB]` (unconditional short jump; replaces conditional) |

### 2. Update fsd-jwt push / immediate — `raw_overwrite`

| Field | Value |
|-------|--------|
| Section | `section1` |
| VA `section_address` | `0x4644E3` |
| **File offset** | `0x400 + (0x4644E3 - 0x401000) = 0x638E3` |
| `new_bytes` | `[0x58, 0xDE, 0x65]` |

These three bytes are the **fixed** relocation of the code that references the
JWT URL string written below. They are **not** length-dependent on the new URL
(unlike LEA length immediates used by some PE adapters). Re-verify that the instruction at this
site still matches stock before Apply.

### 3. Write new fsd-jwt URL — `padded_string`

| Field | Value |
|-------|--------|
| Section | `section2` |
| VA `section_address` | `0x65DE58` |
| **File offset** | `0x25AA00 + (0x65DE58 - 0x65C000) = 0x25C858` |
| `available_bytes` | `0x76` (118 decimal) |
| Encoding | `utf8` (NUL-padded) |
| Template | `{{.JWTURL}}` → e.g. `https://{host}/api/v1/fsd-jwt` |

**Budget:** encoded UTF-8 + trailing `0x00` must fit in 118 bytes. Default path
`https://` + host + `/api/v1/fsd-jwt` (15 fixed path chars after host) allows
hosts up to roughly **118 − 1 − 8 − 15 = 94** bytes for the full URL body
including scheme — practical FQDNs fit easily (far looser than vPilot `#US`).

## Suggested future profile shape (schema v2, not embedded yet)

```yaml
# Sketch only — do not enable until stock PE sha1 re-verified.
schema_version: 2
client_id: euroscope
supported_client_version: "3.2.9"
primary_binary:
  relative_path: Euroscope.exe
  sha1: dfb1caf3d73e897b2a04964dc35867b4059bc537
  default_install_globs:
    windows:
      - "%ProgramFiles(x86)%\\Euroscope\\Euroscope.exe"
      - "%ProgramFiles%\\Euroscope\\Euroscope.exe"
mutations:
  - id: disable_fsd_version_check
    kind: raw_overwrite
    file_offset: 0x1AD41A
    new_bytes: [0xEB]
  - id: retarget_fsd_jwt_ptr
    kind: raw_overwrite
    file_offset: 0x638E3
    new_bytes: [0x58, 0xDE, 0x65]
  - id: write_fsd_jwt_url
    kind: padded_string
    file_offset: 0x25C858
    # slot_len: 0x76, encoding: utf8, template: "{{.JWTURL}}"
```

## Research method (prior art)

- Prior art authored against a stock `Euroscope.exe` with SHA-1 above.
- Section VAs and raw offsets taken from that PE’s section headers.
- No monorepo re-extract in this PR (legal: no PE in git).

## Follow-ups before enabling Apply

- [ ] Obtain vendor Euroscope 3.2.9 install under local `.research/` (gitignored).
- [ ] Confirm `Euroscope.exe` SHA-1 == `dfb1caf3d73e897b2a04964dc35867b4059bc537`.
- [ ] Hex-dump sites `0x1AD41A`, `0x638E3`, `0x25C858` match expected stock opcodes/string.
- [ ] Capture installer digests + size into third_party / schema-v2 profile.
- [ ] Decide status/server-list strategy (manual `myservers`-style vs PE).
- [ ] Implement `adapters/euroscope` + embed profile; enable GUI slot; table-driven tests with synthetic PE fixtures (not real ES bytes).
- [ ] Antivirus / code-signing interaction notes for rewritten PE.

## Related

- Design: `docs/design/client-runtime-injector.md` (multi-client extension model)
- Prior art utility: [renorris/openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility)
- Third-party ES patch experiments: [Misaka-Nnnnq/openfsd-patch-for-es](https://github.com/Misaka-Nnnnq/openfsd-patch-for-es) (external; not a monorepo source of truth)
- Wiki: `wiki/Client-Connection.md` § Euroscope
