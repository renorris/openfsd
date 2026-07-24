# Enumerations

Unconfirmed or multi-source naming differences are called out inline.

## Protocol revisions

| Value | Name (common) | Notes |
|-------|---------------|--------|
| `9` (also historically `1`) | Classic / private FSD | Marty Bochane-style private servers |
| `10` | VATSIM pre-auth (deprecated) | Pre–client-auth VATSIM |
| `100` | VATSIM Auth | JWT + in-band auth era; common for modern ATC `#AA` |
| `101` | VATSIM Velocity (2022+) | Adds fast pilot positions (`^` / `#SL` / `#ST`) and `$SF` |

openfsd’s velocity / `$SF` path is gated on protocol revision **101**.

## Network Ratings

Used in `#AP` / `#AA` login, position packets, and privilege checks.

| Shorthand | Name | Protocol value | Description |
|-----------|------|----------------|-------------|
| *(inactive)* | Inactive | `-1` | Present in some codebases; **rare on the wire** |
| *(suspended)* | Suspended | `0` | Present in some codebases; **rare on the wire** |
| `OBS` | Observer | `1` | Default. Observer-only for ATC; used for pilot connections on the ATC rating field |
| `S1` | Student 1 / Tower Trainee | `2` | Initial ATC rating |
| `S2` | Student 2 / Tower Controller | `3` | Aerodrome control: DEL / GND / TWR |
| `S3` | Student 3 / Senior Student | `4` | APP / DEP |
| `C1` | Controller 1 / Enroute Controller | `5` | CTR / area |
| `C2` | Controller 2 | `6` | Not generally used on VATSIM |
| `C3` | Controller 3 / Senior Controller | `7` | Division-granted; no extra protocol privilege beyond rating checks |
| `I1` | Instructor 1 | `8` | ATC instructor |
| `I2` | Instructor 2 | `9` | Not generally used |
| `I3` | Instructor 3 / Senior Instructor | `10` | |
| `SUP` | Supervisor | `11` | Network supervision / CoC enforcement tools |
| `ADM` | Administrator | `12` | |

openfsd type: `protocol.NetworkRating` (`NetworkRatingObserver = 1` … `NetworkRatingAdministator = 12`; historical misspelling kept for API stability).

## Facility Types

Serialization values for ATC facility types in `%` position packets.

| Name | Protocol value | Typical callsign suffix (informal) |
|------|----------------|-------------------------------------|
| Observer | `0` | `_OBS` |
| Flight Service Station | `1` | `_FSS` |
| Delivery | `2` | `_DEL` |
| Ground | `3` | `_GND` |
| Tower | `4` | `_TWR` |
| Approach | `5` | `_APP` / `_DEP` |
| Centre | `6` | `_CTR` |

**Note:** openfsd currently answers `$CQ …:SERVER:ATC` with `Y` only when `FacilityType > 0`. Clients that self-query ATC immediately after login (before the first `%`) may therefore see `N` until a position update is processed.

## Pilot Ratings

JWT payloads on the public network carry a numeric `pilot_rating` claim ([authentication-token.md](authentication-token.md)).

IDs and names match the [VATSIM.dev pilot ratings table](https://vatsim.dev/resources/ratings/). Values are **not** a dense 0…N sequence; they increase with qualification (bitmask-shaped):

| ID | Short | Long name | Informal P-code |
|----|-------|-----------|-----------------|
| `0` | P0 | No Pilot Rating | P0 |
| `1` | PPL | Private Pilot License | P1 |
| `3` | IR | Instrument Rating | P2 |
| `7` | CMEL | Commercial Multi-Engine License | P3 |
| `15` | ATPL | Air Transport Pilot License | P4 |
| `31` | FI | Flight Instructor | P5 |
| `63` | FE | Flight Examiner | P6 |

openfsd type: `protocol.PilotRating` (`PilotRatingNone = 0` … `PilotRatingFE = 63`). Only these IDs are valid; other integers are rejected on create/update.

## Client Capabilities

- Advertised via `$CQ` / `$CR` **`CAPS`** as `{NAME}=1` tokens.
- Deep reference: **[capabilities.md](capabilities.md)**.

| Wire token | Description |
|------------|-------------|
| `VERSION` | Schema version; always `VERSION=1` when present |
| `ATCINFO` | ATC / controller-info related features |
| `MODELDESC` | Aircraft model description (typically pilot clients) |
| `ACCONFIG` | Aircraft config JSON via `$CQ …:ACC` |
| `VISUPDATE` | Visual position stream |
| `RADARUPDATE` | Radar-style high-rate updates; some clients mirror server `FASTPOS` into this |
| `ATCMULTI` | Multi-controller coexistence |
| `SECPOS` | Secondary ATC visibility centers |
| `ICAOEQ` | ICAO equipment / equipment-code FP fields |
| `FASTPOS` | Fast pilot positions + `$SF` path |
| `NEWATIS` | `NEWATIS` / new-ATIS letter broadcasts |
| `NEWINFO` | Related new-info broadcast (seen on some clients) |
| `MUMBLE` | Mumble / VSCS landline-related client features |
| `GLOBALDATA` | Global ops data (`GD` via `$CQ` / `#PC`) |
| `ESTIMATES` | Estimate coordination (`EST`) |
| `ONGOINGCOORD` | Ongoing coordination (rare / legacy) |
| `INTERIMPOS` | Interim position updates — **unconfirmed semantics** |
| `STEALTH` | Stealth / reduced presence — **unconfirmed semantics** |
| `TEAMSPEAK` | TeamSpeak integration (legacy vs `MUMBLE`) |
| `SIMULATED` | Simulated traffic marker — **unconfirmed semantics** |
| `OBSPILOT` | Observer/pilot hybrid — **unconfirmed semantics** |

### openfsd server advertisement

`$CQ …:SERVER:CAPS` →  

`VERSION=1:ATCINFO=1:NEWATIS=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1:FASTPOS=1`

`SECPOS` is advertised (`SECPOS=1`) now that secondary visibility centers are implemented.

## Simulator Types

Used in `#AP` (pilot login).

| Name | Protocol value |
|------|----------------|
| Unknown | `0` |
| Microsoft Flight Simulator 95 | `1` |
| Microsoft Flight Simulator 98 | `2` |
| Microsoft Combat Flight Simulator | `3` |
| Microsoft Flight Simulator 2000 | `4` |
| Microsoft Combat Flight Simulator 2 | `5` |
| Microsoft Flight Simulator 2002 | `6` |
| Microsoft Combat Flight Simulator 3 | `7` |
| Microsoft Flight Simulator 2004 | `8` |
| Microsoft Flight Simulator X | `9` |
| Microsoft Flight Simulator 2020 | `10` |
| Microsoft Flight Simulator 2024 | `11` |
| X-Plane 8 | `12` |
| X-Plane 9 | `13` |
| X-Plane 10 | `14` |
| X-Plane 11 | `15` |
| X-Plane 12 | `16` |
| Prepar3D v1 | `17` |
| Prepar3D v2 | `18` |
| Prepar3D v3 | `19` |
| Prepar3D v4 | `20` |
| Prepar3D v5 | `21` |
| FlightGear | `22` |

Values beyond this table are treated as unknown by common parsers.

## Flight Rules

| Name | Protocol value |
|------|----------------|
| DVFR | `D` |
| SVFR | `S` |
| VFR | `V` |
| IFR | `I` |

## Server Error Codes

| Error code | Name (openfsd / common) | Description |
|------------|-------------------------|-------------|
| `0` (`NoError`) | — | No error (rarely sent) |
| `1` (`CallsignInUse`) | Callsign in use | |
| `2` (`InvalidCallsign`) | Invalid callsign | |
| `3` (`AlreadyRegistered`) | Already registered | |
| `4` (`SyntaxError`) | Syntax error | |
| `5` (`InvalidSrcCallsign`) | Invalid source callsign | |
| `6` (`InvalidCidPassword`) | Invalid CID/password / token | |
| `7` (`NoSuchCallsign`) | No such callsign | |
| `8` (`NoFlightPlan`) | No flight plan | |
| `9` (`NoWeatherProfile`) | No such weather profile | |
| `10` (`InvalidRevision`) | Invalid protocol revision | |
| `11` (`RequestedLevelTooHigh`) | Requested level too high | |
| `12` (`ServerFull`) | Server full | |
| `13` (`CidSuspended`) | CID/PID suspended | |
| `14` (`InvalidCtrl`) | Invalid control | |
| `15` (`RatingTooLow`) | Rating too low / invalid position for rating | |
| `16` (`InvalidClient`) | Unauthorized client software | |
| `17` (`AuthTimeout`) | Authorization timeout | |
| other | Unknown | Treated as generic / message-bearing |

### Wire formatting note

Canonical padded form (many clients / docs):

```text
$ERSERVER:unknown:006::Invalid CID/password.
```

openfsd currently emits a variant:

```text
$ERserver:unknown:6::Invalid CID/password.
```

(lowercase `server`, unpadded code). Receivers should accept both. See `pkg/protocol.FormatError`.

## Client query type ordinals (partial)

Some client libraries use integer enums for `$CQ` / `$CR` type tags. The following map is **partial** and derived from observed call sites / wire correlation — not an official table.

| Int | Wire type | Notes |
|-----|-----------|--------|
| `1` | `ATC` | Post-login self-query |
| `2` | `CAPS` | Post-login + peer |
| `4` | `RN` | Real name query |
| `6` | `ATIS` | ATIS lines |
| `7` | `IP` | Public IP |
| `8` | `INF` | Info (supervisor path) |
| `9` | `FP` | Flight plan request |
| `11` | `BY` | Request relief |
| `12` | `HI` | Cancel relief request |
| `15` | `WH` | Who has |
| `16` | `IT` | Initiate track |
| `17` | `HT` | Accept handoff (query side) |
| `18` | `DR` | Drop track |
| `20` | `TA` | Temporary altitude / CFL |
| `21` | `BC` | Beacon code |
| `22` | `SC` | Scratchpad |
| `24` | `ACC` | Aircraft configuration (payload-bearing) |
| `27` | `EST` | Estimates |
| `28` | `GD` | Global data |

Other wire strings (`C?`, `NEWATIS`, `NEWINFO`, `SV`, `IPC`, `VT`, `FA`, …) are confirmed on the wire; their integer ordinals are **not fully extracted** here.
