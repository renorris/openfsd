# Enumerations

## Network Ratings

| Shorthand Identifier | Name                              | Protocol Value | Description                                                                                    |
|----------------------|-----------------------------------|----------------|------------------------------------------------------------------------------------------------|
| `OBS`                | Observer                          | `1`            | The default rating.<br>Observer-only permissions for ATC.<br>Used for pilot connections.       |
| `S1`                 | Student 1 / Tower Trainee         | `2`            | Initial rating given to new ATCs.                                                              |
| `S2`                 | Student 2 / Tower Controller      | `3`            | All aerodrome control services: Delivery (DEL), Ground (GND) and Tower (TWR).                  |
| `S3`                 | Student 3 / Senior Student        | `4`            | Approach (APP) and Departure (DEP) positions.                                                  |
| `C1`                 | Controller 1 / Enroute Controller | `5`            | 'Enroute' or 'Area' sectors (CTR); both radar and non-radar control services.                  |
| `C2`                 | Controller 2                      | `6`            | Not in use.                                                                                    |
| `C3`                 | Controller 3 / Senior Controller  | `7`            | A further rating granted by divisions. No increased privileges.                                |
| `I1`                 | Instructor 1                      | `8`            | ATC Instructor                                                                                 |
| `I2`                 | Instructor 2                      | `9`            | Not in use.                                                                                    |
| `I3`                 | Instructor 3 / Senior Instructor  | `10`           |                                                                                                |
| `SUP`                | Supervisor                        | `11`           | Responsible for answering questions, providing technical support and enforcing the VATSIM CoC. |
| `ADM`                | Administrator                     | `12`           |                                                                                                |

## Facility Types

Serialization values for different ATC facility types.

| Name                              | Protocol Value | 
|-----------------------------------|----------------|
| Observer                          | 0              |
| Flight Service Station            | 1              |
| Delivery                          | 2              |
| Ground                            | 3              |
| Tower                             | 4              |
| Approach                          | 5              |
| Centre                            | 6              |

# Pilot Ratings

Serialization values for different pilot ratings used in the system.

| Name              | Protocol Value |
|-------------------|----------------|
| Student           | 1              |
| Private Pilot     | 2              |
| Instrument Pilot  | 3              |
| Flight Instructor | 4              |
| DPE               | 5              |

## Client Capabilities

- Capabilities a client or the server can advertise on the wire via `$CQ`/`$CR` **`CAPS`**.
- On the wire each supported flag appears as `{ID}=1` (e.g. `ATCINFO=1`). Absent means unsupported.
- Deep reference (vatSys gates, openfsd server advertisement, lifecycle): **[capabilities.md](capabilities.md)**.

| Shorthand Identifier | Name                     | Description |
|----------------------|--------------------------|-------------|
| `VERSION`            | Version                  | Capabilities schema version; always `VERSION=1` when present |
| `ATCINFO`            | ATC Info                 | ATC/controller info features; server advertises when ATC info paths work |
| `MODELDESC`          | Model Description        | Aircraft model description (typically pilot clients) |
| `ACCONFIG`           | Aircraft Configuration   | Aircraft config JSON via `$CQ …:ACC` (typically pilot clients) |
| `VISUPDATE`          | Visual Position Updates  | High-rate visual position stream |
| `RADARUPDATE`        | Radar Updates            | Radar-style high-rate updates; vatSys mirrors server `FASTPOS` into this |
| `ATCMULTI`           | ATC Multi                | Multi-controller coexistence / multi-facility operation |
| `SECPOS`             | Secondary Position       | Secondary ATC visibility centers; **do not advertise unless server implements multi-center range** |
| `ICAOEQ`             | ICAO Equipment Suffixes  | ICAO equipment / equipment-code flightplan fields |
| `FASTPOS`            | Fast Position Updates    | Fast pilot positions (`^`, `#SL`, `#ST`) and related `$SF` enable path |
| `ONGOINGCOORD`       | Ongoing Coordination     | Ongoing coordination (seen in some client CAPS; not used by vatSys `Capabilities.cs`) |
| `INTERIMPOS`         | Interim Position Updates | Interim position updates |
| `STEALTH`            | Stealth Mode             | Stealth / reduced presence |
| `TEAMSPEAK`          | TeamSpeak Integration    | Legacy TeamSpeak integration |
| `NEWATIS`            | New ATIS                 | `NEWATIS` / new-ATIS letter broadcasts |
| `MUMBLE`             | Mumble Integration       | Mumble/VSCS landline-related client features |
| `GLOBALDATA`         | Global Data              | Global ops data (`GD` via `$CQ` / `#PC`) |
| `ESTIMATES`          | Estimates                | Estimate coordination (`EST` client queries) |
| `SIMULATED`          | Simulated                | Simulated traffic marker |
| `OBSPILOT`           | Observer/Pilot           | Observer/pilot hybrid |

### openfsd server advertisement

openfsd currently replies to `$CQ …:SERVER:CAPS` with:

`VERSION=1:ATCINFO=1:NEWATIS=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1:FASTPOS=1`

`SECPOS` is intentionally omitted until secondary visibility centers are implemented.

## Simulator Types

Serialization values for different flight simulators.

| Name                                | Protocol Value |
|-------------------------------------|----------------|
| Unknown                             | `0`            |
| Microsoft Flight Simulator 95       | `1`            |
| Microsoft Flight Simulator 98       | `2`            |
| Microsoft Combat Flight Simulator   | `3`            |
| Microsoft Flight Simulator 2000     | `4`            |
| Microsoft Combat Flight Simulator 2 | `5`            |
| Microsoft Flight Simulator 2002     | `6`            |
| Microsoft Combat Flight Simulator 3 | `7`            |
| Microsoft Flight Simulator 2004     | `8`            |
| Microsoft Flight Simulator X        | `9`            |
| Microsoft Flight Simulator 2020     | `10`           |
| Microsoft Flight Simulator 2024     | `11`           |
| X-Plane 8                           | `12`           |
| X-Plane 9                           | `13`           |
| X-Plane 10                          | `14`           |
| X-Plane 11                          | `15`           |
| X-Plane 12                          | `16`           |
| Prepar3D v1                         | `17`           |
| Prepar3D v2                         | `18`           |
| Prepar3D v3                         | `19`           |
| Prepar3D v4                         | `20`           |
| Prepar3D v5                         | `21`           |
| FlightGear                          | `22`           |

## Flight Rules

Serialization values for the different types of flight rules.

| Name  | Protocol Value |
|-------|----------------|
| DVFR  | `D`            |
| SVFR  | `S`            |
| VFR   | `V`            |
| IFR   | `I`            |

## Server Error Codes

| Error Code                     | Description                  |
|--------------------------------|------------------------------|
| `0` (`NoError`)                | No error                     |
| `1` (`CallsignInUse`)          | Callsign in use              |
| `2` (`InvalidCallsign`)        | Invalid callsign             |
| `3` (`AlreadyRegistered`)      | Already registered           |
| `4` (`SyntaxError`)            | Syntax error                 |
| `5` (`InvalidSrcCallsign`)     | Invalid source callsign      |
| `6` (`InvalidCidPassword`)     | Invalid CID/password         |
| `7` (`NoSuchCallsign`)         | No such callsign             |
| `8` (`NoFlightPlan`)           | No flight plan               |
| `9` (`NoWeatherProfile`)       | No such weather profile      |
| `10` (`InvalidRevision`)       | Invalid protocol revision    |
| `11` (`RequestedLevelTooHigh`) | Requested level too high     |
| `12` (`ServerFull`)            | Server full                  |
| `13` (`CidSuspended`)          | CID/PID suspended            |
| `14` (`InvalidCtrl`)           | Invalid control              |
| `15` (`RatingTooLow`)          | Rating too low               |
| `16` (`InvalidClient`)         | Unauthorized client software |
| `17` (`AuthTimeout`)           | Authorization timeout        |
| `18` (`Unknown`)               | Unknown error                |

