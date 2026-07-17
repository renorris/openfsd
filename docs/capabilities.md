# Client and server capabilities (`CAPS`)

> Empirically derived from modern ATC/pilot client behavior and openfsd’s wire handlers.  
> **Not** an official VATSIM specification.

## Wire format

**Query** (`$CQ`):

```text
$CQ{from}:{to}:CAPS
```

- No payload fields.
- `{to}` is either `SERVER` or a peer callsign.

**Response** (`$CR`):

```text
$CR{from}:{to}:CAPS:{FLAG}=1[:{FLAG}=1…]
```

Notes:

- Only flags with value `1` are present. Absent flags mean unsupported/unknown.
- Server responses use the compacted source token `SERVER` (no extra colon):
  `$CRSERVER:{callsign}:CAPS:…` — same pattern as `$CRSERVER:…:ATC:…` and `$CRSERVER:…:IP:…`.
- Flag order is not significant; receivers match on exact `NAME=1` tokens.
- Capabilities format version is advertised as `VERSION=1` when present.

### Examples

Client → server (typical post-login):

```text
$CQKSFO_TWR:SERVER:CAPS
```

Server → client:

```text
$CRSERVER:KSFO_TWR:CAPS:VERSION=1:ATCINFO=1:NEWATIS=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1:FASTPOS=1
```

Peer → peer (pilot or ATC advertising their client features):

```text
$CQJBU325:JBU1005:CAPS
$CRJBU1005:JBU325:CAPS:VERSION=1:ATCINFO=1:MODELDESC=1:ACCONFIG=1:VISUPDATE=1:ATCMULTI=1
```

Client → server (unsolicited or answer to server-initiated query):

```text
$CRKSFO_TWR:SERVER:CAPS:VERSION=1:SECPOS=1:ATCINFO=1:NEWATIS=1:ESTIMATES=1:MUMBLE=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1
```

## Lifecycle (modern ATC clients)

After successful `#AA` / auth, many ATC clients immediately send (among others):

1. `$CQ {cs}:SERVER:IP`
2. `$CQ {cs}:SERVER:CAPS` ← **requires a `$CRSERVER` CAPS reply**
3. `$CQ {cs}:SERVER:ATC:{cs}`
4. Primary `%` ATC position (and secondary vis centers only if server CAPS has `SECPOS=1`)

On receiving `$CR …:CAPS` from `SERVER`, clients typically:

1. Parse the flag list into server capability state
2. Gate client features on server flags, commonly:
   - secondary vis centers ← server `SECPOS`
   - radar / high-rate related flags ← server `FASTPOS`
   - ATC info features ← server `ATCINFO`
   - ICAO equipment fields ← server `ICAOEQ`
3. Refresh visibility / position updates if needed

If the server never answers CAPS, server capabilities stay empty → **secondary visibility centers are never sent** when gated on `SECPOS`.

Clients also:

- Answer peer `$CQ …:CAPS` with their own serialized capabilities
- May push `$CR {cs}:SERVER:CAPS:…` after compatibility adjust
- Query peer CAPS when discovering other ATC

ATIS secondary sessions often answer CAPS with a minimal set: `VERSION=1:ATCINFO=1`.

## Known capability flags

Token form on the wire is always `{NAME}=1`.

| Wire token     | Typical advertiser | Meaning (empirical) |
|----------------|--------------------|---------------------|
| `VERSION`      | client + server    | Capabilities schema version (`VERSION=1`) |
| `SECPOS`       | server (gates client send) | Secondary ATC visibility centers |
| `MODELDESC`    | pilot clients      | Model description / MTL-style aircraft model info |
| `ACCONFIG`     | pilot clients      | Aircraft configuration (`$CQ …:ACC` JSON) |
| `ATCINFO`      | ATC clients + server | ATC info / controller-info related features |
| `NEWATIS`      | ATC clients + server | `NEWATIS` / new-ATIS letter broadcasts |
| `ESTIMATES`    | ATC clients        | Estimate coordination (`$CQ …:EST:…`) |
| `MUMBLE`       | ATC clients        | Mumble/VSCS landline integration (not FSD routing) |
| `GLOBALDATA`   | ATC clients + server | Global ops data (`$CQ`/`#PC` `GD`) |
| `ICAOEQ`       | clients + server   | ICAO equipment / equipment-code flightplan fields |
| `FASTPOS`      | **server** (also peers) | Fast pilot positions (`^`, `#SL`, `#ST`) and `$SF` enable path |
| `RADARUPDATE`  | clients            | High-rate radar-style updates; often derived from server `FASTPOS` |
| `VISUPDATE`    | pilot/tower path   | Visual position update stream |
| `ATCMULTI`     | ATC clients + server | Multi-controller / multi-facility coexistence |
| `ONGOINGCOORD` | some clients       | Ongoing coordination (rare / legacy) |
| `NEWINFO`      | some clients       | Related to new-info broadcasts; separate from `NEWATIS` |
| `INTERIMPOS`   | rare               | Interim position updates — **unconfirmed semantics** |
| `STEALTH`      | rare               | Stealth / hidden presence — **unconfirmed semantics** |
| `TEAMSPEAK`    | rare               | TeamSpeak integration (legacy vs `MUMBLE`) |
| `SIMULATED`    | rare               | Simulated traffic — **unconfirmed semantics** |
| `OBSPILOT`     | rare               | Observer/pilot hybrid — **unconfirmed semantics** |

Unknown tokens are typically ignored by receivers that only match known names.

### Server vs client roles

| Role | Who sends `$CQ …:CAPS` | Who answers |
|------|------------------------|-------------|
| Discover server features | Client → `SERVER` | Server (`$CRSERVER:…:CAPS:…`) |
| Discover peer features | Client → peer callsign | Peer (`$CR{peer}:{requester}:CAPS:…`) |
| Server asks client | Server → client (`$CQSERVER:{cs}:CAPS`) | Client (`$CR{cs}:SERVER:CAPS:…`) — optional; openfsd does not require it |
| Unsolicited announce | Client → `SERVER` | N/A (server may ignore) |

Live VATSIM captures have shown both patterns: the network server advertising a short CAPS list (`ATCINFO=1:SECPOS=1`) and requesting client CAPS. Answering the client’s **server CAPS query** is the compatibility-critical path for feature gates such as `SECPOS`.

## openfsd server CAPS

openfsd answers:

```text
$CQ{callsign}:SERVER:CAPS
```

with:

```text
$CRSERVER:{callsign}:CAPS:VERSION=1:ATCINFO=1:NEWATIS=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1:FASTPOS=1
```

### Flag justification (what is actually implemented)

| Flag | Advertised? | Empirical basis in openfsd |
|------|-------------|----------------------------|
| `VERSION=1` | **yes** | Schema token expected by modern clients |
| `ATCINFO=1` | **yes** | Peer ATC info paths; `RN` / controller info queries forwarded |
| `NEWATIS=1` | **yes** | `$CQ …:NEWATIS` / `NEWINFO` forwarded for ATC (range broadcast) |
| `GLOBALDATA=1` | **yes** | `$CQ …:GD` and `#PC …:GD` privileged ATC forward |
| `ICAOEQ=1` | **yes** | Flight plan store/forward is opaque; ICAO equipment strings preserved |
| `ATCMULTI=1` | **yes** | Multiple simultaneous ATC sessions; registry supports many ATC |
| `FASTPOS=1` | **yes** | `^` / `#SL` / `#ST` handlers + `$SF` send-fast enable/disable for proto 101 |
| `SECPOS` | **no** | Secondary vis centers not implemented yet (would lie to clients that gate on it) |
| `ESTIMATES` | **no** | Peer-to-peer only; not required as a server advertisement for basic operation |
| `MODELDESC` / `ACCONFIG` / `VISUPDATE` / `RADARUPDATE` / `MUMBLE` | **no** | Client-local features; server only relays related packets where applicable |
| `ONGOINGCOORD` and rarer flags | **no** | No proven openfsd support |

**Rule:** never advertise a flag whose absence is how clients gate a feature the server cannot honor. Especially `SECPOS` — advertising it causes clients to emit secondary centers the server currently cannot apply to range.

### Peer CAPS (client ↔ client)

openfsd **forwards** `$CQ`/`$CR` CAPS between peers (and ranged special recipients where applicable). The server does not synthesize peer capability lists.

### Client CAPS to `SERVER`

`$CR{callsign}:SERVER:CAPS:…` is accepted and ignored (no error). openfsd does not yet persist client capability maps for policy decisions.

## Post-login CAPS-related sequence

Typical ATC order after `#AA`:

| Order | Wire | Notes |
|-------|------|-------|
| 1 | `$CQ {cs}:SERVER:IP` | Public IP |
| 2 | `$CQ {cs}:SERVER:CAPS` | **Requires** `$CRSERVER` reply |
| 3 | `$CQ {cs}:SERVER:ATC:{cs}` | Self “valid ATC” query |
| 4 | `%` | Primary ATC position |

Partial integer enum map used by some libraries: [enumerations.md](enumerations.md#client-query-type-ordinals-partial).

## Implementation map (openfsd)

| Concern | Location |
|---------|----------|
| Server CAPS payload constant | `internal/server/caps.go` |
| `$CQ …:SERVER:CAPS` handler | `internal/server/handler_query.go` |
| Peer CAPS forward | `handleClientQuery` allowlist (`CAPS`) |
| Fast position support (`FASTPOS`) | `handler_position.go` (`handleFastPilotPosition`, `$SF`) |
| NEWATIS / GD / EST forward | `handler_query.go`, `handleProcontroller` |
| Protocol overview | [protocol.md — CAPS](protocol.md#caps-capabilities) |
| Flag table | [enumerations.md — Client Capabilities](enumerations.md#client-capabilities) |
