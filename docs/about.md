# About

The FSD protocol functions primarily as a **message forwarder**.
Aside from a few direct client–server interactions (login, METAR, capability advertisement, range fan-out, auth challenges), its main purpose is to relay messages between flight-simulator clients via a centralized server.
This architecture is not peer-to-peer; all communication is routed through the central server.

FSD is a plaintext protocol that can be intercepted and analyzed with tools such as Wireshark.
By default it operates on **TCP port 6809**, and its functionality can be tested with [telnet](https://linux.die.net/man/1/telnet):

```text
telnet <FSD server address> 6809
```

Various implementations of FSD exist, each with unique protocol nuances.
This site documents the **modern VATSIM dialect**. openfsd-specific behavior is called out where the server intentionally differs or is incomplete.

## Wire conventions

- FSD messages are plaintext lines ending with **CR/LF** (`\r\n`, `0x0d0a`).
- Effective character set on the wire is [ISO/IEC 8859-1](https://en.wikipedia.org/wiki/ISO/IEC_8859-1) (“Latin alphabet no. 1”).
- Each message begins with a short **packet identifier**, followed by colon (`:`) delimited fields.
- Numerical values are base-10 (or occasionally base-16) ASCII; no raw binary payload.
- Clients are identified by plaintext aviation callsigns (e.g. `N7938C`, `KSFO_TWR`).
- Most packets include **From** and **To** (source and recipient). **To** may be a callsign, `SERVER`, or a special group token (see [Special Recipients](protocol.md#special-recipients)).

## Example: Server Identification (`$DI`)

```text
$DISERVER:CLIENT:VATSIM FSD V3.43:d95f57db664f\r\n
```

##### Hexadecimal representation

```text
00000000  24 44 49 53 45 52 56 45  52 3a 43 4c 49 45 4e 54   $DISERVE R:CLIENT
00000010  3a 56 41 54 53 49 4d 20  46 53 44 20 56 33 2e 34   :VATSIM  FSD V3.4
00000020  33 3a 64 39 35 66 35 37  64 62 36 36 34 66 0d 0a   3:d95f57 db664f..
```

##### Explanation

| Piece | Value | Notes |
|-------|-------|-------|
| Packet type | `$DI` | Server identification |
| From | `SERVER` | Reserved callsign for the server |
| To | `CLIENT` | Placeholder used before the client callsign is known |
| Version string | `VATSIM FSD V3.43` | Human-readable server software version (format varies by server) |
| Initial challenge | `d95f57db664f` | Random hex data for [VATSIM Auth](vatsim-auth.md) |
| Terminator | `\r\n` | `0x0d 0x0a` |

Full packet catalog: [Protocol](protocol.md).
