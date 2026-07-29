# TrackAudio — reverse-engineering research notes

**Date:** 2026-07-28  
**Prior art patchfile:** **None** in `openfsd-client-patch-utility/example_patchfiles/`  
**Legal posture:** Research notes only. **Never commit or redistribute**
TrackAudio binaries.

## Apply readiness (honest)

| Gate | Status |
|------|--------|
| Prior-art hash-pinned PE offsets | **Absent** |
| Evidence of on-disk config fields for voice base / FSD | **Insufficient for Apply** |
| Config-only adapter | **Not shipped** |
| GUI | **Coming soon** (`trackaudio` in `FutureClientCatalog`) |

**Decision for PR-11:** **docs only**. Do not invent PE offsets. Do not ship a
config-only adapter until a version-pinned config path + field map is proven
against a stock install (hashes recorded under local `.research/`).

## What TrackAudio is (product context)

TrackAudio is an **AFV-Native-derived** standalone voice client (ATC/pilot
radios). openfsd AFV design treats it as a first-class consumer of:

| Surface | openfsd |
|---------|---------|
| AFV REST base | `AFV_API_PUBLIC_BASE_URL` (HTTPS) |
| AFV UDP voice | advertised in channel config (`AFV_UDP_ADVERTISE_IPV4`, …) |
| Auth | same certificate DB as FSD (CID + password) via AFV login API |

See `docs/design/afv-server.md` and `wiki/Client-Connection.md` § Voice (AFV).

TrackAudio is **not** a full FSD pilot client like vPilot. FSD traffic
usually stays in a separate client; TrackAudio attaches for **voice**.

## Why PE patching is not the default path

AFV-Native clients historically expose **user-configurable** voice server / API
base settings (UI or config file) rather than a single hardcoded JWT URL that
must be binary-patched for private networks. If TrackAudio can point at an
operator’s AFV base URL without PE surgery, the monorepo adapter should be:

1. **Config-only** (preferred), or  
2. **Docs-only operator steps** (no adapter),  

—not `raw_overwrite` / `padded_string` invented from a decompile.

No monorepo or prior-art patchfile has recorded:

- TrackAudio version  
- primary PE SHA-1 / SHA-256  
- config file relative path + format  
- stock AFV base string sites  

Until those exist, any PE offset would be **invented** — forbidden by Client
Setup policy.

## Operator path today (no openfsd-client Apply)

1. Run openfsd with **`-afv`** and publish `AFV_API_PUBLIC_BASE_URL` over HTTPS.
2. Ensure `AFV_UDP_ADVERTISE_IPV4` (and related) are client-reachable.
3. In TrackAudio (or equivalent AFV-Native client), configure the **voice /
   AFV server URL** to that public base (exact UI labels are version-specific).
4. Log in with a CID + password present in the openfsd certificate/user DB.
5. Keep FSD in the pilot/ATC client of choice (vPilot adapter, Swift private
   server entry, etc.).

Do **not** market TrackAudio as “one-click Client Setup Apply” until a
version-pinned profile lands.

## Research gates before any adapter

### Config-only (preferred)

- [ ] Pin a TrackAudio release version + download page.
- [ ] Record installer and primary binary digests (SHA-1 + SHA-256) in
      `third_party/client-profiles/trackaudio-<ver>.yaml` (metadata only).
- [ ] Locate config path(s) under `%APPDATA%` / install dir (document globs).
- [ ] Identify field(s) for AFV REST base (and any FSD-related settings).
- [ ] Prove rewrite is reversible (backup sibling) and does not wipe unrelated keys.
- [ ] Implement `adapters/trackaudio` with `MutConfigRewrite` only; GUI enable.

### PE path (discouraged unless config impossible)

- [ ] Same hash pin as above.
- [ ] Document stock AFV base string encoding (UTF-8 / UTF-16) and file offsets
      from a real PE walk — **no invented offsets**.
- [ ] Prefer `padded_string` with measured `available_bytes`.
- [ ] Re-verify on a second clean install before enabling Apply.

## Related

- AFV server design: `docs/design/afv-server.md`
- Client Setup design: `docs/design/client-runtime-injector.md` (TrackAudio
  may be config-only — open question in design)
- Wiki: `wiki/Client-Connection.md` § Voice (AFV)
- AFV-Native: [xsquawkbox/AFV-Native](https://github.com/xsquawkbox/AFV-Native)
