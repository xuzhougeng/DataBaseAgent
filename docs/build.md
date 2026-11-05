# Build and Install

DBA is a Wails v3 desktop application written in Go with an embedded vanilla-JS frontend. There is no npm step; `go build` produces the whole product.

## Platform matrix

| Platform | Runtime dependency | Native build dependency | Artifact |
| --- | --- | --- | --- |
| Windows 10/11 | WebView2 Runtime (bundled with Windows 11) | none — pure Go | `bin/DBA.exe` |
| macOS 11+ | none (system WebKit) | Xcode Command Line Tools (`xcode-select --install`) | `bin/DBA.app` |
| Linux | GTK 4 + WebKitGTK 6.0 | `libgtk-4-dev libwebkitgtk-6.0-dev` (Debian/Ubuntu) or `gtk4-devel webkitgtk6.0-devel` (Fedora) | `bin/DBA` |

Remote execution additionally requires an `ssh` client on the machine running DBA (Windows ships OpenSSH).

## Build scripts

macOS / Linux:

```bash
scripts/build.sh                # current platform
```

```bash
scripts/build.sh all            # windows + linux + darwin
```

Windows (PowerShell):

```powershell
./scripts/build.ps1
```

```powershell
./scripts/build.ps1 -Target all
```

Targets: `windows`, `darwin`, `linux`, `all`, `setup`, `dmg`. Options:

| Option | Effect |
| --- | --- |
| `--debug` / `-Debug` | Keep symbols, disable the `production` tag, enable Wails development features. |
| `GOARCH=arm64` | Switch Windows/Linux architecture. |
| `TAGS=gtk3` | Build Linux against GTK 3 + WebKit2GTK 4.1 for distributions without WebKitGTK 6.0. |
| `PUREGO=1` | Use the Wails purego backend on the host platform too (no C toolchain). |

Release builds use `-tags production -ldflags "-s -w"` (plus `-H windowsgui` on Windows) and target macOS 11.0.

### Cross-compilation

Windows targets are pure Go and build anywhere. For a non-host macOS or Linux target the scripts switch to the Wails purego backend (`CGO_ENABLED=0 -tags purego`), which needs no C toolchain but is marked experimental by Wails — build on the target platform for releases. `build.ps1` defaults to `http://127.0.0.1:7897` as module-download proxy when `HTTPS_PROXY` is unset; `build.sh` only inherits the environment.

## Installers

```bash
scripts/build.sh setup          # bin/DBA-Setup.exe (NSIS 3 required)
```

```bash
scripts/build.sh dmg            # bin/DBA.dmg (macOS host only, hdiutil)
```

`setup` (`./scripts/build.ps1 -Target setup` on Windows) needs `makensis` on `PATH`: `brew install makensis`, `apt install nsis`, or the installer from nsis.sourceforge.io. The installer is per-user (`%LOCALAPPDATA%\Programs\DBA`), needs no administrator rights, registers a Start-menu entry and an uninstaller, prompts to download WebView2 when missing, and leaves the workspace untouched on uninstall. Script: `scripts/installer.nsi`.

`dmg` bundles `DBA.app` with an `Applications` symlink. Neither installer is code-signed: on macOS use right-click → Open or `xattr -dr com.apple.quarantine DBA.app`; on Windows SmartScreen may require "More info → Run anyway".

## Workspace

| Platform | Default workspace |
| --- | --- |
| Windows | `%APPDATA%\DataBaseAgent` |
| macOS | `~/Library/Application Support/DataBaseAgent` |
| Linux | `~/.config/DataBaseAgent` |

Contents: `workbench.json` (all state) and `projects/<projectId>/results/<jobId>/` (artifacts and manifests). Override with `-workspace <dir>`; on macOS `open bin/DBA.app --args -workspace <dir>`. A second desktop instance on the same workspace focuses the existing window instead of starting.

## Browser preview

```bash
go run ./cmd/dba-server -port 3415 -workspace .tmp/dev-workspace
```

Same backend, same assets, no native toolchain, bound to `127.0.0.1`. Do not point the preview and the desktop app at the same workspace at the same time.

## Verification

```bash
go test ./... && go vet ./... && node --check frontend/workbench.js
```

Add `-race` to the test command for the scheduler tests. The original design package with its checksums is preserved under `design/DBA_design_package/`.
