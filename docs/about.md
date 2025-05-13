# About

The FSD protocol functions primarily as a message forwarder.
Aside from a few direct client-server interactions, its main purpose is to relay messages between flight simulator clients via a centralized server.
This architecture is not peer-to-peer; all communication is routed through the central server.

FSD is a plaintext protocol that can be easily intercepted and analyzed with tools such as Wireshark. 
By default, it operates on TCP port 6809, and its functionality can be tested using [telnet](https://linux.die.net/man/1/telnet):

```
telnet <FSD server address> 6809
```

Various implementations of FSD exist, each with unique protocol nuances.
This project specifically replicates VATSIM behavior, which differs from other networks such as [IVAO](https://www.ivao.aero/).

- FSD messages consist of plaintext MS-DOS-style lines ending with CR/LF characters.
- The protocol is strictly limited to the [ISO/IEC 8859-1](https://en.wikipedia.org/wiki/ISO/IEC_8859-1) aka. 'Latin alphabet no. 1' character set.
- Each message, or 'line', begins with a 1- or 3-character packet identifier, followed by colon (`:`) delimited fields.
- All numerical values are encoded as base-10 (or occasionally base-16) ASCII strings, with no raw binary data used.
- Clients are identified by plaintext aviation callsigns (e.g., `N7938C`). 
- Most packets include "From" and "To" fields, which serve as source and recipient identifiers, respectively. 
- Depending on the packet type, the "To" field may specify a single recipient or a group of clients.

#### Example [Server Identification](/packets/#server-identification-di) Packet

```text
$DISERVER:CLIENT:VATSIM FSD V3.43:d95f57db664f\r\n
```

##### Hexadecimal Representation

```text
00000000  24 44 49 53 45 52 56 45  52 3a 43 4c 49 45 4e 54   $DISERVE R:CLIENT
00000010  3a 56 41 54 53 49 4d 20  46 53 44 20 56 33 2e 34   :VATSIM  FSD V3.4
00000020  33 3a 64 39 35 66 35 37  64 62 36 36 34 66 0d 0a   3:d95f57 db664f..
```

##### Explanation
- Packet Type Identifier: `$DI`
- Fields are delimited by colon (`:`) characters.
- Sender: `SERVER` (reserved callsign for the server)<br>
- Recipient: `CLIENT` (placeholder used for an unknown client)<br>
- Server version identifier: `VATSIM FSD V3.43`<br>
- Random data (see [documentation](/packets/#server-identification-di)): `d95f57db664f`
- Packet is terminated with the delimiter sequence: `\r\n` (`0x0d0a`)

For technical documentation on packet types, see [Protocol](/protocol/).
