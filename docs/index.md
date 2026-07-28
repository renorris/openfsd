# VATSIM FSD Protocol Documentation

> Updated July 2026

FSD is an application-layer TCP/IP protocol facilitating real-time communications between clients connected to the [VATSIM](https://vatsim.net) network (and private FSD servers that speak the same dialect).

This documentation is **reverse-engineered and empirical**. It is **not** an official VATSIM specification. Claims that are not confirmed on the wire or in known client/server behavior are marked as such (e.g. **unconfirmed**, **unknown**, or **TODO**).

See [openfsd](https://github.com/renorris/openfsd) for a free and open-source FSD server implementation.

## Key topics

| Topic | Doc |
|-------|-----|
| Introduction / framing | [about.md](about.md) |
| Packet reference | [protocol.md](protocol.md) |
| Client/server `CAPS` | [capabilities.md](capabilities.md) |
| Enumerations | [enumerations.md](enumerations.md) |
| FSD JWT login tokens | [authentication-token.md](authentication-token.md) |
| In-band client authenticity (`$ZC`/`$ZR`) | [vatsim-auth.md](vatsim-auth.md) |
| X-Plane airport data (sweatbox layouts) | [xplane-airport-data.md](xplane-airport-data.md) |

## Related openfsd docs (outside this protocol set)

| Topic | Location |
|-------|----------|
| Operator wiki (deploy / config / clients) | Repository [`wiki/`](../wiki/) |
| Operator REST + OpenAPI | [`internal/web/README.md`](../internal/web/README.md) |
| Design notes (AFV, cluster, sweatbox, editor) | [`docs/design/`](design/) |
| Contributor package rules | [`AGENTS.md`](../AGENTS.md) |

*This documentation is an independent work and is not affiliated with, endorsed by, or associated with VATSIM, Inc.*
