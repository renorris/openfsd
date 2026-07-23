# X-Plane airport data (apt.dat) — sources and licensing

openfsd’s sweatbox tooling can convert X-Plane **Global Airports** `apt.dat`
layouts into TWRTrainer-style `.apt` files. This document is the packaging and
legal posture for the public repository.

**Not legal advice.** It summarizes published Laminar Research / Scenery Gateway
materials so contributors can keep the repo clean. When in doubt, prefer the
official Gateway API or a local X-Plane install, and do not commit third-party data.

---

## What openfsd ships (and what it does not)

| Ships in git / releases | Does **not** ship |
|-------------------------|-------------------|
| Converter (`cmd/aptdat2apt`) | Full Global Airports `apt.dat` |
| Optional downloader code (`internal/xp12aptdat`) | CDN zip blobs |
| Tiny hand-authored sweatbox samples under `internal/sweatbox/testdata/` | Bulk `generated-apt/*.apt` trees |
| This notice | Laminar 3D art, DSF, libraries, payware |

End users obtain airport **source data on their own machine**. openfsd never
hosts or redistributes Laminar’s Global Airports database.

Ignored by git (see root `.gitignore`):

- `xp12-aptdat/`
- `apt.dat` / `apt.dat.zip`
- `generated-apt/`

---

## What the data actually is

Global Airports `apt.dat` is the community / Laminar **airport layout database**
(runways, taxi networks, ramps, etc.) used by X-Plane. It is **not** the same
as:

- paid X-Plane product DRM / “clearancedelivery” content,
- Laminar 3D object libraries and textures,
- third-party payware scenery.

openfsd only needs **layout geometry** (2D airport network), which is exactly
what `apt.dat` provides.

---

## License of Global Airports / Gateway layouts

Published facts used by this project:

1. **Scenery Gateway packs include GPLv2.** Moderators and docs treat Gateway
   downloads as licensed under the **GNU GPL version 2** via the pack’s
   `COPYING` file. Derivative scenery may be redistributed (including
   commercially) if GPLv2 obligations are met (license copy, source/design
   availability as required by GPL).

2. **Global Airports ships a `COPYING` file** under
   `Global Scenery/Global Airports/` (GPL). That is the license for the default
   airport set that ships with X-Plane.

3. **Historical apt.dat specifications** (e.g. 1000 / 1050) stated that the
   airport data was released under the **GNU GPL**, expressly to encourage
   modification and redistribution, including in commercial derivatives, and
   required keeping the copyright banner intact when redistributing the data
   file itself.

4. **Third-party commercial products** (e.g. AirEFB) publicly credit Global
   Airports as freely obtainable from [gateway.x-plane.com](https://gateway.x-plane.com/)
   under **GPL-2.0**.

5. **Laminar publishes an official Gateway API** and the open-source
   [`xplane_airports`](https://github.com/X-Plane/xplane_airports) Python tools
   for parsing `apt.dat` and downloading Gateway packs — evidence that
   programmatic, non-malicious access to this data is expected.

**Implication for openfsd:** redistributing the raw Global Airports / Gateway
layout data is generally allowed **under GPLv2 terms**. We still **choose not
to redistribute it** in this repo: smaller clones, fresher data for users, and
zero risk of accidentally shipping something outside the layout data set
(art assets, DRM paths, etc.).

---

## Preferred ways for end users to obtain `apt.dat`

Use these in order of “most obviously blessed by Laminar”:

### 1. Local X-Plane 12 install (best if available)

If the user already has X-Plane 12 (or the free demo where Global Airports is
installed):

```text
…/Global Scenery/Global Airports/Earth nav data/apt.dat
```

```bash
go run ./cmd/aptdat2apt -aptdat "/path/to/X-Plane 12/Global Scenery/Global Airports/Earth nav data/apt.dat" \
  -out generated-apt -quiet
```

`aptdat2apt` also searches common install locations when `-aptdat` is omitted.

### 2. Official Scenery Gateway API (public, documented)

API docs: [https://gateway.x-plane.com/api](https://gateway.x-plane.com/api)

Each scenery pack ZIP includes `apt.dat` (per airport) plus `COPYING` (GPLv2).
Suitable when you need specific airports or want the clearest “official API”
path. Laminar asks clients to be considerate of server load (do not hammer
bulk endpoints unnecessarily).

### 3. User-initiated Global Airports bulk download (optional convenience)

```bash
# Download latest Global Airports apt.dat, then convert (one step)
go run ./cmd/aptdat2apt -download -out generated-apt -quiet

# Download only
go run ./cmd/download-xp12-aptdat -out ./xp12-aptdat
```

What this does:

- Reads the **public** XP12 server list (`lookup.x-plane.com`).
- Fetches `Global Scenery/Global Airports/Earth nav data/apt.dat.zip` from the
  **unsecured** CDN prefix (no product key / XDD). That is the same
  non-DRM content tree the official installer uses for Global Airports.
- Writes files **only on the user’s machine**. openfsd never re-hosts them.

This is intentionally **client-side**: the repository contains only the
fetch/convert **program**, not the data.

---

## Derived `.apt` files (sweatbox)

Converting `apt.dat` → openfsd/TWRTrainer `.apt` produces a **derivative of
GPL-licensed layout data** when the source is Global Airports / Gateway.

If **you** redistribute bulk generated `.apt` files outside this repo:

1. Treat them as **GPLv2-derived data** (include a notice + point at source).
2. Prefer regenerating from current `apt.dat` rather than shipping stale dumps.
3. Do **not** claim Laminar Research endorsement.

openfsd’s **converter source code** remains under this project’s MIT license.
MIT tooling that *reads* GPL data at runtime does not relicense the server
binary; obligations attach if you **redistribute the GPL data or derivatives**.

Hand-written fixtures under `internal/sweatbox/testdata/` are project test
assets, not a Global Airports dump.

---

## CDN / ToS notes (why this is OK for openfsd)

| Concern | Posture |
|---------|---------|
| Redistributing paid X-Plane content | We don’t. Only optional client fetch of unsecured Global Airports. |
| Product-key / DRM bypass | Not used. No XDD / clearancedelivery. |
| Hosting apt.dat on GitHub/ghcr | Forbidden by project policy (gitignored). |
| Reverse-engineered installer URLs | Same unsecured paths the public installer uses for this package; data is GPL layout data. Prefer local install or Gateway if you want the more “official” surface. |
| Rate / load abuse | Be polite to Laminar CDNs and Gateway; no CI that downloads world airports on every PR. |
| 3D objects / art libraries | Out of scope — never downloaded by this tool. |

---

## Attribution (include when redistributing data or bulk derivatives)

Suggested short notice:

```text
Airport layout data originates from the X-Plane Scenery Gateway / Global
Airports database (Laminar Research and community contributors), licensed
under the GNU General Public License version 2 as published with those
packages (see COPYING). openfsd does not claim ownership of that data.
Format reference: https://developer.x-plane.com/article/airport-data-apt-dat-12-00-file-format-specification/
Gateway: https://gateway.x-plane.com/
```

---

## Quick operator recipe

```bash
# One-shot: fetch Global Airports apt.dat locally, emit sweatbox .apt files
go run ./cmd/aptdat2apt -download -out generated-apt -quiet

# Point sweatbox / ops at generated-apt/ (local path; not committed)
```

See also: `cmd/aptdat2apt`, `cmd/download-xp12-aptdat`, `internal/xp12aptdat`.
