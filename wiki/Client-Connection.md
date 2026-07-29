# Client Connection

openfsd supports the modern VATSIM FSD protocol. **Clients using protocol revisions older than `100` (VatsimAuth) are *not* supported.** This includes clients designed to connect to the classic [Marty Bochane FSD2 server](https://github.com/kuroneko/fsd).

## Primary path: openfsd Client Setup

**Product:** openfsd Client Setup (`openfsd-client`) — monorepo auxiliary binary that configures **user-installed** third-party clients for a private openfsd network.

| | |
|--|--|
| **Operator guide** | [docs/client-injector/README.md](../docs/client-injector/README.md) |
| **Design** | [docs/design/client-runtime-injector.md](../docs/design/client-runtime-injector.md) |
| **vPilot research** | [docs/client-injector/research/vpilot-3.12.1.md](../docs/client-injector/research/vpilot-3.12.1.md) |

### Phase 0 (honest limits)

Phase 0 is **on-disk apply + config** (reversible backups) — **not** in-memory inject.

- **Default JWT path** (`/api/v1/fsd-jwt`): max public host length **12** characters for in-place PE patch.
- **`--prefer-short-jwt`** + server short path **`POST /j`** (A8: handler must be `getFsdJwt`, not a bare 302): max host **25**. Stock openfsd still **302-redirects** `/j` until A8 lands — confirm your deploy before relying on host length 25.
- **R1 free-slot `#US` remap** is still **OPEN** — production long hostnames need **A8 short paths** and/or short DNS until R1 lands.
- **AFV:** retarget voice base if URL **≤ 25** chars; otherwise plan **`-novoice`**. PE AFV disable is **not** until research gate **R2**.
- **Quit the client completely** before Apply / Revert.
- **Never redistribute** vPilot or other proprietary clients; users install from the vendor.
- Headless binary is **CLI-only** (no subcommand → help; Fyne GUI later). **`launch` dry-prints** args only (does not spawn the PE; real/ephemeral spawn is later).

CLI sketch (see [operator guide](../docs/client-injector/README.md) for full flags):

```bash
openfsd-client                          # help (GUI later)
openfsd-client list-profiles
openfsd-client detect --client vpilot   # Discover only; no --install
openfsd-client plan  --client vpilot --install DIR --web-base URL --fsd-host HOST [--prefer-short-jwt] [--afv-base URL]
openfsd-client apply --client vpilot --install DIR --web-base URL --fsd-host HOST ...   # client must be quit
openfsd-client health --client vpilot --install DIR --web-base URL --fsd-host HOST ...  # same endpoints as apply
openfsd-client revert --client vpilot --install DIR
openfsd-client launch --client vpilot --install DIR --web-base URL --fsd-host HOST ...  # dry-print exec line only
```

### status.txt (openfsd subset)

Clients that poll a VATSIM-style status URL should use:

```text
GET {web-base}/api/v1/data/status.txt
```

openfsd serves a **VATSIM-shaped subset** (not a full public VATSIM mirror). Keys of interest include **`json3`**, **`url1`**, and **`servers.live`** pointing at openfsd data/server list feeds. See `internal/web/data_templates/status.txt`.

### Historical external patch tools

Older CLI utilities remain historical references; prefer **`openfsd-client`** for openfsd:

- [vpilot-patch-utility](https://github.com/renorris/vpilot-patch-utility) (archived; vPilot 3.11.1-era)
- [openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility)

---

## VRC

VRC 1.2.6 is the most recent version that still sends a plaintext password to the FSD server, making it highly compatible with openfsd. You can obtain it [here](https://web.archive.org/web/20250518164130/https://vrc.rosscarlson.dev/files/VRCSetup1.2.6.exe) (archive.org)

You will need to add an entry to the `myservers.txt` file. See the following snippet from the official [VRC documentation](https://vrc.rosscarlson.dev/docs/doc.php?page=advanced_topics):

> *Custom Server List*
>
> *If you need to add servers to the list downloaded from VATSIM, you can create a custom servers file. This file is called myservers.txt and should be kept in your "My Documents\VRC" folder. Each line of this file represents a single custom server. The line must contain the server IP address or hostname, followed by a space, followed by a descriptive name for the server. The name will show up in the server list in the Connect window. Here's an example entry:*
>
> `sweatbox.vatsim.net Public Sweatbox`

Newer versions of VRC such as 1.3.0 use the new VATSIM fsd-jwt authentication system. The binary for these newer versions would need to be patched to call the openfsd fsd-jwt URL (openfsd Client Setup may gain a VRC adapter later; until then use a versioned external tool carefully).

openfsd JWT endpoints (same VATSIM-shaped body; no redirect): canonical `POST /api/v1/fsd-jwt`, short aliases `POST /j` and `POST /api/fsd-jwt` for PE `#US` URL budgets. See [authentication-token.md](../docs/authentication-token.md) for the max_host table.

## Euroscope

Primary path when an Euroscope adapter lands: **openfsd Client Setup** (`openfsd-client`). Until then, historical reference:

- [openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility)
- Third-party: [github.com/Misaka-Nnnnq/openfsd-patch-for-es](https://github.com/Misaka-Nnnnq/openfsd-patch-for-es)

## vatSys

TODO (future Client Setup adapter).

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

**Preferred:** [openfsd Client Setup](../docs/client-injector/README.md) (`openfsd-client`) with the **vPilot 3.12.1** profile.

- Phase 0: on-disk PE + config apply; quit vPilot before Apply.
- JWT host budget: **12** chars default path; **25** with `--prefer-short-jwt` and server `POST /j` (A8).
- R1 free-slot remap still **OPEN** — long production hosts need A8 and/or short DNS.
- AFV: retarget if voice base URL ≤ **25** chars; else launch with **`-novoice`**. PE disable not until R2.
- **Never redistribute** vPilot; install from [vpilot.rosscarlson.dev](https://vpilot.rosscarlson.dev/Download).

Historical: [vPilot Patch Utility](https://github.com/renorris/vpilot-patch-utility) (3.11.1-era; archived).

## xPilot

**Preferred when adapter lands:** openfsd Client Setup. Until then, historical reference:
[openfsd-client-patch-utility](https://github.com/renorris/openfsd-client-patch-utility).

## Voice (AFV)

FSD data and **Audio for VATSIM (AFV)** voice are separate attachment planes. openfsd can serve AFV when started with **`-afv`** (see [Configuration](Configuration.md#afv-voice-optional) and [Deployment](Deployment.md#optional-afv-voice)).

Clients such as **TrackAudio**, **xPilot**, and **VectorAudio** (AFV-Native-derived) need:

1. FSD host/port (or private-server entry) as usual
2. A **voice base URL** pointing at the AFV REST API (`AFV_API_PUBLIC_BASE_URL` / your published HTTPS URL)

AFV authenticates against the same certificate database as FSD (CID + password). UDP voice endpoints are advertised in the AFV channel config from `AFV_UDP_ADVERTISE_IPV4` (must be reachable by clients, including through NAT).

Without `-afv`, use an external voice solution (Discord, etc.); FSD multiplayer still works independently of voice.

For **vPilot via Client Setup**: prefer retargeting the embedded AFV base when the full URL is ≤ **25** characters; otherwise Client Setup plans **`-novoice`** rather than PE-disabling the voice connect path (R2 still open).
