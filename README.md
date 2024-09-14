# openfsd

**openfsd** is an experimental FSD server, implementing the `Vatsim2022` protocol revision ([Velocity](https://forums.vatsim.net/topic/32619-vatsim-announces-velocity-release-date-and-rollout-plan/)) for pilot clients.

## About

FSD (Flight Sim Daemon) is the software/protocol responsible for connecting home flight simulator clients to a single, shared multiplayer world on hobbyist networks such as [VATSIM](https://vatsim.net/docs/about/about-vatsim).
FSD was originally written in the late 90's by [Marty Bochane](https://github.com/kuroneko/fsd) for [SATCO](https://web.archive.org/web/20000619145015/http://www.satco.org/), later to be forked and taken closed-source by VATSIM in 2001. As of April 2024, FSD is still used to facilitate over 140,000 active members connecting their flight simulators to the [network](https://map.vatsim.net).

## Building
```
go mod download
go build -o fsd .
```

A default admin user will be printed to stdout on first startup. A simple web interface can be accessed using these credentials to add more users at `/dashboard`

The server is configured via environment variables:

| Variable Name         | Default Value | Description                                                                                                                       |
|-----------------------|---------------|-----------------------------------------------------------------------------------------------------------------------------------|
| `FSD_ADDR`            | 0.0.0.0:6809  | FSD listen address                                                                                                                |
| `HTTP_ADDR`           | 0.0.0.0:9086  | HTTP listen address                                                                                                               |
| `HTTPS_ENABLED`       | false         | Enable HTTPS                                                                                                                      |
| `TLS_CERT_FILE`       |               | TLS certificate file path                                                                                                         |
| `TLS_KEY_FILE`        |               | TLS key file path                                                                                                                 |
| `DATABASE_FILE`       | ./fsd.db      | SQLite database file path                                                                                                         |
| `MOTD`                | openfsd       | Message to send on FSD client login (line feeds supported)                                                                        |
| `PLAINTEXT_PASSWORDS` | false         | Setting this to true treats the "token" field in the #AP packet to be a plaintext password, rather than a VATSIM-esque JWT token. |
## Overview

Various clients such as [vPilot](https://vpilot.rosscarlson.dev/), [xPilot](https://docs.xpilot-project.org/) and [swift](https://swift-project.org/) are used to connect to VATSIM FSD servers.

This project does not currently support air traffic control clients.

At its core, FSD is a message forwarder. Other than a few direct client/server transactions, the main purpose of the FSD server is to facilitate the passing of messages between flight simulator clients. Albeit, none of this is P2P—all messages are forwarded via a centralized server.

### Protocol

The protocol is entirely plaintext, and can be easily sniffed using packet capture tools such as Wireshark. FSD conventionally listens on TCP port 6809. You can use telnet to interact with an FSD server, try it out:

```
telnet fsd.connect.vatsim.net 6809
```

The few mainstream implementations of FSD have their own nuances within their respective protocols. This project attempts to replicate VATSIM-specific behavior (which differs from other implementations such as [IVAO](https://www.ivao.aero/)).

Throughout this project, I often refer to FSD messages as "packets" or "lines." The term "packet" is referring to the application-layer implementation of FSD, and has nothing to do with any transport or IP layers.

FSD packets are plaintext MS-DOS-style lines that end with (CR/LF) characters.
Each line starts with a packet identifier, which is either 1 or 3 characters in length. Each field within the packet is delimited by a colon `:` character. All numerical values are represented as base-10 (or rarely base-16) encoded ASCII strings. There are no "binary" encodings in the protocol.

Clients are addressed by their plaintext aviation callsigns, e.g. N7938C. FSD packets generally have "From" and "To" fields, where the "From" field can be thought of as a source address (or source callsign, in this case) and the "To" field being a recipient address/callsign. Depending on the packet type, the "To" field can be either a single client address (representing a "Direct Message" of sorts), or another special identifier representing several clients.

For example, the following is a verbatim "Server Identification" message represented as a go string:

```go
"$DISERVER:CLIENT:VATSIM FSD V3.43:d95f57db664f\r\n"
```

Hex representation:

```
    00000000  24 44 49 53 45 52 56 45  52 3a 43 4c 49 45 4e 54   $DISERVE R:CLIENT
    00000010  3a 56 41 54 53 49 4d 20  46 53 44 20 56 33 2e 34   :VATSIM  FSD V3.4
    00000020  33 3a 64 39 35 66 35 37  64 62 36 36 34 66 0d 0a   3:d95f57 db664f..
```

> See the  `protocol` package for packet parsers and serializers.

### Connection Flow

1. **Server Identification packet:** Once the TCP connection has been established, the server identifies itself with a "Server Identification" message, as seen in the example above. The packet identifier is `$DI` (Server Identification). The first field is the "From" field: `SERVER`. The second field is the "To" field: `CLIENT` (`CLIENT` is used here as a generic recipient callsign because the client hasn't announced itself yet.) The third field is the server version identifier. The fourth field is a random hexadecimal string used later for 'VatsimAuth' client verification (see fsd/vatsimauth) 


2. **Login Token request:** Up until 2022, user's passwords were sent in plaintext over FSD (yikes.) Now, login tokens are obtained over HTTPS via `auth.vatsim.net/api/fsd-jwt` (implemented in http_server.go.) Now, logging into FSD, this token is sent in place where the old password used to be... in plaintext (still yikes.)


3. **Client Identification packet:** The client identifies itself in its first message:

```go
"$IDN12345:SERVER:88e4:vPilot:3:8:1000000:5816673295:35df255c\r\n"
```

This includes their callsign (N12345 in this case), client information, "cert ID" aka user ID, an identifier based on the client's MAC address, and another random hexadecimal string used for 'VatsimAuth'

4. **Add Pilot packet:** The client sends its second message containing more login information:

```go
"#APN12345:SERVER:1000000:<login token>:1:101:2:John Doe\r\n"
```

This includes the source callsign again, the cert ID again, the login token previously obtained, the user's requested network rating, the protocol revision they're using, their simulator type, and their real name.


5. **Server MOTD packet:** If all goes well for the login, the server sends a Text Message packet with the message of the day. This serves as the "login successful" message. If the login does not go well, the server will send an error packet and close the connection.


```go
"#TMserver:N12345:Welcome to VATSIM! Need help getting started? Visit https://vats.im/plc for excellent resources.\r\n"
```

VATSIM's FSD implementation sends a lowercase '`server`' identifier for this specific message. I have no idea why this is the case. Inconsistencies like this are common across FSD.

### Post-Login

After the client has logged in, it's generally free to send whatever it wants. This project implements logic for the following packets:

### Login-only packets

| Name                  | Identifier | Direction | Description                                                                                                                       |
|-----------------------|------------|-----------|-----------------------------------------------------------------------------------------------------------------------------------|
| Server Identification | `$DI`      | S → C     | Server identification                                                                                                             |
| Client Identification | `$ID`      | C → S     | Client identification                                                                                                             |
| Add Pilot             | `#AP`      | C ↔ S     | Client login information. Broadcasted to all other clients on server with the "Token" field omitted, assuming a successful login. |


### Post-login packets

| Name                               | Identifier | Description                                                                                                                                                                                                                                                                                                                                                                                                                                           |
|------------------------------------|------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Auth Challenge                     | `$ZC`      | 'VatsimAuth' challenge                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Auth Challenge Response            | `$ZR`      | Response for an Auth Challenge.                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Client Query                       | `$CQ`      | Query another client for some information. In some cases, `SERVER` can be the recipient. There are cases when this packet type can also be used to broadcast unsolicited information to several other clients.                                                                                                                                                                                                                                        |
| Client Query Response              | `$CR`      | Respond to a client query.                                                                                                                                                                                                                                                                                                                                                                                                                            |
| Delete Pilot                       | `#DP`      | Sent when a client would like to disconnect. Broadcasted to all other clients on server.                                                                                                                                                                                                                                                                                                                                                              |
| Fast Pilot Position (Fast Type)    | `^`        | Contains position and velocity information for the client's airplane. Broadcasted to all clients within reasonable geographical range. Sent 5 times per second. This packet is only sent when the server signals the client with a "Send Fast" packet marked as `true`, and the velocity of the airplane is > 0.                                                                                                                                      |
| Fast Pilot Position (Slow Type)    | `#SL`      | Contains position and velocity information for the client's airplane. Broadcasted to all clients within reasonable geographical range. Sent once every 5 seconds. This packet is only sent when the server signals the client with a "Send Fast" packet marked as `false`.                                                                                                                                                                            |
| Fast Pilot Position (Stopped Type) | `#ST`      | Contains position and velocity information for the client's airplane. Broadcasted to all clients within reasonable geographical range. Sent 5 times per second. This packet is only sent when the server signals the client with a "Send Fast" packet marked as `true`, and the velocity of the airplane is zero.                                                                                                                                     |
| Normal Pilot Position              | `@`        | Contains position and other miscellaneous airplane-related information. Broadcasted to all clients within reasonable geographical range. Sent once every 5 seconds. This packet is repeatedly broadcasted from the client throughout the entire lifetime of the connection.                                                                                                                                                                           |
| Send Fast                          | `$SF`      | Sent by the server to signal whether the client should send Fast Pilot Position packets 5 times per second, rather than just the standard pilot positions once per 5 seconds.                                                                                                                                                                                                                                                                         |
| Error                              | `$ER`      | Sent by the server when it encounters an error                                                                                                                                                                                                                                                                                                                                                                                                        |
| Kill                               | `$!!`      | When sent by a privileged user, it is forwarded to the "victim" client, then the victim client is kicked from the server.                                                                                                                                                                                                                                                                                                                             |
| Ping                               | `$PI`      | Ping                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Pong                               | `$PO`      | Respond to a Ping                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Plane Info Request                 | `#SB`      | Query another client for model matching information                                                                                                                                                                                                                                                                                                                                                                                                   |
| Plane Info Response                | `#SB`      | Respond to a Plane Info Request                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Text Message                       | `#TM`      | Used to send text messages. Depending on the value of the recipient field, a text message can represent any of the following: <br/>— Radio message: a message to be broadcasted to all airplanes on a simulated VHF frequency<br/>— Direct message: addressed to a single client<br/>— "Wallop" message: a message that is forwarded to all users with "Supervisor" or higher privilege)<br/>— Broadcast message: forwarded to all connected clients. |


