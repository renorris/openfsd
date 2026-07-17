# Client and server capabilities (`CAPS`)

> Empirically derived from vatSys decompilation (`Capabilities.cs`, `Network.cs`),
> live-network captures documented in this project's historical FSD notes, and
> openfsd's implemented wire handlers. Not an official VATSIM specification.

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

Client → server (vatSys post-login):

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

## Lifecycle (vatSys)

After successful `#AA` / auth, vatSys immediately sends (among others):

1. `$CQ {cs}:SERVER:IP`
2. `$CQ {cs}:SERVER:CAPS` ← **requires a `$CRSERVER` CAPS reply**
3. `$CQ {cs}:SERVER:ATC:{cs}`
4. Primary `%` ATC position (and secondary vis centers only if server CAPS has `SECPOS=1`)

On receiving `$CR …:CAPS` from `SERVER`, vatSys:

1. Deserializes the flag list into `serverCapabilities`
2. Runs `CheckCompatabilityWithServer()`:
   - client `SecPos` ← server `SECPOS`
   - client `RadarUpdate` ← server `FASTPOS`
   - client `ATCInfo` ← server `ATCINFO`
   - client `ICAOEq` ← server `ICAOEQ`
3. Forces a visibility-center position refresh (`ForceSendVisUpdate`)

If the server never answers CAPS, `serverCapabilities` stays empty/null-like →
`SecPos` stays false → **secondary visibility centers are never sent**.

vatSys also:

- Answers peer `$CQ …:CAPS` with its own serialized capabilities
- May push `$CR {cs}:SERVER:CAPS:…` (`SendCapabilities`) after compatibility adjust
- Queries peer CAPS when discovering other ATC (`ClientQueryType` CAPS = 2)

ATIS secondary sessions answer CAPS with a minimal set: `VERSION=1:ATCINFO=1`.

## Known capability flags

Token form on the wire is always `{NAME}=1`. Boolean properties below map 1:1.

| Wire token     | vatSys property | Typical advertiser | Meaning (empirical) |
|----------------|-----------------|--------------------|---------------------|
| `VERSION`      | `Version`       | client + server    | Capabilities schema version (`VERSION=1`) |
| `SECPOS`       | `SecPos`        | server (gates client send) | Secondary ATC visibility centers (`PDUSecondaryVisCenter`) |
| `MODELDESC`    | `ModelDesc`     | pilot clients      | Model description / MTL-style aircraft model info |
| `ACCONFIG`     | `AcConfig`      | pilot clients      | Aircraft configuration (`$CQ …:ACC` JSON) |
| `ATCINFO`      | `ATCInfo`       | ATC clients + server | ATC info / controller-info related features |
| `NEWATIS`      | `NewATIS`       | ATC clients + server | `NEWATIS` / new-ATIS letter broadcasts |
| `ESTIMATES`    | `Estimates`     | ATC clients        | Estimate coordination (`$CQ …:EST:…`) |
| `MUMBLE`       | `Mumble`        | ATC clients        | Mumble/VSCS landline integration (not FSD routing) |
| `GLOBALDATA`   | `GlobalData`    | ATC clients + server | Global ops data (`$CQ`/`#PC` `GD`) |
| `ICAOEQ`       | `ICAOEq`        | clients + server   | ICAO equipment / equipment-code flightplan fields |
| `FASTPOS`      | `FastPos`       | **server** (also peers) | Fast pilot positions (`^`, `#SL`, `#ST`) and `$SF` enable path |
| `RADARUPDATE`  | `RadarUpdate`   | clients            | High-rate radar-style updates; vatSys sets from server `FASTPOS` |
| `VISUPDATE`    | `VisUpdate`     | pilot/tower path   | Visual position update stream (tower-client gated in vatSys) |
| `ATCMULTI`     | `ATCMulti`      | ATC clients + server | Multi-controller / multi-facility coexistence |
| `ONGOINGCOORD` | *(not in vatSys `Capabilities.cs`)* | some clients | Ongoing coordination (seen in older captures) |
| `INTERIMPOS`   | —               | rare               | Interim position updates |
| `STEALTH`      | —               | rare               | Stealth / hidden presence |
| `TEAMSPEAK`    | —               | rare               | TeamSpeak integration (legacy vs `MUMBLE`) |
| `SIMULATED`    | —               | rare               | Simulated traffic |
| `OBSPILOT`     | —               | rare               | Observer/pilot hybrid |

### Server vs client roles

| Role | Who sends `$CQ …:CAPS` | Who answers |
|------|------------------------|-------------|
| Discover server features | Client → `SERVER` | Server (`$CRSERVER:…:CAPS:…`) |
| Discover peer features | Client → peer callsign | Peer (`$CR{peer}:{requester}:CAPS:…`) |
| Server asks client | Server → client (`$CQSERVER:{cs}:CAPS`) | Client (`$CR{cs}:SERVER:CAPS:…`) — optional; openfsd does not require it |
| Unsolicited announce | Client → `SERVER` | N/A (server may ignore) |

Live VATSIM captures have shown both patterns: the network server advertising a short CAPS list (`ATCINFO=1:SECPOS=1`) and requesting client CAPS. vatSys always **queries** the server; answering that query is the compatibility-critical path.

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
| `VERSION=1` | **yes** | Schema token recognized by vatSys `Deserialize` |
| `ATCINFO=1` | **yes** | Peer ATC info paths; `RN` / controller info queries forwarded |
| `NEWATIS=1` | **yes** | `$CQ …:NEWATIS` / `NEWINFO` forwarded for ATC (range broadcast) |
| `GLOBALDATA=1` | **yes** | `$CQ …:GD` and `#PC …:GD` privileged ATC forward |
| `ICAOEQ=1` | **yes** | Flight plan store/forward is opaque; ICAO equipment strings preserved |
| `ATCMULTI=1` | **yes** | Multiple simultaneous ATC sessions; registry supports many ATC |
| `FASTPOS=1` | **yes** | `^` / `#SL` / `#ST` handlers + `$SF` send-fast enable/disable for proto 101 |
| `SECPOS` | **no** | Secondary vis centers not implemented yet (would lie to vatSys) |
| `ESTIMATES` | **no** | Peer-to-peer only; not a server gate in vatSys compatibility check |
| `MODELDESC` / `ACCONFIG` / `VISUPDATE` / `RADARUPDATE` / `MUMBLE` | **no** | Client-local features; server only relays related packets where applicable |
| `ONGOINGCOORD` and rarer flags | **no** | No proven openfsd support |

**Rule:** never advertise a flag whose absence is how clients gate a feature the server cannot honor. Especially `SECPOS` — advertising it causes vatSys to emit secondary centers the server currently drops/unknowns.

### Peer CAPS (client ↔ client)

openfsd **forwards** `$CQ`/`$CR` CAPS between peers (and ranged special recipients where applicable). The server does not synthesize peer capability lists.

### Client CAPS to `SERVER`

`$CR{callsign}:SERVER:CAPS:…` is accepted and ignored (no error). openfsd does not yet persist client capability maps for policy decisions.

## RossCarlson / ClientQueryType (vatSys)

vatSys uses `RossCarlson.Vatsim.Network` enums. Observed CAPS-related values:

| `ClientQueryType` (int) | Wire type string | Notes |
|-------------------------|------------------|-------|
| `2` | `CAPS` | Capabilities query/response |
| `1` | `ATC` | Active ATC (often sent right after CAPS) |
| `7` | `IP` | Client IP (server-only answer) |

Exact enum names live in the closed library; integers above are from decompiled call sites.

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

## References

- vatSys `artifacts/decompiled/src/vatsys/Capabilities.cs` — serialize/deserialize token list
- vatSys `Network.cs` — post-login CAPS query, `CheckCompatabilityWithServer`, `SendPosition` SECPOS gate, `SendCapabilities`
- vatSys `NetworkATC.cs` — `Estimates` / `Mumble` / `NewATIS` / `GlobalOps` derived from peer CAPS
- vatSys `AtisConnection.cs` — ATIS session CAPS answer `VERSION=1:ATCINFO=1`
- Historical live-network note: server CAPS short form `ATCINFO=1:SECPOS=1` (VATSIM public network; openfsd deliberately omits `SECPOS` until multi-center range exists)
