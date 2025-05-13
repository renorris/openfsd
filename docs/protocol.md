# Protocol

| Name                                                                     | Identifier | Purpose                                                                                         |
|--------------------------------------------------------------------------|------------|-------------------------------------------------------------------------------------------------|
| [Server Identification](#server-identification-di)                       | `$DI`      | Server identification                                                                           |
| [Client Identification](#client-identification-id)                       | `$ID`      | Client identification                                                                           |
| [Add Pilot](#add-pilot-ap)                                               | `#AP`      | Login as pilot                                                                                  |
| [Add ATC](#add-atc-aa)                                                   | `#AA`      | Login as ATC                                                                                    |
| [Pilot Position](#pilot-position-)                                       | `@`        | Pilot geographical position/velocity update                                                     |
| [Fast Pilot Position](#fast-pilot-position)                              | `^`        | Pilot geographical position/velocity update, sent at a higher frequency                         |
| [Fast Pilot Position (Slow)](#fast-pilot-position-slow-variant-sl)       | `#SL`      | Pilot geographical position/velocity update, sent at a lower frequency                          |
| [Fast Pilot Position (Stopped)](#fast-pilot-position-stopped-variant-st) | `#ST`      | Pilot geographical position/velocity update, sent when the airplane is stopped (zero velocity). |
| [ATC Position](#atc-position)                                            | `%`        | ATC geographical position update                                                                |
| [Ping](#ping-pi)                                                         | `$PI`      | Ping a recipient                                                                                |
| [Pong](#pong-po)                                                         | `$PO`      | Respond to a ping                                                                               |
| [Client Query](#client-query-cq)                                         | `$CQ`      | Query information about a recipient                                                             |
| [Client Query Response](#client-query-response-cr)                       | `$CR`      | Respond to a client query                                                                       |
| [METAR Request](#metar-request-ax)                                       | `$AX`      | Request a METAR                                                                                 |
| [METAR Response](#metar-response-ar)                                     | `$AR`      | Respond to a METAR request                                                                      |
| [Wind Data](#wind-data-wd)                                               | `#WD`      | Wind data                                                                                       |
| [Cloud Data](#cloud-data-cd)                                             | `#CD`      | Cloud data                                                                                      |
| [Temperature Data](#temperature-data-td)                                 | `#TD`      | Temperature data                                                                                |
| [Initiate Handoff](#initiate-handoff-ho)                                 | `$HO`      | Initiate an ATC handoff request                                                                 |
| [Accept Handoff](#accept-handoff-ha)                                     | `$HA`      | Accept an ATC handoff request                                                                   |
| [Weather Profile Request](#weather-profile-request-wx)                   | `#WX`      | Request the server's weather profile                                                            | 
| [Flight Plan](#flight-plan-fp)                                           | `$FP`      | Send a flightplan                                                                               |
| [Flight Plan Amendment](#flight-plan-amendment-am)                       | `$AM`      | Amend a flightplan                                                                              |
| [Delete Pilot](#delete-pilot-dp)                                         | `#DP`      | Notify server before disconnecting (pilot connections)                                          |
| [Delete ATC](#delete-atc-da)                                             | `#DA`      | Notify server before disconnecting (ATC connections)                                            |                                           |
| [Kill Request](#kill-request)                                            | `$!!`      | Kick a user from the server (admin/supervisor only)                                             |
| [Auth Challenge](#auth-challenge-zc)                                     | `$ZC`      | Challenge other side of the connection using an obfuscation technique                           |
| [Auth Response](#auth-response-zr)                                       | `$ZR`      | Respond to an auth challenge                                                                    |
| [ATC Shared State](#atc-shared-state-pc)                                 | `#PC`      | ATC-specific data exchange                                                                      |
| [Plane Information](#plane-information-sb)                               | `#SB`      | Pilot-specific data exchange                                                                    |
| [Text Message](#text-message-tm)                                         | `#TM`      | Send a text message                                                                             |

<br>

## Server Identification (`$DI`)

| Field Name            | Type                           | Description                                                       | Notes           |
|-----------------------|--------------------------------|-------------------------------------------------------------------|-----------------|
| From                  | string                         | Source callsign                                                   | always `SERVER` |
| To                    | string                         | Destination callsign                                              | always `CLIENT` |
| Version               | string                         | Server software version                                           |                 |
| Initial Challenge Key | hexadecimal-encoded byte array | Initial server challenge key for "VATSIM Auth" obfuscation scheme |

- First packet sent by the server upon TCP connection establishment.
- The client must wait for this packet before continuing initialization.
- TODO: see Connection Flow

Example:
```text
$DISERVER:CLIENT:VATSIM FSD V3.50:76617473696d
```

<br>

## Client Identification (`$ID`)

| Field Name                    | Type                           | Description                                                       | Notes           |
|-------------------------------|--------------------------------|-------------------------------------------------------------------|-----------------|
| From                          | string                         | Source callsign                                                   |                 |
| To                            | string                         | Destination callsign                                              | always `SERVER` |
| Client Software ID            | hexadecimal-encoded uint16     | VATSIM-assigned client software ID                                |                 |
| Client Software Name          | string                         | Human-readable client software name                               |                 |
| Client Software Major Version | integer                        | Client software major version number                              |                 |
| Client Software Minor Version | integer                        | Client software minor version number                              |                 |
| CID                           | integer                        | VATSIM-assigned user certificate ID                               |                 |
| System UID                    | integer                        | System hardware-specific identifier                               |                 |
| Initial Challenge Key         | hexadecimal-encoded byte array | Initial client challenge key for "VATSIM Auth" obfuscation scheme |                 | 

- First packet sent by the client in the connection lifecycle.
- Sent in response to a [Server Identification](#server-identification-di)
- The Client Software ID is a static VATSIM-assigned value given to each [approved software](https://vatsim.net/docs/policy/approved-software) client. It is used as an input for the VATSIM Auth obfuscation scheme.
- The Client Software Major and Minor versions describe the release version of the client software. e.g. given `v1.0`: major version = 1, minor version = 0
- CID or "cert ID" is the VATSIM-equivalent of a username.
- The System UID is a hardware-specific identifier. Some clients [derive](https://github.com/expipiplus1/openvatsimauth/blob/9cf39462aed7316936d77f33ad91e02116b8f8c7/openvatsimauth.cpp#L175) it from the MAC address of their network card. Other clients such as vatSys generate it using hardware information from the Win32 [GetVolumeInformation()](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getvolumeinformationa) call.

Example:
```text
$IDN172SP:SERVER:88e4:vPilot:3:8:123456:-582057156:6d6973746176
```

<br>

## Add Pilot (`#AP`)

| Field Name        | Type    | Description                                             | Notes           |
|-------------------|---------|---------------------------------------------------------|-----------------|
| From              | string  | Source callsign                                         |                 |
| To                | string  | Destination callsign                                    | always `SERVER` |
| CID               | integer | VATSIM-assigned user certificate ID                     |                 |
| Token             | string  | [Authentication JWT](/authentication-token/)            |                 |
| Network Rating    | integer | VATSIM [Network Rating](/network-rating/)               | ATC Rating      |
| Protocol Revision | integer | FSD protocol revision                                   |                 |
| Simulator Type    | integer | Flight [Simulator Type](/enumerations/#simulator-types) |                 |
| Real Name         | string  | User's real name                                        |                 |

- The second packet required by the login initialization process. 
- Indicates that a client wishes to connect as a pilot.
- Sent immediately after the [Client Identification](#client-identification-id) packet.
- Upon successful login, retransmitted by the server to all other clients with the Token field omitted.

Example:
```text
#APN7938C:SERVER:100000:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJkdW1teSI6InRydWUifQ.WC-jYTEoENgaWQ9WUj9A9_-olUELBHmNOwv7UrCVn9w:1:101:2:John Doe
```

<br>

## Add ATC (`#AA`)

| Field Name        | Type    | Description                                  | Notes           |
|-------------------|---------|----------------------------------------------|-----------------|
| From              | string  | Source callsign                              |                 |
| To                | string  | Destination callsign                         | always `SERVER` |
| Real Name         | string  | User's real name                             |                 |
| CID               | integer | VATSIM-assigned user certificate ID          |                 |
| Token             | string  | [Authentication JWT](/authentication-token/) |                 |
| Network Rating    | integer | VATSIM [Network Rating](/network-rating/)    | ATC Rating      |
| Protocol Revision | integer |                                              |                 |

- The second packet required by the login initialization process.
- Indicates that a client wishes to connect as an Air Traffic Controller.
- Sent immediately after the [Client Identification](#client-identification-id) packet.
- Upon successful login, retransmitted by the server to all other clients with the Token field omitted.

Example:
```text
#AARN_OBS:SERVER:John Doe:100000:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJkdW1teSI6InRydWUifQ.WC-jYTEoENgaWQ9WUj9A9_-olUELBHmNOwv7UrCVn9w:2:100
```

<br>

## Pilot Position (`@`)

| Field Name          | Type                    | Description                                                                  | Notes                                              |
|---------------------|-------------------------|------------------------------------------------------------------------------|----------------------------------------------------|
| Transponder Mode    | char                    | Ident = `Y`, Mode C = `N`, Mode A = `S`                                      |                                                    |
| From                | string                  | Source callsign                                                              |                                                    |
| Transponder Code    | string                  | Squawk code                                                                  | Must be 4 characters each between 0 and 7          |
| Network Rating      | integer                 | VATSIM [Network Rating](/network-rating/)                                    |                                                    |
| Latitude            | floating-point number   | Geographical latitude                                                        | Formatted in decimal degrees                       |
| Longitude           | floating-point number   | Geographical longitude                                                       | Formatted in decimal degrees                       |
| True Altitude       | integer                 | Aircraft [true altitude](https://en.wikipedia.org/wiki/Altitude#In_aviation) | Unit is feet                                       |
| Groundspeed         | integer                 | Aircraft [ground speed](https://en.wikipedia.org/wiki/Ground_speed)          | Unit is knots                                      |
| Pitch/Bank/Heading  | unsigned 32-bit integer | [Encoded pitch, bank, and heading](#pitchbankheading-encoding) information   |                                                    |
| Altitude Correction | integer                 | Difference between pressure altitude and true altitude.                      | ```Correction = PressureAltitude - TrueAltitude``` |

- Sent by pilot clients at 0.2Hz intervals.
- Retransmitted by the server to all other clients within visibility range.

Example:
```text
@S:GTI8197:2000:1:40.65906:-73.79891:26:0:4290776072:359
```

<br>

## Fast Pilot Position (`^`)

| Field Name                          | Type                    | Description                                                                      | Notes                        |
|-------------------------------------|-------------------------|----------------------------------------------------------------------------------|------------------------------|
| From                                | string                  | Source callsign                                                                  |                              |
| Latitude                            | floating-point number   | Geographical latitude                                                            | Formatted in decimal degrees |
| Longitude                           | floating-point number   | Geographical longitude                                                           | Formatted in decimal degrees |
| True Altitude                       | floating-point number   | Aircraft [true altitude](https://en.wikipedia.org/wiki/Altitude#In_aviation)     | Unit is feet                 |
| Altitude AGL                        | floating-point number   | Aircraft [altitude AGL](https://en.wikipedia.org/wiki/Height_above_ground_level) | Unit is feet                 |
| Pitch/Bank/Heading                  | unsigned 32-bit integer | [Encoded pitch, bank, and heading](#pitchbankheading-encoding) information       |                              |
| Positional Velocity Vector (X-axis) | floating-point number   | Aircraft positional velocity (X-axis)                                            | Unit is radians/second       |
| Positional Velocity Vector (Y-axis) | floating-point number   | Aircraft positional velocity (Y-axis)                                            | Unit is radians/second       |
| Positional Velocity Vector (Z-axis) | floating-point number   | Aircraft positional velocity (Z-axis)                                            | Unit is radians/second       |
| Rotational Velocity Vector (X-axis) | floating-point number   | Aircraft rotational velocity (X-axis)                                            | Unit is radians/second       |
| Rotational Velocity Vector (Y-axis) | floating-point number   | Aircraft rotational velocity (Y-axis)                                            | Unit is radians/second       |
| Rotational Velocity Vector (Z-axis) | floating-point number   | Aircraft rotational velocity (Z-axis)                                            | Unit is radians/second       |
| Nose Gear Angle                     | floating-point number   | Aircraft nose gear tiller angle                                                  | Unit is degrees              |

- Sent by pilot clients supporting protocol revision `101` at **5Hz** intervals when:<br>
1\. A [Send Fast](#send-fast) packet flagged as `true` is received.<br>
- Retransmitted by the server to all other clients within visibility range supporting protocol revision `101`.
- See [Notes on Fast Pilot Positions](#fast-pilot-positions)

Example:
```text
^DAL1151:40.6354992:-73.7795597:16.81:8.10:12582828:0.0015:0.0001:0.0005:0.0001:0.0000:-0.0029:-0.40
```

<br>

## Fast Pilot Position (Slow Variant) (`#SL`)

| *Fields are identical to [Fast Pilot Position](#fast-pilot-position)*

- Sent by pilot clients supporting protocol revision `101` at **0.2Hz** intervals when:<br>
1\. A [Send Fast](#send-fast) packet flagged as `false` is received, and<br>
2\. The aircraft velocity is greater than zero.
- Retransmitted by the server to all other clients within visibility range supporting protocol revision `101`.
- See [Notes on Fast Pilot Positions](#fast-pilot-positions)

Example:
```text
#SLPRM4211:41.0844150:-73.1060790:26684.57:26961.66:4269806144:196.8918:-1.4936:174.1947:-0.0000:-0.0000:-0.0001:-2.11
```

<br>

## Fast Pilot Position (Stopped Variant) (`#ST`)

| _Fields are identical to [Fast Pilot Position](#fast-pilot-position), with **all Rotational and Positional velocity vectors omitted.**_

- Sent by pilot clients supporting protocol revision `101` at **0.2Hz** intervals when:<br>
1\. The aircraft velocity is zero.<br>
- Retransmitted by the server to all other clients within visibility range supporting protocol revision `101`.
- See [Notes on Fast Pilot Positions](#fast-pilot-positions)

Example:
```text
#STDAL2119:40.6453400:-73.7743400:13.56:-0.03:29360076:0.00
```

<br>

## ATC Position (`%`)

| Field Name        | Type                            | Description                                                                                                                                                             | Notes                                                  |
|-------------------|---------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------|
| From              | string                          | Source callsign                                                                                                                                                         |                                                        |
| Radio Frequencies | `&`-delimited array of integers | An array of 5-digit integer representations of the ATC's tuned aviation VHF radio frequencies each with the initial `1` and the decimal omitted e.g., `122.800 = 22800` | Example with multiple frequencies: `19600&20950&20150` | 
| Facility Type     | integer                         | VATSIM [Facility Type](/enumerations#facility-types)                                                                                                                    |                                                        |
| Visibility Range  | integer                         | Station [visibility range](#visibility-range)                                                                                                                           | Unit is nautical miles                                 | 
| Network Rating    | integer                         | VATSIM [Network Rating](/network-rating/)                                                                                                                               |                                                        |
| Latitude          | floating-point number           | Geographical latitude                                                                                                                                                   | Formatted in decimal degrees                           |
| Longitude         | floating-point number           | Geographical longitude                                                                                                                                                  | Formatted in decimal degrees                           |
| Unknown `0` Value | integer                         | A static field with the value `0` appended to each ATC position packet. Its purpose is unknown.                                                                         |                                                        |

- Sent by air traffic controller clients at 15-second intervals.
- Retransmitted by the server to all other clients within visibility range.

Example:
```text
%EWR_P_APP:28550:5:150:4:40.67317:-74.18533:0
```

<br>

## Ping (`$PI`)

| Field Name | Type   | Description                                                        | Notes |
|------------|--------|--------------------------------------------------------------------|-------|
| From       | string | Source callsign                                                    |       |
| To         | string | Recipient callsign                                                 |       |
| Timestamp  | string | Timestamp to be echoed in a corresponding [Pong](#pong-po) packet. |       |

- Used to ping another client.
- Responses follow the [Pong](#pong-po) format.

Example:
```text
$PIN172SP:N7938C:1736029820
```

<br>

## Pong (`$PO`)

| Field Name | Type   | Description                                                    | Notes |
|------------|--------|----------------------------------------------------------------|-------|
| From       | string | Source callsign                                                |       |
| To         | string | Recipient callsign                                             |       |
| Timestamp  | string | Echoed timestamp from a corresponding [Ping](#ping-pi) packet. |       |

- Used to respond to [Ping](#ping-pi) packets from another client.

Example:
```text
$PON7938C:N172SP:1736029820
```

<br>

## Client Query (`$CQ`)

| Field Name    | Type   | Description                                   | Notes    |
|---------------|--------|-----------------------------------------------|----------|
| From          | string | Source callsign                               |          |
| To            | string | Recipient callsign                            |          |
| Query Type    | string | see [Client Query Types](#client-query-types) |          |
| Payload       | varies | (see below)                                   | Optional |

- Used to query another client or the server for information.
- Responses follow the [Client Query Response](#client-query-response-cr) format.
- Not all queries require a response. Some simply broadcast information or a state change.
- **Payload** can consist of an arbitrary amount of separate contiguous FSD fields. It is **optional** _depending on the [type](#client-query-types)._

<br>

## Client Query Response (`$CR`)

| Field Name    | Type   | Description                                   | Notes    |
|---------------|--------|-----------------------------------------------|----------|
| From          | string | Source callsign                               |          |
| To            | string | Recipient callsign                            |          |
| Query Type    | string | see [Client Query Types](#client-query-types) |          |
| Payload       | varies | (see below)                                   | Optional |

- Used to respond to a [Client Query](#client-query-cq)
- Not all client queries have an associated response using this packet format.
- **Payload** can consist of an arbitrary amount of separate contiguous FSD fields. It is **optional** _depending on the [type](#client-query-types)._

<br>

## Client Query Types

### `ATC` (Active ATC)
- Query whether a client is an active air traffic controller (with permission to mutate flightplans/scratchpads/etc.) or just an observer.
- Only allowed to be sent directly to or from `SERVER`.
- The queried callsign may be for another client or the sender's callsign itself.

Request Payload Fields:

| Request Field | Description                        |
|---------------|------------------------------------|
| Callsign      | Callsign of the client in question |

Response Payload Fields:

| Response Field | Description                                                                                                             |
|----------------|-------------------------------------------------------------------------------------------------------------------------|
| Boolean Flag   | Whether the client in question is an active ATC.<br>Formatted as `Y` or `N`, indicating `true` or `false` respectively. |
| Callsign       | Callsign of the client in question                                                                                      |

Request Example:
```text
$CQSAN_GND:SERVER:ATC:SAN_GND
```

_"This is SAN_GND. Does SAN_GND have active ATC privileges?"_

Response Example:
```text
$CRSERVER:SAN_GND:ATC:Y:SAN_GND
```

_"Yes, SAN_GND has active ATC privileges."_

<br>

### `CAPS` (Capabilities)
- Query the capabilities of another client or the server.

Request Payload Fields:

N/A

Response Payload Fields:

| Response Field                         | Description                   |
|----------------------------------------|-------------------------------|
| Capabilities List (several FSD fields) | List of [Capabilities](#TODO) |

Request Example:
```text
$CQJBU325:JBU1005:CAPS
```

_"This is JBU325. I would like to know JBU1005's client capabilities."_

Response Example:
```text
$CRJBU1005:JBU325:CAPS:VERSION=1:ATCINFO=1:MODELDESC=1:ACCONFIG=1:VISUPDATE=1:ATCMULTI=1
```

_"This is JBU1005. I would like to share my client's capabilities with JBU325."_

<br>

### `C?` (COM 1 Frequency)
- Query another client for their COM 1 frequency.
- Only for pilot clients, as ATC clients preemptively broadcast their COM 1 frequency in [position reports](#atc-position).

Request Payload Fields:

N/A

Response Payload Fields:

| Response Field  | Description                                                                                                     |
|-----------------|-----------------------------------------------------------------------------------------------------------------|
| COM 1 Frequency | Floating-point number with 3 digits of decimal precision representing an aviation VHF frequency e.g., `122.800` |

Request Example:
```text
$CQJBU617:JBU325:C?
```

_"This is JBU617. I would like to know JBU325's COM 1 frequency."_

Response Example:
```text
$CRJBU325:JBU617:C?:122.800
```

_"This is JBU325. I would like to tell JBU617 that my COM 1 radio is tuned to 122.800."_

<br>

### `RN` (Real Name)
- Query another client for their real name.

Request Payload Fields:

N/A

Response Payload Fields:

| Response Field | Description                                                                                                       |
|----------------|-------------------------------------------------------------------------------------------------------------------|
| Real Name      | Client's real name conventionally formatted as `<FirstName> <LastName> <HomeAirport>`                             |
| Sector File    | Sector file in use (blank if pilot)                                                                               | 
| Pilot Rating   | [Pilot Rating](/enumerations#pilot-ratings) if pilot, ATC/[Network Rating](/enumerations#network-ratings) if ATC. |

Request Example:
```text
$CQJBU617:JBU325:RN
```

_"This is JBU617. I would like to know JBU325's real name."_

Response Example:
```text
$CRJBU325:JBU617:RN:John Doe::1
```

_"This is JBU325. I would like to share my real name with JBU617."_

<br>

### `SV` (Server)

- Query a client for the address of the server they're connected to.
- It is plausible that this packet is only forwarded to its recipient when sent by a [supervisor](/enumerations#network-ratings) or above.
- Some clients do not respond to this packet.

Request Payload Fields:

N/A

Response Payload Fields:

| Response Field | Description                                                  |
|----------------|--------------------------------------------------------------|
| Server Address | Address of the server that the sender client is connected to |

Request Example:
```text
$CQABC_SUP:JBU325:SV
```

_"This is ABC_SUP, a supervisor, who would like to know the address of the server that JBU325 is connected to."_

Response Example:
```text
$CRJBU325:ABC_SUP:SV:127.0.0.1
```

_"This is JBU325. I am connected to server 127.0.0.1."_

<br>

### `ATIS` (ATIS)

- Query a client for their ATIS info.
- Answered by ATC clients only.
- Responses can be multiple FSD packets long. Each response has an ATIS type specifier. Once complete, a final response packet is sent indicating how many packets, including itself, were sent overall for the entire ATIS response.
- Some clients respond to these queries by instead sending [Text Message](#text-message-tm) packets with the ATIS messages in human-readable format.

Request Payload Fields:

N/A

Request Example:
```text
$CQJBU325:KSAN_ATIS:ATIS
```

_"This is JBU325. I would like the ATIS from KSAN_ATIS."_

#### Letter Variant

- Indicates the current ATIS letter.

| Response Field | Description                                                    |
|----------------|----------------------------------------------------------------|
| ATIS Type      | `A`                                                            |
| Letter         | A single letter (A thru Z) specifying the current ATIS letter. |

ATIS Letter Response Example:
```text
$CRKSAN_ATIS:JBU325:ATIS:A:D
```

_"This is KSAN_ATIS. Information Delta is current."_

#### Text Variant

- A _single line or section_ of the controller's ATIS info message.
- Several of these packets may be sent in order.

| Response Field | Description            |
|----------------|------------------------|
| ATIS Type      | `T`                    |
| Text           | ATIS info text section |

ATIS Letter Response Example:
```text
$CRKSAN_ATIS:JBU325:ATIS:T:(THREE ZERO ZERO NINER). LOC, RNAV, AND VIS APPS IN USE. LDG AND
```

#### Logoff Time Variant

- The planned logoff time of the controller.

| Response Field | Description                                                      |
|----------------|------------------------------------------------------------------|
| ATIS Type      | `Z`                                                              |
| Logoff Time    | Logoff time of the controller, formatted as `HHMMz` in zulu time |

ATIS Logoff Time Response Example:
```text
$CRKSAN_ATIS:JBU325:ATIS:Z:0900z
```

_"This is KSAN_ATIS. I plan to log off the network at 0900 zulu time."_

#### Voice Server Variant

- The controller's voice server.

| Response Field | Description          |
|----------------|----------------------|
| ATIS Type      | `V`                  |
| Voice Server   | Voice Server Address |

ATIS Logoff Time Response Example:
```text
$CRKSAN_ATIS:JBU325:ATIS:V:123.123.123.123
```

_"This is KSAN_ATIS. My voice server address can be reached at 123.123.123.123."_

#### End Marker Variant

- Indicates that the sender is done sending ATIS messages

| Response Field         | Description                                                                                  |
|------------------------|----------------------------------------------------------------------------------------------|
| ATIS Type              | `E`                                                                                          |
| Number of packets sent | Total number of packets sent for the entire ATIS response. Total count includes this packet. |

ATIS End Marker Response Example:
```text
$CRKSAN_ATIS:JBU325:ATIS:E:5
```

_"This is KSAN_ATIS. Including this final packet, I have sent 5 ATIS messages in response to your request."_

#### Text Message Alternative

Some clients alternatively format the above ATIS response variants as [Text Message](#text-message-tm) packets:

Examples:

```
$CRKSAN_ATIS:JBU325:ATIS:A:D

becomes...

#TMKSAN_ATIS:JBU325:ATIS A D
```

```
$CRKSAN_ATIS:JBU325:ATIS:T:(THREE ZERO ZERO NINER). LOC, RNAV, AND VIS APPS IN USE

becomes...

#TMKSAN_ATIS:JBU325:ATIS T (THREE ZERO ZERO NINER). LOC, RNAV, AND VIS APPS IN USE
```

<br>

### `IP` (Public IP)

- Ask the server for your public IP address.
- All Public IP requests must be sent to `SERVER`. All Public IP responses must be from `SERVER`.

Request Payload Fields:

N/A

Response Payload Fields:

| Response Field | Description                                    |
|----------------|------------------------------------------------|
| IP Address     | Client's IP address as observed by the server. |

Request Example:
```text
$CQJBU325:SERVER:IP
```

_"Hello server, this is JBU325. I would like to know my IP address as observed by your end of the connection."_

Response Example:
```text
$CRSERVER:JBU325:IP:127.0.0.1
```

_"This is the server. I observe your IP address to be 127.0.0.1."_

<br>

### `INF` (Information)

- **Supervisor/admin/server only** request for a client-generated key/value list.
- Includes information like the client's name, public IP address, system UID, geographical coordinates, etc.

Request Payload Fields:

N/A

Response Payload Fields:

| Response Field | Description                                     |
|----------------|-------------------------------------------------|
| INF String     | String of key/value pairs describing the client |

Request Example:
```text
$CQSERVER:JBU325:INF
```

_"Hello JBU325, this is the server. Send me your INF string."_

Response Example:
```text
$CRJBU325:SERVER:INF:vPilot 3.8.1 PID=100000 (John Doe) IP=127.0.0.1 SYS_UID=99999 FSVER=Msfs LT=41.34555 LO=-84.79253 AL=13
```

_"Hello server, this is JBU325. Here is my INF string."_

<br>

### `FP` (Flight Plan)

- ATC only
- Request a flight plan from the server.
- The recipient must be `SERVER`. 

Request Payload Fields:

| Request Field | Description                           |
|---------------|---------------------------------------|
| Callsign      | Callsign of the requested flight plan |

Response Payload Fields:

N/A

- Replies are formatted as [Flight Plan](#flight-plan-fp) packets.

Request Example:
```text
$CQSAN_GND:SERVER:FP:JBU325
```

_"Hello server, this is SAN_GND. Give me the flight plan for JBU325 if one is filed."_

Response Example:

- See [Flight Plan](#flight-plan-fp).

<br>

### `IPC` (Force Squawk Code Change)

- ATC command to force a client to change their squawk code.
- Some clients ignore this packet.
- This packet may be deprecated.

Request Payload Fields:

| Request Field  | Description                                          |
|----------------|------------------------------------------------------|
| Callsign       | Callsign of estimate.                                |
| Static Field 2 | This field is always `852`. It's meaning is unknown. |
| Squawk Code    | Squawk code to change to                             |

Request Example:
```text
$CQTOR_CTR:ACA365:IPC:W:852:4378
```

_"Hello ACA365, this is TOR_CTR. I'm forcing you to change your squawk code to 4378."_

<br>

### `BY` (Request Relief)

- ATC only
- Broadcasted from an air traffic controller to all other in-range air traffic controllers signalling that they are requesting to be relieved by another air traffic controller.
- Sent to special recipient [@94835](#94835).
- There are no payload fields for this client query type.

Request Example:
```text
$CQTOR_CTR:@94835:BY
```

_"To all nearby ATCs, this is TOR_CTR. I'm logging off soon, and I'm requesting to be relieved of my position."_

Response Example:

N/A

- There are no associated responses for this client query type.

<br>

### `HI` (Cancel Request Relief)

- ATC only.
- Cancels a [Relief Request](#by-request-relief).
- Sent to special recipient [@94835](#94835).

Request Example:
```text
$CQTOR_CTR:@94835:HI
```

_"To all nearby ATCs, this is TOR_CTR. Never mind, I won't be logging off for a while."_

Response Example:

N/A

- There are no associated responses for this client query type.

<br>

### `HLP` (Request Help)

- ATC only.
- Request help from other controllers.
- Sent to special recipient [@94835](#94835).
- This packet is ignored by the [vatSys](https://virtualairtrafficsystem.com) client.

Request Payload Fields:

| Request Field      | Description                                                                                    |
|--------------------|------------------------------------------------------------------------------------------------|
| Message (optional) | Message to send along with the help request. If omitted, the entire FSD field must be omitted. |

Request Example:
```text
$CQTOR_CTR:@94835:HLP:Heeeelp meeee!!!
```

_"To all nearby ATCs, this is TOR_CTR. I'm going down the tubes!"_

<br>

### `NOHLP` (Cancel Request Help)

- ATC only.
- Cancels a [Help Request](#hlp-request-help).
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field      | Description                                                                                                 |
|--------------------|-------------------------------------------------------------------------------------------------------------|
| Message (optional) | Message to send along with the help request cancellation. If omitted, the entire FSD field must be omitted. |

Request Example:
```text
$CQTOR_CTR:@94835:NOHLP:Never mind, I'm good.
```

_"To all nearby ATCs, this is TOR_CTR. I'm good actually."_

<br>

### `WH` (Who Has)

- ATC only
- Broadcast a query to in-range air traffic controllers asking who is tracking a given target.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field   | Description                        |
|-----------------|------------------------------------|
| Target Callsign | Callsign of the target in question |

Response Payload Fields:

N/A

Request Example:
```text
$CQSCT_S_APP:@94835:WH:N7938C
```

_"To all nearby ATCs, this is SCT_S_APP. Who is currently tracking N7938C?"_

Response Example:

N/A

- A [Pro Controller](#pro-controller-pc) packet is used to reply to this query.
- TODO: list pro controller packet types

<br>

### `IT` (Initiate Track)

- ATC-only
- Sent as a broadcast message to all in-range ATC clients signalling that they are taking control over a target's track.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field   | Description                        |
|-----------------|------------------------------------|
| Target Callsign | Callsign of the target in question |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:IT:ROU1887
```

_"To all nearby ATCs, this is SCT_S_APP. I have acquired the track for ROU1887."_

<br>

### `DR` (Drop Track)

- ATC-only
- Sent as a broadcast message to all in-range ATC clients signalling that they are dropping a target's track (not handing off the target to another controller.)
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field   | Description                        |
|-----------------|------------------------------------|
| Target Callsign | Callsign of the target in question |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:DR:ROU1887
```

_"To all nearby ATCs, this is SCT_S_APP. I have dropped my track for ROU1887."_

<br>

### `HT` (Accept Handoff)

- ATC-only
- Accept an ATC [Handoff Request](#handoff-request-ho).
- This packet seems to be sent in sequence after two ATC clients directly negotiate a handoff via [Initiate Handoff](#handoff-request-ho) & [Accept Handoff](#handoff-accept-ha) packets. This broadcasts the change of track to all other in-range ATC clients that would otherwise only be known to the two interacting parties. This helps to maintain global state.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field   | Description                        |
|-----------------|------------------------------------|
| Target Callsign | Callsign of the target in question |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:HT:ROU1887
```

_"To all nearby ATCs, this is SCT_S_APP. I have acquired ROU1887's track from another controller."_

<br>

### `TA` (Set Temporary Altitude)

- ATC-only
- Set the assigned temporary altitude for a target.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field      | Description                        |
|--------------------|------------------------------------|
| Target Callsign    | Callsign of the target in question |
| Temporary Altitude | Temporary altitude value           |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:TA:ROU1887:23000
```

_"To all nearby ATCs, this is SCT_S_APP. I assigned temporary altitude 23000ft to ROU1887."_

<br>

### `FA` (Set Final Altitude)

- ATC-only
- Set the final altitude on a target's scratchpad.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field   | Description                        |
|-----------------|------------------------------------|
| Target Callsign | Callsign of the target in question |
| Final Altitude  | Final altitude value               |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:FA:ROU1887:35000
```

_"To all nearby ATCs, this is SCT_S_APP. I assigned final altitude 23000ft to ROU1887."_

<br>

### `BC` (Set Beacon Code)

- ATC-only
- Set the assigned [SSR](https://en.wikipedia.org/wiki/Secondary_surveillance_radar) beacon code for a target.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field        | Description                        |
|----------------------|------------------------------------|
| Target Callsign      | Callsign of the target in question |
| Assigned Beacon Code | Beacon code value                  |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:BC:ROU1887:7032
```

_"To all nearby ATCs, this is SCT_S_APP. I assigned beacon code 7032 to ROU1887."_

<br>

### `SC` (Set Scratchpad)

- ATC-only
- Set the scratchpad operations data (abbreviated scratchpad text) for a target.
- Sent to special recipient [@94835](#94835).

Request Payload Fields:

| Request Field   | Description                        |
|-----------------|------------------------------------|
| Target Callsign | Callsign of the target in question |
| Data            | Scratchpad data                    |

Response Payload Fields:

N/A

- There is no associated response for this client query type.

Request Example:
```text
$CQSCT_S_APP:@94835:SC:N7938C:SFR
```

_"To all nearby ATCs, this is SCT_S_APP. I assigned scratchpad code 'SFR' to N7938C."_

<br>

### `VT` (Set Voice Type)

- ATC-only
- Set the voice type for a target.
- Sent to special recipient [@94835](#94835).
- The vatSys client ignores this packet.

| Request Field   | Description                                               |
|-----------------|-----------------------------------------------------------|
| Target Callsign | Callsign of the target in question                        |
| Voice Type Code | Send & Receive = `v`, Receive-only = `r`, Text-only = `t` |

Example Request:
```text
$CQSCT_S_APP:@94835:VT:ROU1887:v
```

_"To all nearby ATCs, this is SCT_S_APP. I assigned ROU1887's the voice type to 'Send and Receive'."_

<br>

### `ACC` (Aircraft Configuration)

- Aircraft configuration state (lights, engines, flaps, etc.)

Request Payload Fields:

| Request Field | Description                                     |
|---------------|-------------------------------------------------|
| JSON Payload  | Aircraft configuration JSON payload (see below) |

- All client query packets of this type utilize the `$CQ` variant.
- Aircraft Configuration data is formatted as JSON.
- Updates to aircraft configuration state may be broadcast unsolicited to other clients.
- Unsolicited aircraft configuration updates are sent to the special recipient [@94836](#94836).
- Any `"config": {}` field may be omitted if `"is_full_data"` is set to `false`. This is common for unsolicited broadcast packets, in the case where say only one variable changes e.g. the pilot turns their landing lights on.

A client may request the configuration of another client:

```json
{
    "request": "full"
}
```

The actual aircraft configuration state is nested in the `"config"` object:

```json
{
  "config": {
    "is_full_data": boolean,

    "lights": {
      "strobe_on":  boolean,
      "landing_on": boolean,
      "taxi_on":    boolean,
      "beacon_on":  boolean,
      "nav_on":     boolean,
      "logo_on":    boolean
    },

    "engines": {
      "1": {
        "on":           boolean,
        "is_reversing": boolean
      },
      "2": {
        "on":           boolean,
        "is_reversing": boolean
      },
      "3": {
        "on":           boolean,
        "is_reversing": boolean
      },
      "4": {
        "on":           boolean,
        "is_reversing": boolean
      }
    },

    "gear_down":        boolean,
    "flaps_pct":        integer,
    "spoilers_out":     boolean,
    "on_ground":        boolean,
    "static_cg_height": float64
  }
}
```

Request Example:
```text
$CQPRM4211:JBU325:ACC:{"request":"full"}
```

_"This is PRM4211. Give me JBU325's full aircraft configuration state."_

Associated Response:
```text
$CQJBU325:PRM4211:ACC:{"config":{"is_full_data":true,"lights":{"strobe_on":false,"landing_on":false,"taxi_on":false,"beacon_on":false,"nav_on":false,"logo_on":false},"engines":{"1":{"on":false,"is_reversing":false},"2":{"on":false,"is_reversing":false}},"gear_down":true,"flaps_pct":0,"spoilers_out":false,"on_ground":true,"static_cg_height":10.4399995803833}}
```

_"Hello PRM4211, this is JBU325. Here is my full aircraft configuration state."_

Unsolicited Broadcast Example:
```text
$CQSKW3272:@94836:ACC:{"config":{"flaps_pct":10}}
```

_"To all nearby pilots, this is SKW3272. My flaps are now 10 percent deployed."_

<br>

### `NEWINFO` (New Information)

- ATC-only
- Broadcast when a new ATIS/information is current.
- Assumed to be sent to special recipient [@94835](#94835).
- The vatSys client ignores this packet.
- This packet may be deprecated.

| Request Field   | Description                                                |
|-----------------|------------------------------------------------------------|
| New ATIS Letter | New letter for current ATIS information                    |

Example Request:
```text
$CQKSAN_ATIS:@94835:NEWINFO:B
```

_"To all nearby ATCs, this is KSAN_ATIS. Information Bravo is now current."_

<br>

### `NEWATIS` (New ATIS)

- ATC-only
- Broadcast when a new ATIS is current.
- Assumed to be sent to special recipient [@94835](#94835).
- The vatSys client ignores this packet.
- This packet may be deprecated.

| Request Field             | Description                                                                                          |
|---------------------------|------------------------------------------------------------------------------------------------------|
| New ATIS Letter String    | New letter for current ATIS information, formatted as `ATIS {letter}` e.g. `ATIS B`                  |
| Surface Wind and Pressure | Surface wind and pressure string, formatted as `DEG at VELOCITY - PRESSURE` e.g. `220 at 12 - 29.92` |

Example Request:
```text
$CQKSAN_ATIS:@94835:NEWATIS:ATIS B:220 at 12 - 29.92
```

_"To all nearby ATCs, this is KSAN_ATIS. Information Bravo is now current. Wind 220 at 12, altimeter 29.92."_

<br>

### `EST` (Estimate)

- ATC-only
- Broadcast an estimate for a target.
- Sent to special recipient [@94835](#94835).

| Request Field  | Description                                                                                                                                                                                                                                                                   |
|----------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Callsign       | Callsign in question for estimate                                                                                                                                                                                                                                             |
| Segment        | Segment/intersection name                                                                                                                                                                                                                                                     |
| Filtered Index | In the case a given segment identifier/intersection appears more than once, this value indicates the index of such. e.g. If the intersection XYZ appears twice in the target's flight plan, and the estimate is referring to the second one, this value should be set to `1`. |
| Estimate Type  | ETO = `0`, PETO = `1`, ATO = `2`, FINISH = `3`                                                                                                                                                                                                                                |
| Time           | Datetime formatted as `yyyy-MM-ddTHH:mm:ss` (UTC)                                                                                                                                                                                                                             |

Example Request:
```text
$CQSCT_S_APP:@94835:EST:ROU1887:ZZOOO:0:2025-01-08T14:12:42
```

_"To all nearby ATCs, this is SCT_S_APP. I estimate ROU1887 to be over ZZOOO intersection at 2025-01-08T14:12:42 zulu time."_

<br>

### `GD` (Set Global Data)

- ATC-only
- Set the global data for a target.
- Sent to special recipient [@94835](#94835).

| Request Field   | Description          |
|-----------------|----------------------|
| Target Callsign | Target callsign      |
| Contents        | Global data contents |

Example Request:
```text
$CQSCT_S_APP:@94835:GD:ROU1887:abcdefg
```

_"To all nearby ATCs, this is SCT_S_APP. I assigned ROU1887's global data to 'abcdefg'"_

<br>

## METAR Request (`$AX`)

- Request a METAR from the server.
- The recipient must always be SERVER.

| Field Name   | Type   | Description                               | Notes             |
|--------------|--------|-------------------------------------------|-------------------|
| From         | string | Source callsign                           |                   |
| To           | string | Recipient callsign                        |                   |
| Static Field | string | The value of this field is always `METAR` |                   |
| Station      | string | The station code to query                 | ICAO airport code | 

Example:
```text
$AXSAN_GND:SERVER:METAR:KSAN
```

_"Hello server, this is SAN_GND. Give me the latest METAR report for Lindbergh Field KSAN."_

<br>

## METAR Response (`$AR`)

- Respond to a [METAR Request](#metar-request-ax).
- Always sent from the server to the requester client.

| Field Name | Type   | Description               | Notes             |
|------------|--------|---------------------------|-------------------|
| From       | string | Source callsign           |                   |
| To         | string | Recipient callsign        |                   |
| METAR      | string | METAR value               |                   |

Example:
```text
$ARSERVER:SAN_GND:KSAN 092351Z 30003KT 10SM CLR 18/05 A3008 RMK AO2 SLP185 T01830050 10211 20172 55001 $
```

_"Hello SAN_GND, this is the server. Here is the latest METAR report I have for Lindbergh Field KSAN."_

<br>

## Weather Profile Request (`#WX`)

- Request a weather profile from the server.
- The server responds to this request with [Wind Data](#wind-data-wd), [Cloud Data](#cloud-data-cd), and [Temperature Data](#temperature-data-td) packets.
- No modern client is known to use this packet. It is possible this is how legacy clients obtained real-time weather information.
- The vatSys client ignores this packet.

| Field Name | Type   | Description                              | Notes             |
|------------|--------|------------------------------------------|-------------------|
| From       | string | Source callsign                          |                   |
| To         | string | Recipient callsign                       |                   |
| Station    | string | Station to query weather information for | ICAO airport code |

Example:
```text
#WXAAL365:SERVER:KDFW
```

<br>

## Wind Data (`#WD`)

- Send data on wind layers.
- Only sent by `SERVER`.
- Response for a [Weather Profile Request](#weather-profile-request-wx).
- No known client has been observed to implement use of this packet.
- This packet may be deprecated.
- Boolean values are serialized as `1` or `0`, indicating 'true' or 'false' respectively.

| Field Name         | Type    | Description                         |
|--------------------|---------|-------------------------------------|
| From               | string  | Source callsign                     |                   
| To                 | string  | Recipient callsign                  |                   
| Layer 1 Ceiling    | integer | Ceiling of wind layer 1 in feet     |
| Layer 1 Floor      | integer | Floor of wind layer 1 in feet       |
| Layer 1 Direction  | integer | Direction of wind layer 1 (degrees) |
| Layer 1 Speed      | integer | Speed of wind layer 1 (knots)       |
| Layer 1 Gusting    | boolean | Indicates gusting in layer 1        |
| Layer 1 Turbulence | integer | Turbulence level in layer 1         |
| Layer 2 Ceiling    | integer | Ceiling of wind layer 2 in feet     |
| Layer 2 Floor      | integer | Floor of wind layer 2 in feet       |
| Layer 2 Direction  | integer | Direction of wind layer 2 (degrees) |
| Layer 2 Speed      | integer | Speed of wind layer 2 (knots)       |
| Layer 2 Gusting    | boolean | Indicates gusting in layer 2        |
| Layer 2 Turbulence | integer | Turbulence level in layer 2         |
| Layer 3 Ceiling    | integer | Ceiling of wind layer 3 in feet     |
| Layer 3 Floor      | integer | Floor of wind layer 3 in feet       |
| Layer 3 Direction  | integer | Direction of wind layer 3 (degrees) |
| Layer 3 Speed      | integer | Speed of wind layer 3 (knots)       |
| Layer 3 Gusting    | boolean | Indicates gusting in layer 3        |
| Layer 3 Turbulence | integer | Turbulence level in layer 3         |
| Layer 4 Ceiling    | integer | Ceiling of wind layer 4 in feet     |
| Layer 4 Floor      | integer | Floor of wind layer 4 in feet       |
| Layer 4 Direction  | integer | Direction of wind layer 4 (degrees) |
| Layer 4 Speed      | integer | Speed of wind layer 4 (knots)       |
| Layer 4 Gusting    | boolean | Indicates gusting in layer 4        |
| Layer 4 Turbulence | integer | Turbulence level in layer 4         |

Example:
```text
#WDSERVER:DFW_GND:-1:-1:0:0:0:0:10400:2500:19:0:0:0:22600:10400:359:0:0:0:90000:22700:15:0:0:0
```

<br>

## Cloud Data (`#CD`)

- Send data on cloud layers.
- Only sent by `SERVER`.
- Response for a [Weather Profile Request](#weather-profile-request-wx).
- No known client has been observed to implement use of this packet.
- This packet may be deprecated.
- Boolean values are serialized as `1` or `0`, indicating 'true' or 'false' respectively.

| Field Name             | Type    | Description                                |
|------------------------|---------|--------------------------------------------|
| From                   | string  | Source callsign                            |                   
| To                     | string  | Recipient callsign                         |  
| Layer 1 Ceiling        | integer | Ceiling of cloud layer 1 in feet           |
| Layer 1 Floor          | integer | Floor of cloud layer 1 in feet             |
| Layer 1 Coverage       | integer | Cloud coverage of layer 1                  |
| Layer 1 Icing          | boolean | Indicates the presence of icing in layer 1 |
| Layer 1 Turbulence     | integer | Turbulence level in cloud layer 1          |
| Layer 2 Ceiling        | integer | Ceiling of cloud layer 2 in feet           |
| Layer 2 Floor          | integer | Floor of cloud layer 2 in feet             |
| Layer 2 Coverage       | integer | Cloud coverage of layer 2                  |
| Layer 2 Icing          | boolean | Indicates the presence of icing in layer 2 |
| Layer 2 Turbulence     | integer | Turbulence level in cloud layer 2          |
| Storm Layer Ceiling    | integer | Ceiling of storm layer in feet             |
| Storm Layer Floor      | integer | Floor of storm layer in feet               |
| Storm Layer Deviation  | integer | Direction deviation in the storm layer     |
| Storm Layer Coverage   | integer | Storm coverage                             |
| Storm Layer Turbulence | integer | Turbulence level in the storm layer        |

Example:
```text
#CDSERVER:DFW_GND:-1:-1:0:0:0:-1:-1:0:0:0:-1:-1:0:0:0:10.00
```

<br>

## Temperature Data (`#TD`)

- Send data on temperature layers and atmospheric pressure.
- Only sent by `SERVER`.
- Response for a [Weather Profile Request](#weather-profile-request-wx).
- No known client has been observed to implement use of this packet.
- This packet may be deprecated.
- Boolean values are serialized as `1` or `0`, indicating 'true' or 'false' respectively.

| Field Name          | Type    | Description                                 |
|---------------------|---------|---------------------------------------------|
| From                | string  | Source callsign                             |
| To                  | string  | Recipient callsign                          |
| Layer 1 Ceiling     | integer | Ceiling of temperature layer 1 in feet      |
| Layer 1 Temperature | integer | Temperature of layer 1 in Celsius           |
| Layer 2 Ceiling     | integer | Ceiling of temperature layer 2 in feet      |
| Layer 2 Temperature | integer | Temperature of layer 2 in Celsius           |
| Layer 3 Ceiling     | integer | Ceiling of temperature layer 3 in feet      |
| Layer 3 Temperature | integer | Temperature of layer 3 in Celsius           |
| Layer 4 Ceiling     | integer | Ceiling of temperature layer 4 in feet      |
| Layer 4 Temperature | integer | Temperature of layer 4 in Celsius           |
| Pressure            | integer | Atmospheric pressure in (inHg) e.g., `2992` |

Example:
```text
#TDSERVER:DFW_GND:100:0:10000:-9:18000:-28:35000:-61:2992
```

<br>

## Handoff Request (`$HO`)

- ATC only
- Initiate a handoff of a target
- TODO: determine recipients

| Field Name | Type   | Description                       | Notes             |
|------------|--------|-----------------------------------|-------------------|
| From       | string | Source callsign                   |                   |
| To         | string | Recipient callsign                |                   |
| Target     | string | Callsign of the target to handoff |                   |

Example:
```text
$HOSAN_APP:LAX_35_CTR:ROU1887
```

_"Hello LAX_35_CTR, this is SAN_APP. Handing off ROU1887 to you."_

<br>

## Handoff Accept (`$HA`)

- ATC only
- Accept a [Handoff Request](#handoff-request-ho)
- TODO: determine recipients

| Field Name | Type   | Description                       | Notes             |
|------------|--------|-----------------------------------|-------------------|
| From       | string | Source callsign                   |                   |
| To         | string | Recipient callsign                |                   |
| Target     | string | Callsign of the target to handoff |                   |

Example:
```text
$HOLAX_35_CTR:SAN_APP:ROU1887
```

_"Hello SAN_APP, this is LAX_35_CTR. I accept your handoff request. I will now track ROU1887."_

<br>

## Flight Plan (`$FP`)

- Send a flight plan
- Clients may file a flight plan by setting the recipient to SERVER.
- Used to respond to [FP Client Query](#fp-flight-plan) requests.
- Is broadcasted to all ATC when someone files a flight plan. In this case, the recipient is set to `*A`.
- Sometimes, ATC clients send a text message to the recipient `FP` acknowledging when they receive a flight plan.

| Field Name                | Type    | Description                                                                  | Notes         |
|---------------------------|---------|------------------------------------------------------------------------------|---------------|
| From                      | string  | Source callsign                                                              |               |
| To                        | string  | Recipient callsign                                                           |               |
| Flight Rules              | char    | `I` = IFR ,`V` = VFR, `D` = DVFR, `S` = SVFR                                 |               |
| Equipment Code            | string  | Equipment code or suffix                                                     |               |
| True Airspeed             | integer | Filed true airspeed                                                          | Unit is knots |
| Departure Airport         | string  | ICAO departure airport identifier                                            |               |
| Estimated Departure Time  | integer | Filed ETD                                                                    | Zulu time     |
| Actual Departure Time     | integer | Filed actual departure time                                                  | Zulu time     |
| Cruise Altitude           | integer | Filed cruise altitude                                                        | Unit is feet  |
| Destination Airport       | string  | ICAO arrival airport identifier                                              |               |
| Hours Enroute             | integer | Filed hours enroute                                                          |               |
| Minutes Enroute           | integer | Filed minutes enroute, in addition to Hours Enroute                          |               |
| Hours of Fuel Available   | integer | Number of hours of fuel available                                            |               |
| Minutes of Fuel Available | integer | Number of minutes of fuel available, in addition to Hours of Fuel Available. |
| Alternate Airport         | string  | ICAO alternate airport identifier                                            |               |
| Remarks                   | string  | Flightplan remarks section                                                   |               |
| Route                     | string  | Flight planned route                                                         |               |

Example:
```text
$FPAAL152:SAN_TWR:I:H/B772/L:487:KLAX:250:250:35000:KDFW:2:40:4:5:KOKC:PBN/A1B1D1S2T1 DOF/250111 REG/N755SB EET/KZAB0032 KZFW0138 OPR/AAL PER/D RMK/TCAS SIMBRIEF /V/:DOTSS2 CNERY BLH J169 TFD J50 SSO J4 INK GEEKY BOOVE7
```

<br>

## Flight Plan Amendment (`$AM`)

- Used to amend a previously filed flight plan.
- ATC clients may amend a flight plan by sending the amendment with the recipient set to `SERVER`.
- Broadcasted to all ATC in-range when an amendment is filed.
- Sent to the special recipient [@94835](#94835).

| Field Name                | Type   | Description                                                                  | Notes         |
|---------------------------|--------|------------------------------------------------------------------------------|---------------|
| From                      | string | Source callsign                                                              |               |
| To                        | string | Recipient callsign                                                           |               |
| Callsign                  | string | The flight callsign for the aircraft                                         |               |
| Flight Rules              | char   | `I` = IFR, `V` = VFR, `D` = DVFR, `S` = SVFR                                 |               |
| Equipment Code            | string | Equipment code or suffix                                                     |               |
| True Airspeed             | string | Filed true airspeed                                                          | Unit is knots |
| Departure Airport         | string | ICAO departure airport identifier                                            |               |
| Estimated Departure Time  | string | Filed estimated departure time                                               | Zulu time     |
| Actual Departure Time     | string | Filed actual departure time                                                  | Zulu time     |
| Cruise Altitude           | string | Filed cruise altitude                                                        | Unit is feet  |
| Destination Airport       | string | ICAO arrival airport identifier                                              |               |
| Hours Enroute             | string | Filed hours enroute                                                          |               |
| Minutes Enroute           | string | Filed minutes enroute, in addition to Hours Enroute                          |               |
| Hours of Fuel Available   | string | Number of hours of fuel available                                            |               |
| Minutes of Fuel Available | string | Number of minutes of fuel available, in addition to Hours of Fuel Available. |               |
| Alternate Airport         | string | ICAO alternate airport identifier                                            |               |
| Remarks                   | string | Flightplan remarks section                                                   |               |
| Route                     | string | Flight planned route                                                         |               |

### Example:
```text
$AMAAL123:SAN_TWR:AAL123:I:H/B737/L:420:KLAX:230:240:35000:KORD:3:15:3:30:KDEN:2:50:4:10:KMEM:RMK/TCAS SIMBRIEF /V/:DOTSS2 CNERY J169 TFD J50 SSO INK GEEKY BOOVE7
```

<br>

## ATC Shared State (`#PC`)

| Field Name     | Type   | Description                                                                                    | Notes                                                                                                                                                                                                                   |
|----------------|--------|------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| From           | string | Source callsign                                                                                |                                                                                                                                                                                                                         |
| To             | string | Recipient callsign                                                                             |                                                                                                                                                                                                                         |
| Static Field   | string | The value of this field is always `CCP`.                                                       | This may stand for 'Controller to Controller Packet'. This field is present on all [Shared State](#atc-shared-state-pc) packets. Other variants of this packet without `CCP`  are undocumented. They may be deprecated. |
| Type           | string | Shared state type identifier (see below)                                                       |                                                                                                                                                                                                                         |
| Payload Fields | varies | Potentially many separate contiguous FSD fields, depending on the [type](#shared-state-types). |                                                                                                                                                                                                                         |

- Multipurpose ATC data exchange packet
- `#PC` stands for 'Pro Controller', the first ATC client with widespread use on the VATSIM network.
- Facilitates ATC clients synchronizing with one another in order to maintain a shared global state.
- Some variants of this packet have similar logic to and are interchangeable with some [Client Query](#client-query-cq) variants.
- ATC Shared packets have many [types](#shared-state-types)

## Shared State Types

### `VER` (Version)

- Description TODO

Example:
```text
#PCSCT_APP:SAN_APP:CCP:VER
```

_"Example TODO"_

Example:
```text
TODO
```

<br>

### `ID` (Modern Client Check)

- Description TODO

Example:
```text
#PCSCT_APP:SAN_APP:CCP:ID
```

_"Example TODO"_

Example:
```text
TODO
```

<br>

### `DI` (Reverse Modern Client Check?)

- Description TODO

Example:
```text
#PCSCT_APP:SAN_APP:CCP:DI
```

_"Example TODO"_

Example:
```text
TODO
```

<br>

### `IH` (I Have)

- Broadcast that a client has the track of a target.
- TODO: Confirm that this packet is actually broadcasted.
- Used to reply to [Client Query Who Has](#wh-who-has) packets.

Request Fields:

| Field    | Description     |
|----------|-----------------|
| Callsign | Target callsign |

Example:
```text
#PCSCT_APP:@94835:CCP:IH:ROU1887
```

_"To all nearby controllers, this is SCT_APP. I am tracking the target ROU1887."_

<br>

### `SC` (Scratchpad)

- Broadcast a change to a target's scratchpad.
- Used to convey specific information about a target (e.g., headings, speeds, states).
- Scratchpad fields have various types, described below.

Request Fields:

| Field    | Description                     |
|----------|---------------------------------|
| Callsign | Target callsign                 |
| Contents | Scratchpad contents (see below) |

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:ABC
```

_"To all nearby controllers, this is SCT_APP. I set the scratchpad data for ROU1887 to 'ABC'."_

<br>

### Scratchpad Types (Commonly used in Euroscope)

#### Plaintext or Direct to Fix

- **Description:** Represents plain text or direct information assigned to a target's scratchpad.
- **Syntax:** Any unrecognized text is treated as plain text or direct data.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:ABC123
```

_"To all nearby controllers, this is SCT_APP. I set the scratchpad data for ROU1887 to 'ABC123'."_

---

#### Rate of Climb

- **Description:** Indicates a target's rate of climb or descent in feet per minute.
- **Syntax:** Prefixed with `R`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:R1500
```

_"To all nearby controllers, this is SCT_APP. I set the rate of climb/descent for ROU1887 to 1500 feet per minute."_

<br>

#### Heading

- **Description:** Indicates a target's assigned heading in degrees.
- **Syntax:** Prefixed with `H`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:H270
```

_"To all nearby controllers, this is SCT_APP. I set the heading for ROU1887 to 270 degrees."_

<br>

#### Speed

- **Description:** Indicates a target's assigned speed in knots.
- **Syntax:** Prefixed with `S`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:S250
```

_"To all nearby controllers, this is SCT_APP. I set the speed for ROU1887 to 250 knots."_

<br>

#### Mach

- **Description:** Indicates a target's assigned Mach number.
- **Syntax:** No prefix; only numeric Mach values.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:0.84
```

_"To all nearby controllers, this is SCT_APP. I set the Mach number for ROU1887 to 0.84."_

<br>

#### Speed Operator

- **Description:** Represents a speed operator, such as "exactly," "or less," or "or greater."
- **Syntax:** Uses `/ASP=/`, `/ASP-/`, or `/ASP+/`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:/ASP=/
```

_"To all nearby controllers, this is SCT_APP. I set the speed operator for ROU1887 to 'exactly'."_

<br>

#### Rate of Climb Operator

- **Description:** Represents a climb/descent rate operator.
- **Syntax:** Uses `/ARC=/`, `/ARC-/`, or `/ARC+/`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:/ARC+/
```

_"To all nearby controllers, this is SCT_APP. I set the climb/descent rate operator for ROU1887 to 'or greater'."_

<br>

#### Stand

- **Description:** Indicates an assigned stand for a target.
- **Syntax:** Prefixed with `GRP/S/`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:GRP/S/GATE12
```

_"To all nearby controllers, this is SCT_APP. I set the stand for ROU1887 to 'GATE12'."_

---

#### Cancel Stand

- **Description:** Indicates the cancellation of a previously assigned stand.
- **Syntax:** `GRP/S/`

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:GRP/S/
```

_"To all nearby controllers, this is SCT_APP. I cancelled the stand assignment for ROU1887."_

<br>

#### Manual Stand

- **Description:** Indicates a manually assigned stand.
- **Syntax:** Prefixed with `GRP/M/`.

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:GRP/M/GATE12/BAY2
```

_"To all nearby controllers, this is SCT_APP. I manually set the stand for ROU1887 to 'GATE12/BAY2'."_

<br>

#### Cancel Manual Stand

- **Description:** Indicates the cancellation of a previously manually assigned stand.
- **Syntax:** `GRP/M/`

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:GRP/M/
```

_"To all nearby controllers, this is SCT_APP. I cancelled the manual stand assignment for ROU1887."_

<br>

#### Clearance Received

- **Description:** Indicates that a clearance has been given to a target.
- **Syntax:** `CLEA`

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:CLEA
```

_"To all nearby controllers, this is SCT_APP. I gave clearance to ROU1887."_

<br>

#### Clearance Cancelled

- **Description:** Indicates that a clearance has been cancelled for a target.
- **Syntax:** `NOTC`

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:NOTC
```

_"To all nearby controllers, this is SCT_APP. I cancelled clearance for ROU1887."_

<br>

#### Ground State

- **Description:** Represents the ground state of a target, such as startup, taxi, or takeoff.
- **Syntax:** Uses specific keywords (e.g., `STUP`, `TAXI`, `DEPA`).

Ground State Examples:

| Code     | Description            |
|----------|------------------------|
| `NSTS`   | No state               |
| `ONFREQ` | On frequency           |
| `DE-ICE` | De-icing               |
| `STUP`   | Startup                |
| `PUSH`   | Pushback               |
| `TAXI`   | Taxi                   |
| `LINEUP` | Line up                |
| `TXIN`   | Taxi in                |
| `DEPA`   | Departure              |
| `PARK`   | Park                   |

Example:
```text
#PCSCT_APP:@94835:CCP:SC:ROU1887:TAXI
```

_"To all nearby controllers, this is SCT_APP. I set the ground state for ROU1887 to 'Taxi'."_

<br>

### `GD` (Global Data)

- Broadcast a change to a target's global data.
- TODO: Confirm that this packet is actually broadcasted.

Request Fields:

| Field    | Description          |
|----------|----------------------|
| Callsign | Target callsign      |
| Contents | Global data contents |

Example:
```text
#PCSCT_APP:@94835:CCP:GD:ROU1887:XYZ
```

_"To all nearby controllers, this is SCT_APP. I set the global data for ROU1887 to 'XYZ'."_

<br>

### `TA` (Temporary Altitude)

- Broadcast a change to a target's assigned temporary altitude.
- TODO: Confirm that this packet is actually broadcasted.

Request Fields:

| Field    | Description                 |
|----------|-----------------------------|
| Callsign | Target callsign             |
| Altitude | Assigned temporary altitude |

Example:
```text
#PCSCT_APP:@94835:CCP:TA:ROU1887:23000
```

_"To all nearby controllers, this is SCT_APP. I assigned ROU1887's temporary altitude to 23000 feet."_

<br>

### `FA` (Final Altitude)

- Broadcast a change to a target's assigned final altitude.
- TODO: Confirm that this packet is actually broadcasted.

Request Fields:

| Field    | Description             |
|----------|-------------------------|
| Callsign | Target callsign         |
| Altitude | Assigned final altitude |

Example:
```text
#PCSCT_APP:@94835:CCP:FA:ROU1887:35000
```

_"To all nearby controllers, this is SCT_APP. I assigned ROU1887's final altitude to 35000 feet."_

<br>

### `VT` (Voice Type)

- Broadcast a change to a target's voice type.
- TODO: Confirm that this packet is actually broadcasted.

Request Fields:

| Field            | Description                                               |
|------------------|-----------------------------------------------------------|
| Callsign         | Target callsign                                           |
| Voice Capability | Send & Receive = `v`, Receive-only = `r`, Text-only = `t` |

Example:
```text
#PCSCT_APP:@94835:CCP:VT:ROU1887:v
```

_"To all nearby controllers, this is SCT_APP. I assigned ROU1887's voice type to 'Send and Receive Voice'."_

<br>

### `BC` (Beacon Code)

- Broadcast a change to a target's assigned beacon (transponder) code.
- TODO: Confirm that this packet is actually broadcasted.

Request Fields:

| Field       | Description                         |
|-------------|-------------------------------------|
| Callsign    | Target callsign                     |
| Beacon Code | Assigned beacon or transponder code |

Example:
```text
#PCSCT_APP:@94835:CCP:BC:ROU1887:6342
```

_"To all nearby controllers, this is SCT_APP. I assigned ROU1887's beacon code to 6342."_

<br>

### `HC` (Cancel Handoff)

- Cancel a previously initiated [Handoff Request](#handoff-request-ho)

Request Fields:

| Field       | Description                         |
|-------------|-------------------------------------|
| Callsign    | Target callsign                     |

Example:
```text
#PCSAN_APP:LAX_35_CTR:CCP:HC:ROU1887
```

_"LAX_35_CTR, this is SAN_APP. Disregard my handoff request for ROU1887."_

<br>

### `PT` (Pointout)

- Point out a target to another controller

Request Fields:

| Field       | Description                         |
|-------------|-------------------------------------|
| Callsign    | Target callsign                     |

Example:
```text
#PCSAN_APP:LAX_35_CTR:CCP:PT:ROU1887
```

_"LAX_35_CTR, this is SAN_APP. You will see a pointout for ROU1887 on your scope."_

<br>

### `DP` (Push to Departure List)

- Push a target to a departure list.
- TODO: determine recipient logic for this packet

Request Fields:

| Field       | Description                         |
|-------------|-------------------------------------|
| Callsign    | Target callsign                     |

Example:
```text
#PCSAN_TWR:SAN_APP:CCP:DP:ROU1887
```

TODO: verify
_"SAN_APP, this is SAN_TWR. Push ROU1887 to your departure list."_

<br>

### `ST` (Flight Strip)

- Broadcast a change to a target's flight strip
- TODO: determine recipient logic for this packet

Request Fields:

| Field                                                            | Description                    |
|------------------------------------------------------------------|--------------------------------|
| Callsign                                                         | Target callsign                |
| Format (optional)                                                | Flight strip format identifier |
| Contents (may consist of several separate contiguous FSD fields) | Flight strip contents          |

TODO: find flight strip format identifier and finish the example.
Example:
```text
#PCSAN_TWR:SAN_APP:CCP:ST:
```

TODO: verify
_"SAN_APP, this is SAN_TWR. TODO."_

<br>

### `IC`, `IK`, `IB`, `IO`, `OC`, `OK`, `OB`, `OO`, `MC`, `MK`, `MB`, `MO` (Landline)

- Landline coordination logic
- A landline command is identified by a 2-character code, as seen above. The first character determines the landline type, the second the landline command.

| Identifer | Landline Type |
|-----------|---------------|
| `I`       | Intercom      |
| `O`       | Override      |
| `M`       | Monitor       |

| Identifer | Landline Command |
|-----------|------------------|
| `C`       | Request          |
| `K`       | Approve          |
| `B`       | Reject           |
| `O`       | End              |

All landline packets with the 'Request' or 'Approve' commands contain two additional fields:

- IP Address
- Port

Example:
```text
#PCSAN_TWR:SAN_APP:CCP:IK:127.0.0.1:6789
```

_"SAN_APP, this is SAN_TWR. I accept your intercom landline call. Dial me at 127.0.0.1:6789."_

<br>

## SquawkBox Data (`#SB`)

- Exchange data between pilot clients

| Field Name          | Type   | Description                                                               | Notes |
|---------------------|--------|---------------------------------------------------------------------------|-------|
| From                | string | Source callsign                                                           |       |
| To                  | string | Recipient/victim callsign                                                 |       |
| SquawkBox Data Type | string | Identifier specifying the SquawkBox Data type                             |       |
| Payload             | varies | Potentially several separate contiguous FSD fields depending on the type. | 

### SquawkBox Data Types

#### Plane Info Request (`PIR`)

- Request model-matching information from another client

Example:
```text
#SBN7938C:DAL625:PIR
```

<br>

#### Plane Info (`PI:GEN`)

- Send model-matching information to another client

| Payload Field  | Type      | Description                                                                                                                              |
|----------------|-----------|------------------------------------------------------------------------------------------------------------------------------------------|
| Key/Value Pair | KEY=VALUE | Potentially several contiguous FSD fields containing key/value pairs describing model matching information. See below for possible keys. |

Possible Keys (non-exhaustive):

- `EQUIPMENT`: Aircraft equipment identifier
- `AIRLINE`: Airline identifier
- `LIVERY`: Livery identifier
- `CSL`: CSL identifier

Notes:

- Key/value pair ordering is not guaranteed.
- Presence of each key/value type is not guaranteed.

Example:
```text
#SBDAL625:N7938C:PI:GEN:EQUIPMENT=B738:AIRLINE=DAL
```

<br>

#### Plane Info (Legacy) (`PI:X`)

- Send model-matching information to another client
- Still implemented by some libraries, but is marked as legacy.

| Payload Field | Type               | Description                                                                                       |
|---------------|--------------------|---------------------------------------------------------------------------------------------------|
| Unknown Field | `0`                | This field is always set to `0`. Its purpose is unknown.                                          |
| Engine Type   | integer            | Piston = `0`, Jet = `1`, None = `2`, Helicopter = `3`                                             |
| CSL           | `CSL={identifier}` | CSL model identifier. Some older clients omit the `CSL=` prefix and instead use `~` e.g., `~PA24` |

Example:
```text
#SBDAL625:N7938C:PI:X:0:1:CSL=A320_DAL
```

<br>

#### Plane Info Request (FSInn) (`FSIPIR`)

- Request and send model-matching information to another client.
- Even though this packet is explicitly requesting plane info from another client, it is convention to include the requester's own plane info as well.
- This packet is specific to the FSInn pilot client (last updated in 2012)

| Payload Field                      | Type   | Description                                                                                                                                                                        |
|------------------------------------|--------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Unknown Field                      | `0`    | This field is always set to `0`. Its purpose is unknown.                                                                                                                           |
| Airline ICAO Code                  | string | Airline ICAO identifier                                                                                                                                                            |
| Aircraft ICAO Code                 | string | Aircraft ICAO identifier                                                                                                                                                           |
| Empty Field 0                      | N/A    | Modern libraries leave this field empty. Its purpose is unknown. Older clients utilized this field for an unknown floating-point number.                                           | 
| Empty Field 1                      | N/A    | Modern libraries leave this field empty. Its purpose is unknown. Older clients utilized this field for an unknown floating-point number.                                           | 
| Empty Field 2                      | N/A    | Modern libraries leave this field empty. Its purpose is unknown. Older clients utilized this field for an unknown floating-point number.                                           | 
| Empty Field 3                      | N/A    | Modern libraries leave this field empty. Its purpose is unknown. Older clients utilized this field for an unknown possibly-hexadecimal formatted string e.g., `3.3028DC0.98CF9A41` | 
| Aircraft ICAO Type                 | string | ICAO type identifier e.g., `L1P`                                                                                                                                                   |
| Combined Model Matching Identifier | string | Model-matching string                                                                                                                                                              |

Example:
```text
#SBDLH478:GBOZI:FSIPI:0:DLH:A320:::::L2J:FLIGHTFACTOR A320 LUFTHANSA D-AIPC
```

<br>

#### Plane Info (FSInn) (`FSIPI`)

- Send model-matching information to another client.
- Response for a [Plane Info Request (FSInn)](#plane-info-request-fsinn-fsipir).
- This packet is specific to the FSInn pilot client (last updated in 2012).

- Fields are identical to the [Plane Info Request (FSInn)](#plane-info-request-fsinn-fsipir) packet.

Example:
```text
#SBDLH478:GBOZI:FSIPIR:0:DLH:A320:::::L2J:FLIGHTFACTOR A320 LUFTHANSA D-AIPC
```

<br>

#### Interim Position (`I`)

- Used by the SquawkBox client to send faster position updates.
- This packet type is deprecated.
- Suffered from severe precision errors.

- The payload fields for this packet are unknown.

<br>

#### Swift Interim Position (`VI`)

- Repurposed interim position update utilized by the swift pilot client.

| Payload Field      | Type                    | Description                                                    |
|--------------------|-------------------------|----------------------------------------------------------------|
| Latitude           | floating-point number   | Geographical latitude in decimal degrees                       |
| Longitude          | floating-point number   | Geographical longitude in decimal degrees                      |
| True Altitude      | integer                 | True altitude in feet                                          |
| Groundspeed        | integer                 | Groundspeed in knots                                           |
| Pitch/Bank/Heading | unsigned 32-bit integer | [Encoded](#pitchbankheading-encoding) pitch, bank, and heading |

Example:
```text
#SBDLH478:GBOZI:VI:43.12578:-72.15841:12008:400:25132146
```

<br>

## Text Message (`#TM`)

- Send a text message to a specific recipient, a group of recipients, or a VHF frequency.

| Field Name | Type   | Description          | Notes |
|------------|--------|----------------------|-------|
| From       | string | Source callsign      |       |
| To         | string | Recipient identifier |       |
| Message    | string | Text Message         |       |

### Text Message Recipient Types

#### `@XXXXX` VHF Frequency

- Send a text message to a VHF frequency.
- Frequency is formatted as `@HHTTT` for a given VHF frequency `1HH.TTT` e.g., `122.800 = @22800`.
- Multiple frequencies may be listed, delimited with `&` characters e.g., `@21950&@19600`
- Broadcasted to all in-range clients.

Example:
```text
#TMN7938C:@22800:Gillespie Traffic, N7989C, taking off runway 27R, right downwind departure.
```

<br>

#### `@49999` ATC Chat

- Send a text message to the ATC chat room.
- Sent to the special recipient [@49999](#49999).
- Broadcasted to all in-range ATC clients.

Example:
```text
#TMSAN_GND:@49999:Yo SAN_TWR, you got any coffee up there?
```

<br>

#### `*S` Wallop

- Send a "Wallop" message.
- Sent to the special recipient `*S`, indicating 'All Supervisors'.
- Broadcasted to all supervisors and administrators on the network.

Example:
```text
#TMSAN_TWR:*S:N7938C just crashed on the runway and he isn't disconnecting.
```

<br>

## Kill Request (`$!!`)

- When sent by a client with Supervisor privilege or above, it is forwarded to the target client, then the target client is disconnected.

| Field Name | Type   | Description                | Notes             |
|------------|--------|----------------------------|-------------------|
| From       | string | Source callsign            |                   |
| To         | string | Recipient/victim callsign  |                   |
| Reason     | string | Reason explaining the kick |                   |

Example:
```text
$!!ABC_SUP:N505GS:Refusing to follow ATC instructions
```

<br>

## Server Error (`$ER`)

- Represents an FSD network error sent by the `SERVER`.

| Field Name        | Type    | Description                                           | Notes                        |
|-------------------|---------|-------------------------------------------------------|------------------------------|
| From              | string  | Source callsign                                       | Always `SERVER`              |
| To                | string  | Recipient callsign                                    | May be `CLIENT` or `unknown` |
| Error Code        | integer | [Server Error Code](/enumerations#server-error-codes) |                              |
| Causing Parameter | string  | Repeated FSD field causing the error                  | May be empty if not relevant |
| Description       | string  | Human-readable description of the error               |                              |

- The error code always fills 3 digits e.g., `006` (`%3d`)

Example:
```text
$ERSERVER:unknown:006::Invalid CID/password.
```

<br>

## Delete Pilot (`#DP`)

- Sent by pilot clients before terminating the connection.
- Broadcasted to all other clients on the server.

| Field Name | Type    | Description                           | Notes             |
|------------|---------|---------------------------------------|-------------------|
| From       | string  | Source callsign                       |                   |
| CID        | integer | User's assigned VATSIM Certificate ID |                   |

Example:
```text
#DPAAL325:1400000
```

<br>

## Delete ATC (`#DA`)

- Sent by ATC clients before terminating the connection.
- Broadcasted to all other clients on the server.

| Field Name | Type                       | Description                           | Notes |
|------------|----------------------------|---------------------------------------|-------|
| From       | string                     | Source callsign                       |       |
| CID        | integer                    | User's assigned VATSIM Certificate ID |       |
| Challenge  | hexadecimal-encoded string | Auth challenge                        |       |

Example:
```text
#DASAN_TWR:1555555
```

<br>

## Auth Challenge (`$ZC`)

- Interrogate the other side of the connection using the [VATSIM Auth](/vatsim-auth) obfuscation scheme.

| Field Name | Type                       | Description        | Notes |
|------------|----------------------------|--------------------|-------|
| From       | string                     | Source callsign    |       |
| To         | string                     | Recipient callsign |       |
| Challenge  | hexadecimal-encoded string | Challenge data     |       |

Example:
```text
$ZCN7938C:SERVER:0123456789abcdef
```

## Auth Response (`$ZR`)

- Respond to an [Auth Challenge](#auth-challenge-zc)

| Field Name         | Type                         | Description             | Notes |
|--------------------|------------------------------|-------------------------|-------|
| From               | string                       | Source callsign         |       |
| To                 | string                       | Recipient callsign      |       |
| Challenge Response | hexadecimal-encoded MD5 hash | Challenge response data |       |

Example:
```text
$ZRSERVER:N7938C:4d87482917dc9c2395bd2df151835734
```

<br>

## Notes

### Fast Pilot Positions
 
There are three variants of the Fast Pilot Position packet:

| Identifier | Type      | Interval | Enabled by Default |
|------------|-----------|----------|--------------------|
| `^`        | "Fast"    | 5 Hz     | false              |
| `#SL`      | "Slow"    | 0.2 Hz   | true               |
| `#ST`      | "Stopped" | 0.2 Hz   | true               |

Fast pilot position packets were added to the protocol in 2022 to accomodate the VATSIM Velocity release, increasing the pilot position update frequency from 0.2Hz to 5Hz.
The format and operation of "slow" pilot position updates (`@`) was left unchanged.
Fast positions (also known as "visual" updates) also include velocity information, measured in meters/second on all 3 axes, as well as radians/second on all rotational axes.
Fast positions repeat the groundspeed, altitude, and pitch/bank/heading information found in standard `@` position updates.

(`^`) packets are **not** sent at 5Hz by default. 
The server instructs the client whether to send these fast position updates using a "Send Fast Positions" (`$SF`) packet, which contains a boolean flag.
(`^`) packets are sent at 5Hz intervals by the client when enabled by the server.
(`#ST`) **or** (`#SL`) variants are *always* sent alongside standard slow (`@`) packets at 0.2Hz intervals, regardless of the Send Fast Position (`$SF`) state.
If the pilot client determines that the aircraft is exhibiting zero velocity, "stopped" (`#ST`) packets are used. Otherwise, "slow" (`#SL`) packets are used.

### Pitch/Bank/Heading Encoding

An aircraft's pitch, bank, and heading are encoded into a single unsigned 32-bit integer for use in position update packets.

- Each angle is mapped into a 10-bit number (`0..1023`).
- Angles are in degrees. 0 degrees = `0`. 360 degrees = `1023`.
- Pitch is shifted left by 22 bits.
- Bank is shifted left by 12 bits.
- Heading is shifted left by 2 bits.
- The lowest 2 bits are always zero.

```text
   Bit index:   31            22 21           12 11            2 1   0
                |<-- 10 bits -->|<-- 10 bits -->|<-- 10 bits -->|<-2->
                +---------------+---------------+---------------+-----+
                |     PITCH     |     BANK      |    HEADING    |  0  |
                +---------------+---------------+---------------+-----+
      Shifts:    << 22           << 12           << 2
```

### Special Recipients

#### `SERVER`

- Callsign reserved for the FSD server.

#### `FP`

- Callsign reserved for some flightplan transactions.

#### `@94835`

- Special frequency telling the server to broadcast to all in-range ATC clients.

#### `@94836`

- Special frequency telling the server to broadcast to all in-range pilot clients.

#### `@49999`

- Special frequency for ATC-only `#TM` chat messages.
- Sent to all in-range ATC clients.

#### `*S`

- Special frequency for wallop `#TM` chat messages.
- Sent to all supervisors and administrators connected to the network.
