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

See [here](https://github.com/renorris/openfsd-client-patch-utility).

Some additional 3rd party work can be found here: [github.com/Misaka-Nnnnq/openfsd-patch-for-es](https://github.com/Misaka-Nnnnq/openfsd-patch-for-es)

## vatSys

TODO

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

See [vPilot Patch Utility](https://github.com/renorris/vpilot-patch-utility)

## xPilot

See [here](https://github.com/renorris/openfsd-client-patch-utility).
