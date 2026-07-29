# openfsd-client installers

Traditional OS installers for the **openfsd Client Setup** GUI (`cmd/openfsd-client`).
Each installer installs the **plain Go binary** (no Fyne packaging wrappers).

| Platform | Installer | Install location | User config (app-owned) |
|----------|-----------|------------------|-------------------------|
| **Windows** | Inno Setup `.exe` | `%ProgramFiles%\openfsd-client\openfsd-client.exe` | `%AppData%\openfsd-client\` |
| **macOS** | `.pkg` (+ `.dmg` for drag-install) | `/Applications/openfsd Client Setup.app` | `~/Library/Application Support/openfsd-client/` |
| **Linux** | `.deb` | `/usr/bin/openfsd-client` | `~/.config/openfsd-client/` |

The `.app` on macOS is a **minimal** bundle: `Info.plist` + the same compiled binary as `Contents/MacOS/openfsd-client`. Finder needs a bundle for double-click; it is not a Fyne-generated package.

## Local build

```bash
# From repo root — requires platform tools (see below).
./packaging/openfsd-client/build.sh
# Artifacts under dist/openfsd-client/
```

### Tools

| Platform | Tools |
|----------|--------|
| Windows | Go, [Inno Setup 6](https://jrsoftware.org/isinfo.php) (`ISCC.exe`) |
| macOS | Go, Xcode CLT (`pkgbuild`, `productbuild`, `hdiutil`, `iconutil`) |
| Linux | Go, [nfpm](https://nfpm.goreleaser.com/) |

CI builds all three on GitHub Actions (see `.github/workflows/openfsd-client-packages.yml`).

## Version

`VERSION` env, else `git describe --tags --always --dirty`, else `0.0.0-dev`.
