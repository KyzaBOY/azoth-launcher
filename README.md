# Azoth Launcher

The launcher for **Azoth**, a fan-run server for *New World*. It starts your own copy of the original game client against the Azoth server, signs you in with Discord and gets out of the way.

> **Azoth is a fan project. It is not affiliated with, endorsed by or connected to Amazon or Amazon Games.** *New World* is a trademark of Amazon.com, Inc. or its affiliates. You need your own copy of the game, and nothing from the game is included in this repository.

## What it does

1. **Finds your game.** You pick the New World folder (or press *Search*, which looks through your Steam libraries and your local drives).
2. **Checks the version.** Only one build is supported, `1.400.6031.6004151`. The launcher computes the sha256 of `NewWorld.exe` and refuses anything else.
3. **Plays from a copy.** Your install is never run and never modified. On first Play the launcher builds an isolated copy in `%LOCALAPPDATA%\AzothLauncher\client` (the `Bin64` folder plus a few small config files, about 270 MB; the large `assets` folder is an NTFS junction back to your install, so it takes no extra space). The copy is reused until the game files change.
4. **Signs you in.** *Log in with Discord* opens your browser on projectazoth.com. Play stays disabled until you are logged in.
5. **Starts the game suspended, patches it in memory, resumes it.** The four patches are listed below. The game does not execute a single instruction before every patch has been written and read back; if anything looks unexpected (a different build, bytes that are not the ones we expect), the process is killed instead of resumed.

The launcher has no log window, no server list and no news feed. The only feedback is one status line under the Play button.

## The four in-memory patches

All four are written into the running game process only. No file on disk is changed, and nothing stays hooked after the patch is applied.

| # | What it changes | Why |
|---|---|---|
| 1 | **DTLS trust.** One conditional jump in the game's secure-socket initialization becomes an unconditional jump. | The game validates the world server's certificate. The Azoth server uses its own self-signed certificate, so without this the connection is refused. |
| 2 | **Menu ping over ICMP.** One pointer in the region pinger's table is switched from the string `https` to the string `icmp`. | The menu pinger normally speaks HTTPS to an Amazon endpoint and would show 9999 ms. |
| 3 | **Ping host.** The place where the pinger reads its target host now reads a string the launcher wrote, which is the Azoth server's host. | So the menu ping measures the Azoth server instead of an Amazon host name. No `hosts` file edit is needed. |
| 4 | **Persona.** The place where the client reads its login name now reads a string the launcher wrote, which is your Discord ticket. | The Azoth server learns who you are from the ticket instead of your Windows user name. |

Patches 3 and 4 write their strings into unused space at the end of the game's `.rdata` section, and only after checking that the space is empty.

## What it does not do

- It does **not** modify your New World install or any game file.
- It does **not** edit the `hosts` file, install a proxy, load a DLL or install a driver, and it never asks for administrator rights. Everything lives under your own user profile.
- It does **not** connect you to Amazon's servers; it only starts the client pointed at the Azoth server.
- It does **not** download the game, and it does **not** work with other builds of it.
- It contains **no secrets**. A build contains the address of the server it was made for and the public address of the account site, nothing else. Who is allowed to play is decided by the server's access list; the launcher has no way to grant anyone access.

## Discord login: what is stored and where

The login follows the native-app pattern (RFC 8252) with PKCE:

1. The launcher listens on `127.0.0.1` on a random port, only while a login is open (at most five minutes), and opens your browser on `https://projectazoth.com`.
2. After you approve on Discord, the site sends the browser back to that loopback address with a single-use code. The code is only worth something together with a secret verifier that never left the launcher.
3. The launcher exchanges the code for a **session** (valid for 30 days).

What is kept on your PC, in `%LOCALAPPDATA%\AzothLauncher`:

| File | Content |
|---|---|
| `account.dat` | Your Discord id, your Discord name and the session token, encrypted with Windows **DPAPI** (only your Windows user, on this PC, can decrypt it). *Log out* deletes it. |
| `config.json` | One setting: `{"gameDir": "<the New World folder you picked>"}`. |
| `client\` | The isolated game copy described above. |

On every Play the launcher trades the session for a **ticket** (valid for 24 hours). The ticket goes straight into the game process as the persona. It is never written to disk and never logged or shown. If the server answers that your Discord account is not on the access list, you see a message and your session is left alone.

## Requirements

- Windows 10 or 11, 64-bit, with the WebView2 runtime (already part of Windows 11 and of current Edge installs).
- Your own copy of *New World* at build `1.400.6031.6004151` (sha256 of the unpatched `NewWorld.exe`: `8654f01d324636d9f74f1c793b0cc4a417c3c5fa9847d9913c358ca29e0fdc8e`).
- A Discord account that the server's access list accepts.

## Build

You need Go 1.26 or newer and the [Wails v2](https://wails.io) CLI:

```
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

**Dev build.** `wails build` produces `build\bin\AzothLauncher.exe`. The server address is deliberately not in the source, so a plain build starts and shows the screen, but Play refuses with *"This build has no server configured"*. To point a dev build at a server, pass it to the linker:

```
wails build -ldflags "-X main.gatewayHost=your.server.host -X main.gatewayPort=6000"
```

The account site defaults to `https://projectazoth.com`. For a local account server you can override it with `-X main.accountURL=http://localhost:8090` (plain `http` is only accepted for `localhost` and `127.0.0.1`).

**Release build for your own server.** Copy `release.env.example` to `release.env` (it is gitignored), set `GATEWAY_HOST` and `GATEWAY_PORT`, then run:

```
powershell -ExecutionPolicy Bypass -File scripts\release.ps1
```

It runs `wails build -clean` with your values baked in and writes `build\bin\AzothLauncher.exe.sha256` (`<sha256>  AzothLauncher.exe`) next to the executable.

**Tests.** `go test ./...` runs everything. If you set `AZOTH_CLIENT_BIN` to a `Bin64` folder of the supported build, one extra test also checks the patch sites against the real `NewWorld.exe`.

**The screen without Wails.** `frontend\index.html` also opens in a plain browser with sample data (`frontend\js\bridge.js`), which is handy for working on the look: `?account=1` starts logged in, `?screen=settings` opens the settings dialog, `?search=none|one|many` picks what Search finds.

## Layout

| Path | What is there |
|---|---|
| `main.go`, `app.go`, `flow.go` | Wails app, the methods the screen can call, and the Play pipeline. |
| `internal/client` | The game side: version check, isolated copy, search for installs, launching and the in-memory patches. |
| `internal/account` | Discord login (loopback + PKCE), ticket request, DPAPI session store. |
| `internal/config` | `config.json`. |
| `frontend/` | The screen: plain HTML, CSS and JavaScript, with its fonts. |
| `scripts/release.ps1` | Release build. |

## License

MIT, see [LICENSE](LICENSE). The bundled fonts (IM FELL DW Pica, Archivo, Cinzel Decorative) are under the SIL Open Font License; their license texts are in `frontend/fonts`.
