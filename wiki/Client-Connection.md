# Client Connection

openfsd supports the modern VATSIM FSD protocol. **Clients using protocol revisions older than `100` (VatsimAuth) are *not* supported.** This includes clients designed to connect to the classic [Marty Bochane FSD2 server](https://github.com/kuroneko/fsd). 

## VRC

VRC 1.2.6 is the most recent version that still sends a plaintext password to the FSD server, making it highly compatible with openfsd. You can obtain it [here](https://web.archive.org/web/20250518164130/https://vrc.rosscarlson.dev/files/VRCSetup1.2.6.exe) (archive.org)

You will need to add an entry to the `myservers.txt` file. See the following snippet from the official [VRC documentation](https://vrc.rosscarlson.dev/docs/doc.php?page=advanced_topics):

> *Custom Server List*
>
> *If you need to add servers to the list downloaded from VATSIM, you can create a custom servers file. This file is called myservers.txt and should be kept in your "My Documents\VRC" folder. Each line of this file represents a single custom server. The line must contain the server IP address or hostname, followed by a space, followed by a descriptive name for the server. The name will show up in the server list in the Connect window. Here's an example entry:*
>
> `sweatbox.vatsim.net Public Sweatbox`

Newer versions of VRC such as 1.3.0 use the new VATSIM fsd-jwt authentication system. The binary for these newer versions would need to be patched to call the openfsd fsd-jwt URL.

## Euroscope

External prior-art patcher: [openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility).

Monorepo research (hash + offsets; **Apply not enabled** yet — Coming soon in
`openfsd-client`): [docs/client-injector/research/euroscope-3.2.9.md](../docs/client-injector/research/euroscope-3.2.9.md).

Some additional 3rd party work can be found here: [github.com/Misaka-Nnnnq/openfsd-patch-for-es](https://github.com/Misaka-Nnnnq/openfsd-patch-for-es)

## vatSys

External prior-art patcher: [openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility).

Monorepo research (hash + CLR `#US` / ldstr sites; **Apply not enabled** yet —
Coming soon in `openfsd-client`): [docs/client-injector/research/vatsys-1.4.19.md](../docs/client-injector/research/vatsys-1.4.19.md).

## Swift

1. In the Settings > Servers menu: Make a new server entry for openfsd with the correct address and port.
2. Select **FSD (Private)** for the "Eco." field
3. Select **FSD [VATSIM]** for the "Type" field.
4. In the FSD tab, enable the following flags (send and receive to TRUE):
   - "Parts"
   - "Gnd. flag"
   - "Send visual pos."
5. Leave the "Fast pos" flag unchecked.

## vPilot

Use **openfsd Client Setup** (`openfsd-client`) for private-network endpoint
retarget (Windows vPilot only today). Operator guide:
[docs/client-injector/README.md](../docs/client-injector/README.md).

Installers (plain binary; no Fyne-specific packaging): Windows Setup.exe, macOS
pkg/dmg, Linux deb — see [packaging/openfsd-client](../packaging/openfsd-client/README.md).
Settings live under the OS user config dir (`openfsd-client`), not under the
install path.

External prior art: [vPilot Patch Utility](https://github.com/renorris/vpilot-patch-utility).

## Voice (AFV)

FSD data and **Audio for VATSIM (AFV)** voice are separate attachment planes. openfsd can serve AFV when started with **`-afv`** (see [Configuration](Configuration.md#afv-voice-optional) and [Deployment](Deployment.md#optional-afv-voice)).

Clients such as **TrackAudio** and **VectorAudio** (AFV-Native-derived) need:

1. FSD host/port (or private-server entry) as usual
2. A **voice base URL** pointing at the AFV REST API (`AFV_API_PUBLIC_BASE_URL` / your published HTTPS URL)

TrackAudio Client Setup status: **docs only** (no PE offsets; config-only
adapter when evidence lands) — see [docs/client-injector/research/trackaudio.md](../docs/client-injector/research/trackaudio.md).

AFV authenticates against the same certificate database as FSD (CID + password). UDP voice endpoints are advertised in the AFV channel config from `AFV_UDP_ADVERTISE_IPV4` (must be reachable by clients, including through NAT).

Without `-afv`, use an external voice solution (Discord, etc.); FSD multiplayer still works independently of voice.
