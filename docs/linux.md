# Native Linux launcher: design note

Status: built on the branch `linux-native`. This note describes what is there.

## Goal

Today Linux players run `AzothLauncher.exe` in Wine and must first install WebView2 (214 MB) into the prefix. This change adds a native Linux build of the launcher:

- The window, login, settings, search and file work run natively (Wails v2 on Linux uses WebKitGTK, so no WebView2).
- Only the part that must be Windows runs in Wine/Proton: start `NewWorld.exe` suspended, write the four patches, read them back, resume (or kill). This part is a small helper, `azoth-patcher.exe`, that calls the existing `client.Launch()` without changes.
- The Windows build stays exactly as it is. Both builds use the same frontend.

## What was checked first (2026-10-22, PikaOS 4, umu-launcher 1.4.4, GE-Proton10-12)

Each check used a small test exe in a new prefix. The Windows-build prefix was not used.

| Risk | Result |
|---|---|
| Pass data to a Wine exe on **stdin** through `umu-run` | **Does not work.** Proton sends the exe's stdout and stderr to its own log, so the result cannot come back. Proton's setup steps also inherit stdin and use it up before the exe starts: with `PROTON_VERB=runinprefix` the exe reads EOF, and with the default verb (`start.exe /unix`) it waits forever. With Proton's `wine` run directly (no umu, no container), stdin works. |
| Pass data through a **FIFO** (named pipe) through `umu-run` | **Works** in the default container (`waitforexitandrun`, pressure-vessel). The exe opened `Z:\…\in` and `Z:\…\out`, read the request and wrote the result. FIFO data stays in kernel memory and never goes to disk. |
| Game files through a **Unix symlink** in Wine | **Works.** The exe read `NewWorld.exe` through a symlink to an install on a second disk (`/mnt/<disk>/…/Bin64`), inside the container. The size matched (179204176 bytes). |
| **ICMP** from Wine (patches 2 and 3) | **Works** when the host allows unprivileged ping sockets: `IcmpSendEcho` to 1.1.1.1 inside the container got a reply in 5 ms. This PC has `net.ipv4.ping_group_range = 0 2147483647`. What happens when the range does not include the user: **not tested** (it needs a root sysctl change). The launcher checks the range natively and tells the player (see below). |

So the brief's stdin rule changes to: the ticket goes to the helper through a FIFO, never on the command line, never in a file, never in a log. The FIFO is the same kind of channel as stdin (a pipe). Only the way to reach it is different.

## Code split

Build tags only, no copied code. Every file without a suffix builds on both systems.

### Shared (platform-free)

| File | Change |
|---|---|
| `internal/client/client.go`, `validate.go` | No change. `IsSteamInstall` also matches `/steamapps/common/` (it matches `\steamapps\common\` today). |
| `internal/client/redirect.go` | Keeps the patch tables (`StringRedirect`, `Patched`, `validRedirectValue`). `applyRedirect` moves to a `_windows.go` file, because it needs a Windows process handle. Today this file imports `golang.org/x/sys/windows`, so the package does not build on Linux at all. |
| `internal/client/launch.go` (new) | `LaunchOptions`, `LaunchResult`, `ErrPatchFailed`, moved without change from `launch_windows.go`. The Linux side needs these types. |
| `internal/client/prepare.go` (new) | `Prepare`, `CopyUpToDate`, `ensureAssetsLink`, `copyFileIfDifferent`, moved from `prepare_windows.go`. They call two small platform functions: `linkDir(link, target)` and `sameDir(a, b)`. |
| `internal/client/locate.go` | `ResolveGame`, `IsolatedCopyDir` and the VDF parsing (`libraryFolders(data)`), plus `FindSteamInstalls`, which reads the VDF of each root that `steamRoots()` returns. |
| `internal/client/search.go` | `Search`, `scan`, `sortInstalls`. It calls `searchRoots()` and `walkable(e)`, which are per platform. |
| `internal/patcher` (new package) | The helper protocol: request and result types, encode, decode, version check. The Windows helper and the Linux launcher both use it. |
| `internal/account/account.go` | No change (login, PKCE, ticket). |
| `internal/account/store.go` | `Save`, `Load` and `Clear` keep JSON and the expiry check. They call `saveBlob`, `loadBlob` and `clearBlob`, which are per platform. |
| `internal/config/config.go` | One `config.json`, one `Config` type and one `Load`/`Save` for both systems. Only the folder differs (`DataDir()` reads a per-platform variable name). One new field, used only on Linux and omitted from JSON when empty: `protonPath`. A Windows `config.json` stays byte for byte the same. Where the isolated copy lives is left to upstream's copy-folder setting (`feat/copy-folder`), so it works the same on both systems. |
| `app.go`, `flow.go` | `play()` calls `launchGame(opts)`, which is per platform. Player texts that contain `Bin64\NewWorld.exe` use the platform's path separator. |
| `frontend/` | The settings dialog gets one field (Proton folder). It shows only when the new binding `GetPlatform()` returns `"linux"`. |

### `_windows.go` (today's code, moved, not changed)

`launch_windows.go`, `redirect_windows.go` (`applyRedirect`), `prepare_windows.go` (`CreateJunction`, `linkDir`, `sameDir` that ignores case and `\??\`), `locate_windows.go` (registry `steamRoots`), `search_windows.go` (`FixedDrives`, Win32 attributes in `walkable`), `account/dpapi_windows.go` (DPAPI file blob), `config_windows.go` (`%LOCALAPPDATA%`), and in `main`: `launch_windows.go` (`launchGame` calls `client.Launch`) and `options_windows.go` (the `windows.Options` block from `main.go`).

### `_linux.go` (new)

| Part | Design |
|---|---|
| Data dir | `$XDG_DATA_HOME/AzothLauncher` (default `~/.local/share/AzothLauncher`): `config.json`, `account.dat` (only if there is no keyring), `client/`, `prefix/`, `bin/`. |
| Steam roots | `~/.local/share/Steam`, `~/.steam/steam`, `~/.var/app/com.valvesoftware.Steam/.local/share/Steam`. Each root's `steamapps/libraryfolders.vdf` gives the libraries. A path that two roots both give is checked once (after `filepath.EvalSymlinks`). |
| Scan roots | The home folder, plus each mount point in `/proc/self/mountinfo` whose file system type is a real disk type (ext2/3/4, btrfs, xfs, f2fs, ntfs, ntfs3, exfat, vfat, zfs, bcachefs). The scan never enters `/proc`, `/sys`, `/dev`, `/run`, `/tmp`, `/snap` or the launcher's own data dir. It does not follow symlinks. Hidden folders (name starts with `.`) are skipped, except the Steam roots above, which come from the Steam step. |
| Isolated copy | `<data>/client`. The `assets` entry is a plain `os.Symlink` to the install. `sameDir` compares cleaned paths (case-sensitive). |
| Prefix | `<data>/prefix`. umu creates it on the first Play. |
| Runner | `umu-run` from `PATH`. `PROTONPATH` comes from the `protonPath` setting; if that is empty, umu downloads and uses UMU-Proton itself. `GAMEID=umu-1063730` (New World's Steam id), so umu-protonfixes can apply fixes for this game. `PROTON_VERB=run`, so a second Play does not wait for a running wineserver. |
| Session store | Secret Service over D-Bus (`org.freedesktop.secrets`: GNOME Keyring, KWallet/ksecretd, KeePassXC). This uses `github.com/godbus/dbus/v5`, which is already in `go.sum` through Wails, so there is no cgo and no libsecret build dependency. The secret is the session JSON, with the attribute `application = azoth-launcher`. If no Secret Service answers, `account.dat` is written with mode 0600 and is **not encrypted**; the README says so. There is no new key and no new secret in the build. |
| Browser | `runtime.BrowserOpenURL` already calls `xdg-open` on Linux. No change. |
| Folder picker | `runtime.OpenDirectoryDialog` works on Linux (GTK). No change. |
| Window options | `options_linux.go`: `linux.Options{ProgramName: "azoth-launcher", Icon: build/appicon.png}`. |
| Runner code | `internal/wine` (Linux only): `Launch` (pipes, umu-run, replies), `InstallHelper` (sha256 check and copy), `PingAllowed`. `launch_linux.go` in `main` connects it to Play. |
| Ping check | Before Play, the launcher reads `/proc/sys/net/ipv4/ping_group_range` and compares it with the user's groups. If the user cannot open ping sockets, Play still runs, and the status line says: *"Started. The menu ping will show 9999 ms because this system blocks ping for normal users (net.ipv4.ping_group_range). See the README."* |

## Helper: `cmd/azoth-patcher`

A Windows-only `main` package (`//go:build windows`), about 100 lines. It reads one request, calls `client.Launch()` without a `Log` function (so nothing goes to Proton's log) and writes one result.

### Protocol (version 1)

1. The launcher makes `<data>/run/` with mode 0700, then two FIFOs with random names and mode 0600: `req-<16 hex>` and `rep-<16 hex>`. It opens both read-write (such an open of a FIFO never blocks) and writes one JSON line to the request pipe:
   `{"v":1,"binDir":"Z:\\…\\Bin64","gatewayAddr":"host:port","pingHost":"host","persona":"<ticket>"}`
   The line waits in the pipe until the helper reads it.
2. The launcher starts:
   `umu-run <data>/bin/azoth-patcher.exe -request Z:\…\req-… -reply Z:\…\rep-…`
   with `WINEPREFIX`, `GAMEID`, `PROTON_VERB=run` and, if set, `PROTONPATH`. The paths are not secret. The ticket is not in the arguments or the environment.
3. The helper writes `{"v":1,"stage":"ready"}` on the reply pipe, reads the request (at most 30 seconds, at most 4 KiB), calls `client.Launch()` without a `Log` function (so nothing goes to Proton's log) and writes
   `{"v":1,"stage":"done","ok":true,"result":{…LaunchResult fields…}}`
   or, on failure, `"ok"` false with `"error"` and `"patchFailed"`. `LaunchResult` never holds the ticket (only `PersonaPatched`).
4. The launcher closes and deletes both FIFOs in every case.

The helper refuses to do anything (exit code 2, no process started) when: a flag is missing or unknown, there is an extra argument, a pipe cannot be opened, no request arrives within 30 seconds, the line is too long, the JSON is not valid, `v` is not 1, or `binDir` or `gatewayAddr` is empty.

The launcher waits at most 10 minutes for `ready` (the first Play can download UMU-Proton and the Steam runtime and create the prefix, so the status line says *"Setting up Proton (first time only)…"* when the prefix does not exist yet), then at most 2 minutes for `done`. If `umu-run` fails before `ready`, it stops at once. When `patchFailed` is true, the player sees today's `errPatchFailed` text. The `umu-run` process is waited for in the background, because it can live as long as the game.

### Ship it next to the launcher, not embedded

`azoth-patcher.exe` ships next to the Linux binary (in the AppImage and in the tarball). Before each Play the launcher copies it to `<data>/bin/` if the file there is different.

Why not `go:embed`: the embedded file must exist when Go compiles the `main` package. Then `go test ./...` on Linux would fail on a fresh clone until a build script makes the exe, and the repo would need a committed stand-in file. Shipping next to it keeps `go test ./...` working with no extra step.

Why copy it to `<data>/bin/`: the AppImage is mounted under `/tmp/.mount_*` through FUSE, and it is not certain that pressure-vessel can see that mount. The data dir is known to work.

Integrity: `scripts/release-linux.sh` computes the helper's sha256 and builds it into the launcher with `-X main.patcherSHA256=<hex>`. The launcher refuses a helper with another hash, so a swapped exe never gets the ticket. A build without the value refuses to start the helper, in the same way that a build without a gateway refuses to play.

## Packaging

| Option | For | Against |
|---|---|---|
| **AppImage, using the host's webkit2gtk-4.1** | One file to download and run. It also gives the tarball for free (same files). | Needs FUSE on the host (most desktops have it). It must be built on an old base (Ubuntu 22.04, glibc 2.35) to run on most distributions. |
| Flatpak (GNOME runtime has WebKitGTK) | WebKitGTK is included. | The launcher must start `umu-run` and pressure-vessel on the host. That needs `flatpak-spawn --host`, which is a full sandbox escape, so the sandbox gives nothing and Flathub would likely refuse it. |
| Tarball + `.desktop` file | Simplest. | Two manual steps for the player (unpack, install the `.desktop`). |

**Recommendation: AppImage that uses the host's `libwebkit2gtk-4.1`**, built on Ubuntu 22.04, with the tarball as a second download from the same files. WebKitGTK is not bundled: it is large, it is tied to the host's GPU drivers and graphics stack, and `webkit2gtk-4.1` is in every current distribution (Ubuntu 22.04+, Debian 12+, Fedora, Arch, openSUSE). The build uses the tags `desktop,production,webkit2_41`, so no Wails CLI is needed (same as the Windows build command in the brief).

**umu-launcher is a host dependency, not bundled.** umu updates itself and the Steam runtime, and it is packaged for Arch, Fedora, and Debian/Ubuntu (through its own `.deb` releases). A bundled copy would get old. If `umu-run` is not in `PATH`, Play shows: *"umu-launcher is not installed. See the README for how to install it."*

New files for packaging: `scripts/release-linux.sh` (Linux version of `release.ps1`), `build/linux/azoth-launcher.desktop`. Build output stays in `build/bin/`, which is already in `.gitignore`.

## Tests

`go test ./...` runs on Linux and on Windows. The Windows tests also run under Wine (`GOOS=windows go test -c`, then `wine <pkg>.test.exe`).

- `internal/client`: VDF parsing with Linux and Windows paths; the three Steam roots in a temp `HOME` (the `~/.steam/steam` link counts once); `diskMounts` from a sample `mountinfo` (pseudo file systems, network shares and mounts under the system folders are left out); octal escapes in mount points; the scan skips hidden folders and does not follow links (dot folders and symlinks on Linux, the hidden attribute and junctions on Windows); `linkDir` makes an absolute symlink; `ResolveGame` is case-sensitive on Linux.
- `internal/patcher`: round trip, no ticket in a reply, wrong or missing version, line too long, bad JSON, missing fields, two replies in a row.
- `cmd/azoth-patcher` (Windows, so under Wine here): launch with the right options and no `Log`, a patch failure, and every refusal (missing, unknown or extra arguments, missing pipes, bad requests). The launch function is never called on a refusal.
- `internal/wine` with a fake `umu-run` script: the ticket is not in the arguments or the environment, the pipes are 0600 in a 0700 folder and are deleted, `PROTONPATH` only when set, `umu-run` failing, never answering, or hanging, and cancel. Also the helper install (sha256 refused, missing, replaced when changed) and `ping_group_range` parsing.
- `internal/account`: the keyring store with a fake keyring (no file on disk, an older `account.dat` is removed), the file fallback (mode 0600, expiry, corrupt file, `Clear`), and a refused keyring that does not fall back to disk. The DPAPI test is Windows only.
- `internal/config`: `DataDir` with and without its variable; the Linux settings round-trip and stay out of `config.json` while empty.
- `main`: the Proton folder check. The app tests point `DBUS_SESSION_BUS_ADDRESS` at nothing, so they never write to the real keyring.

Gated tests against real things:

- `AZOTH_CLIENT_BIN=<game>/Bin64`: the patch sites in the real exe, and a real isolated copy (then `CopyUpToDate` is true).
- `AZOTH_KEYRING_TEST=1`: one item in the real Secret Service, written, read and deleted.
- `AZOTH_UMU_E2E=1` with `AZOTH_CLIENT_BIN` (and optionally `PROTONPATH`, `AZOTH_E2E_PREFIX`): builds the helper, makes an isolated copy, runs the helper through the real `umu-run` against the real game with a test persona and a gateway where nothing listens, checks that all four patches were written and read back, then kills the game.

## Not tested, or not possible here

- Connection to a real server: our builds have no gateway address. The end-to-end test stops when the helper has confirmed the patches; the full app stops at *"This build has no server configured"*.
- Discord login: it needs the Azoth account server and an account on the access list.
- What the game does when `ping_group_range` blocks the user.
- UMU-Proton downloaded by umu (all runs used `PROTONPATH` with GE-Proton10-12).
- The AppImage (`appimagetool` was not installed); only the tarball was built.
- Other distributions, a build on Ubuntu 22.04, Flatpak Steam on a real system, KWallet without its Secret Service layer.
- The Windows launcher window on real Windows (the Windows build and its tests ran, the latter under Wine).

## Decisions

1. FIFO instead of stdin (stdin does not reach the exe through umu, see above).
2. The helper ships next to the launcher instead of `go:embed`.
3. umu-launcher is a host dependency.
4. AppImage with the host's WebKitGTK, plus the tarball.
