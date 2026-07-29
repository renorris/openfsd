# vatSys 1.4.19 — reverse-engineering research notes

**Date:** 2026-07-28  
**Prior art:** `openfsd-client-patch-utility` example  
`example_patchfiles/vatsys/vatsys-1.4.19.yaml`  
**Legal posture:** Research notes + hashes only. **Never commit or redistribute**
vatSys binaries. Users install from the vendor.

Tracked fingerprint mirror: `third_party/client-profiles/vatsys-1.4.19.yaml`

## Apply readiness (honest)

| Gate | Status |
|------|--------|
| Prior-art hash + section map + CIL `#US` / ldstr sites | **Present** (not invented) |
| Monorepo re-verify of stock PE SHA-1 against vendor binary | **OPEN** — no PE in tree (legal) |
| Stock `#US` string budgets measured for openfsd URL lengths | **OPEN** (prior art enforces “new ≤ existing” at apply time) |
| Schema-v2 embed profile + enabled adapter | **Not shipped** (PR-11 scaffold) |
| GUI | **Coming soon** (`vatsys` in `FutureClientCatalog`) |

**Decision for PR-11:** document offsets and keep **Apply disabled**. Prior art is
solid for a **CLR / managed** adapter family (same conceptual path as vPilot:
`#US` body rewrite + `ldstr` token remap), but:

1. We have not re-verified the stock PE hash in-tree.
2. `#US` budgets are **implicit** (read live string length) rather than
   published as `payload_budget_bytes` — production planning needs explicit
   budgets + free-slot strategy for long hosts.
3. Extra behavioral patches (CID length, force server-list reload) need a
   product decision before Apply.

## Fingerprints (from prior art)

| Artifact | SHA-1 |
|----------|-------|
| `vatSys.exe` | `b8050748bc436ce4b870d95a37a2eeb20532ab30` |

| Field | Value |
|-------|--------|
| Default install | `C:\Program Files (x86)\vatSys\bin\vatSys.exe` |
| Stack (expected) | .NET / CLR managed PE (uses `#US` + CIL `ldstr`) |
| Prior-art profile name | `Default Patchfile for vatSys 1.4.19` |

Installer digests unknown in prior art. Capture under `.research/` before Apply.

## Connection surfaces

| Surface | Prior-art handling | openfsd target |
|---------|--------------------|----------------|
| FSD JWT URL | `#US` write + `ldstr` token overwrite | `https://{host}/api/v1/fsd-jwt` (or short `/j`) |
| status.txt URL | `#US` write + `ldstr` token overwrite | `https://{host}/api/v1/data/status.txt` |
| CID length requirement | `raw_overwrite` → `0x17` (ldc.i4.1) | allow non-7-digit private CIDs |
| Server list reload | `raw_overwrite` → `0x16` (ldc.i4.0) | force reload of status-derived list |
| AFV | not in prior art | separate; vatSys voice path TBD |

## Prior-art section map

| Section | `raw_offset` | `virtual_start` | Notes |
|---------|-------------:|----------------:|-------|
| `file` | `0x00` | `0x00` | whole-file addressing (CIL / PE raw) |
| `userstring-heap` | `0x12DD4C` | `0x00` | `#US` stream base; `section_address` is heap-relative |

VA → file for `file` section: **file_offset = section_address** (raw 0, VA 0).

VA → file for `#US` heap header:

```text
file_offset = 0x12DD4C + section_address
```

## Prior-art mutations (with computed file offsets)

### A. Section overwrites (`section: file` → file offset = VA)

| Name | File offset | `new_bytes` | Meaning (prior art intent) |
|------|------------:|-------------|----------------------------|
| Overwrite fsd-jwt `ldstr` | `0x84F94` | `72 A8 18 00 70` | CIL `ldstr` → `#US` token `0x0018A8` |
| Overwrite status.txt `ldstr` | `0x8BC2E` | `72 84 17 01 70` | CIL `ldstr` → `#US` token `0x011784` |
| Overwrite CID length requirement | `0x5E761` | `17` | `ldc.i4.1` (relax length check) |
| Force server list reload | `0x8451D` | `16` | `ldc.i4.0` (force path) |

CIL `ldstr` encoding: opcode `0x72` + little-endian metadata token
`0x70xxxxxx` where the low 24 bits are the `#US` heap offset of the string
**header** (ECMA-335).

| Logical string | `#US` heap offset (header) | `ldstr` token bytes |
|----------------|---------------------------:|---------------------|
| new fsd-jwt slot | `0x0018A8` | `72 A8 18 00 70` |
| new status.txt slot | `0x011784` | `72 84 17 01 70` |

### B. CIL userstring patches (`section: userstring-heap`)

| Name | Heap off | **File offset (header)** | New string (template) |
|------|---------:|-------------------------:|------------------------|
| Write new fsd-jwt URL | `0x0018A8` | `0x12DD4C + 0x18A8 = 0x12F5F4` | `https://{host}/api/v1/fsd-jwt` |
| Write new status.txt URL | `0x011784` | `0x12DD4C + 0x11784 = 0x13F4D0` | `https://{host}/api/v1/data/status.txt` |

Prior-art apply path:

1. Read existing `#US` entry at header offset (compressed length + UTF-16 body).
2. Refuse if `len(new_string) > len(existing_string)` (rune/char length in prior art).
3. Rewrite length prefix + body in place (no pad-to-stock-budget like openfsd
   vPilot adapter — shorter strings leave residual stock bytes unless padded).

**openfsd adapter note:** prefer the monorepo `cilus` package (pad to stock
budget, keep prefix width stable) once stock strings and budgets are measured
from a re-verified PE — do not invent free-slot offsets.

## Product decisions before Apply

1. **CID length patch** — private openfsd often uses non-VATSIM CID shapes.
   Shipping this by default matches prior art; document the behavior in the
   operator guide when enabled.
2. **Force server list reload** — one-byte behavioral change; keep as default
   when status URL is retargeted.
3. **Credential clearing** — prior art does not touch vatSys config files; any
   config-side work is future research.
4. **Long hostnames** — if stock `#US` slots are tight, need free-slot remap
   (same R1 class as vPilot) or server short JWT aliases (`/j`).

## Suggested future profile shape (schema v2, not embedded yet)

```yaml
# Sketch only — do not enable until stock PE sha1 re-verified + budgets measured.
schema_version: 2
client_id: vatsys
supported_client_version: "1.4.19"
primary_binary:
  relative_path: vatSys.exe   # under bin/
  sha1: b8050748bc436ce4b870d95a37a2eeb20532ab30
  default_install_globs:
    windows:
      - "%ProgramFiles(x86)%\\vatSys\\bin\\vatSys.exe"
      - "%ProgramFiles%\\vatSys\\bin\\vatSys.exe"
clr:
  us_heap:
    file_offset: 0x12DD4C
    size: null   # measure on re-verify
strings:
  fsd_jwt:
    us_heap_offset: 0x0018A8
    body_file_offsets: []   # measure
    ldstr_file_offsets: [0x84F94]
    payload_budget_bytes: 0  # measure from stock
    template: "{{.JWTURL}}"
  status_txt:
    us_heap_offset: 0x011784
    ldstr_file_offsets: [0x8BC2E]
    template: "{{.StatusURL}}"
mutations:
  - id: patch_fsd_jwt
    kind: cil_us_or_remap
    string_ref: fsd_jwt
  - id: patch_status
    kind: cil_us_or_remap
    string_ref: status_txt
  - id: relax_cid_length
    kind: raw_overwrite
    file_offset: 0x5E761
    new_bytes: [0x17]
  - id: force_server_list_reload
    kind: raw_overwrite
    file_offset: 0x8451D
    new_bytes: [0x16]
```

## Research method (prior art)

- Prior art against stock `vatSys.exe` SHA-1 above.
- `#US` / `ldstr` sites from managed metadata walk + CIL scan.
- No monorepo re-extract in this PR.

## Follow-ups before enabling Apply

- [ ] Local `.research/` extract; confirm SHA-1.
- [ ] Measure stock JWT + status string bodies and `payload_budget_bytes`.
- [ ] Confirm `#US` heap size / bounds.
- [ ] Decide CID-length + force-reload product defaults.
- [ ] Implement `adapters/vatsys` reusing `cilus` + `pepatch`; synthetic fixtures only.
- [ ] GUI enable after hash-pinned Plan/Apply green.

## Related

- Design: `docs/design/client-runtime-injector.md`
- vPilot CLR notes (same family): `docs/client-injector/research/vpilot-3.12.1.md`
- Prior art: [renorris/openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility)
- Wiki: `wiki/Client-Connection.md` § vatSys
