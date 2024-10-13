# openfsd

[![license](https://img.shields.io/github/license/renorris/openfsd)](https://github.com/renorris/openfsd/blob/main/LICENSE)
&nbsp;[![docker](https://img.shields.io/docker/image-size/renorris/openfsd/latest)](https://hub.docker.com/repository/docker/renorris/openfsd)

**openfsd** is a multiplayer flight simulation server implementing the protocol colloquially known as "FSD" (Flight Sim Daemon).
It is specifically modelled to reflect the VATSIM [Velocity](https://forums.vatsim.net/topic/32619-vatsim-announces-velocity-release-date-and-rollout-plan/) protocol revision for pilot clients.

## About

FSD is the software/protocol responsible for connecting home flight simulator clients to a single, shared multiplayer world on hobbyist networks such as [VATSIM](https://vatsim.net/docs/about/about-vatsim) and [IVAO](https://www.ivao.aero/).
FSD was originally written in the late 90's by [Marty Bochane](https://github.com/kuroneko/fsd) for [SATCO](https://web.archive.org/web/20000619145015/http://www.satco.org/), later to be forked and taken closed-source by VATSIM in 2001. 
As of October 2024, FSD is still used to facilitate over 140,000 active members connecting their flight simulators to the [network](https://vatsim-radar.com/).

## Docker

[Prebuilt images](https://hub.docker.com/r/renorris/openfsd) are available for x86_64 and arm64.

Example:

```
docker run -e IN_MEMORY_DB=true \
-p 6809:6809 -p 8080:8080 renorris/openfsd:latest
```

Also see the example [docker-compose.yml](https://github.com/renorris/openfsd/blob/main/docker-compose.yml).

## Manual Build
```
git clone https://github.com/renorris/openfsd && cd openfsd/
go build -o openfsd .
```

## Setup

Persistent storage utilizes MySQL. You will need a MySQL server to point openfsd at.

Use the following environment variables to configure the server:

| Variable Name   | Default Value | Description                                                                                                                                                                                                                           |
|-----------------|---------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `FSD_ADDR`      | 0.0.0.0:6809  | FSD listen address                                                                                                                                                                                                                    |
| `HTTP_ADDR`     | 0.0.0.0:8080  | HTTP listen address                                                                                                                                                                                                                   |
| `DOMAIN_NAME`   |               | Server domain name. This is required to properly set status.txt content.<br>e.g. `myopenfsdserver.com`<br>Optionally use `HTTP_DOMAIN_NAME` and `FSD_DOMAIN_NAME` for more granularity if required.                                   |
| `TLS_ENABLED`   | false         | Whether to **flag** that TLS is enabled somewhere between openfsd and the client, so the status.txt API will format properly. This will **not** enable TLS for the internal HTTP server. Use TLS_CERT_FILE and TLS_KEY_FILE for that. |
| `TLS_CERT_FILE` |               | TLS certificate file path (setting this enables HTTPS. otherwise, plaintext HTTP will be used.)                                                                                                                                       |
| `TLS_KEY_FILE`  |               | TLS key file path                                                                                                                                                                                                                     |
| `IN_MEMORY_DB`  | false         | Enables an ephemeral in-memory database in place of a real MySQL server.<br>This should only be used for testing.                                                                                                                     |
| `MYSQL_USER`    |               | MySQL username                                                                                                                                                                                                                        |
| `MYSQL_PASS`    |               | MySQL password                                                                                                                                                                                                                        |
| `MYSQL_NET`     |               | MySQL network protocol e.g. `tcp`                                                                                                                                                                                                     |
| `MYSQL_ADDR`    |               | MySQL network address e.g. `127.0.0.1:3306`                                                                                                                                                                                           |
| `MYSQL_DBNAME`  |               | MySQL database name                                                                                                                                                                                                                   |
| `MOTD`          | openfsd       | "Message of the Day." This text is sent as a chat message to each client upon successful login to FSD.                                                                                                                                |

For 99.9% of use cases, it is also recommended to set:
```
GOMAXPROCS=1
```
This environment variable limits the number of operating system threads that can execute user-level Go code simultaneously.
Scaling to multiple threads will generally only make the process slower.

openfsd supports "fsd-jwt" authentication over HTTP.
VATSIM uses this standard; clients first obtain a login token by POSTing to `/api/fsd-jwt` with:

```json
{
   "cid": "9999999",
   "password": "supersecretpassword"
}
```

A token is returned and placed in the token/password field of the `#AP` FSD login packet.
To use a vanilla VATSIM client with openfsd,
(except Swift, see below) it will need to be modified to point to openfsd's "fsd-jwt" endpoint:
```
/api/v1/fsd-jwt
```

### Credentials

A default administrator user will be printed to stdout on first startup:
```
    DEFAULT ADMINISTRATOR USER:
    CID:              100000
    PRIMARY PASSWORD: <primary password>
    FSD PASSWORD:     <FSD password>
```
Two unique passwords are stored for each user since FSD runs over an insecure channel.
Use the primary password over a secure channel if possible (web interface over HTTPS) to perform sensitive account-related management. 
Use the FSD password in your pilot client to connect to FSD.

### Web Interface

One can access their account via the web interface served by the HTTP endpoint.
Administrators and supervisors can create/mutate user records via the administrator dashboard.

### Available HTTP calls

- `/api/v1/users` (See [documentation](https://github.com/renorris/openfsd/tree/main/web))
- `/api/v1/data/openfsd-data.json` VATSIM-esque [data feed](https://github.com/renorris/openfsd/blob/main/web/DATAFEED.md)
- `/api/v1/data/status.txt` VATSIM-esque [status.txt](https://status.vatsim.net)
- `/api/v1/data/servers.txt` VATSIM-esque [servers.txt](https://data.vatsim.net/vatsim-servers.txt)
- `/api/v1/data/servers.json` VATSIM-esque [servers.json](https://data.vatsim.net/v3/vatsim-servers.json)
- `/login ... etc` front-end interface

## Connecting

Various clients such as [xPilot](https://docs.xpilot-project.org/), [vPilot](https://vpilot.rosscarlson.dev/) and [swift](https://swift-project.org/) are used to connect to VATSIM FSD servers. 
To connect to openfsd:

- xPilot: one would need to manually recompile the client with the correct JWT token endpoint and FSD server addresses.
- vPilot: see [here](https://github.com/renorris/vpilot-patch-utility).
- Swift: works out of the box. See below.

**Swift Instructions:**

1. In the Settings > Servers menu: Make a new server entry for openfsd with the correct address and port.
2. Select **FSD (Private)** for the "Eco." field
3. Select **FSD [VATSIM]** for the "Type" field.
4. In the FSD tab, enable the following flags (send and receive to TRUE):
   - "Parts"
   - "Gnd. flag"
   - "Fast pos"
   - "Send visual pos."

## Protocol Details

At its core, FSD is a message forwarder.
Other than a few direct client/server transactions, the main purpose of the FSD server is to facilitate the passing of messages between flight simulator clients.
Albeit, none of this is P2P—all messages are forwarded via a centralized server.
The protocol is entirely plaintext, and can be easily sniffed using packet capture tools such as Wireshark.
It conventionally listens on TCP port 6809.
One can use telnet to interact with an FSD server, try it out:

```
telnet <FSD server address> 6809
```

The few mainstream implementations of FSD have their own nuances within their respective protocols. This project attempts to replicate VATSIM-specific behavior (which differs from other implementations such as [IVAO](https://www.ivao.aero/)).

Throughout this project, FSD messages are referred to as "packets" or "lines." The term "packet" is referring to the application-layer implementation of FSD and has nothing to do with any transport or IP layers.

FSD packets are plaintext MS-DOS-style lines that end with (CR/LF) characters.
Each line starts with a packet identifier, which is either 1 or 3 characters in length.
Each field within the packet is delimited by a colon `:` character.
All numerical values are represented as base-10 (or rarely base-16) encoded ASCII strings.
Nothing is encoded as raw binary.

Clients are addressed by their plaintext aviation callsigns, e.g. N7938C. FSD packets generally have "From" and "To" fields, where the "From" field can be thought of as a source address (or source callsign, in this case) and the "To" field being a recipient address/callsign. Depending on the packet type, the "To" field can be either a single client address (representing a "Direct Message" of sorts), or another special identifier representing several clients.

For example, the following is a "Server Identification" FSD packet represented as a string:

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


2. **Login Token request:** Up until 2022, user's passwords were sent in plaintext over FSD (yikes.) Now, login tokens are obtained over HTTPS via `auth.vatsim.net/api/fsd-jwt` (implemented in http_server.go.) Now, logging into FSD, this token is sent in place where the old password used to be... in plaintext (still yikes.) Note that openfsd dynamically supports both options. A client can send either an obtained JWT token or their plaintext password.


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
"#TMSERVER:N12345:openfsd\r\n"
```

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


