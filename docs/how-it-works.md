# How it works

Back to the [README](../README.md).

## Backends

EDSense reaches the controller through a backend: the controller app set by `backend` (or `-backend`), which the window's first run, its Controller page and the tray's **Controller app** change while EDSense runs:

- **DSX**: the triggers and lights as UDP packets to DSX's Mod System; the input, rumble, gyro and native haptics through DSX's virtual DualSense. EDSense switches DSX's own gyro mouse off while its own gyro aims. DSX forgets what it was told a minute after EDSense stops.
- **DS4Windows** 5: the same packets to its game mod listener, in the stricter form it takes (every value in range, no motion-page instruction). The input and rumble come through its virtual DualSense under usbip-win2, the gyro's motion from its UDP server while that sends (else from the virtual DualSense, which drops slow turns), the native haptics through that pad or the controller's own audio device. DS4Windows keeps what it was told until EDSense hands the controller back, so EDSense hands it back whenever DS4Windows comes online, when an output is switched off, and when the loop crashes. Its gyro cannot be switched from outside, so EDSense's gyro aims only while the DS4Windows profile leaves the gyro alone. See [DS4Windows](ds4windows.md).
- **Auto** picks the one that runs; with both, the one that answers a status request; with neither, DS4Windows if it answers on its port, else DSX. It looks again every 3 s while EDSense is not driving the controller, and switches by itself when another app surely runs. A guess (neither app runs, or both run and neither answers) never moves it off the app it uses.

## Switching the controller app

A choice in the first run, the Controller page or the tray, **Apply now**, and Auto all switch the same way, one at a time:

1. EDSense opens a connection to the new app. If that fails, nothing changes.
2. The loop stops as it does when you quit: it hands the controller back to your profile, saves the gyro's drift and silences the haptics. A playing demo stops. While the gyro calibrates, a choice or **Apply now** waits up to 3 s for it, then gives up and changes nothing.
3. It closes the old app's connection, its virtual DualSense and the haptics audio device, and takes the new ones.
4. The loop starts again on the new app. It keeps what it knows of the game, so the journal is not read again (unless `journal_dir` or `bindings_dir` changed). The new app gets a hand-back first, then everything EDSense sets.
5. A choice is saved as `backend` in `edsense.json` only now, once the switch worked. A choice or **Apply now** is refused, with nothing switched, while `edsense.json` has an error.

It takes about a second, with the controller on your profile meanwhile. The log says `Controller app: switching to DSX (chosen in the window)`, then `Controller app: DSX (set in edsense.json)`, and the window's Activity says `Now using DSX`. **Apply now** with the same app but a new `poll_ms`, `journal_dir` or `bindings_dir` only restarts the loop and logs `Settings applied: poll_ms`.

## The window

The window runs as a second process, `EDSense.exe --window`, which EDSense starts when you start it (not with `-tray`) and when you click the tray icon. So while the window is open, Task Manager shows two EDSense.exe, and under the second one the window's own `msedgewebview2.exe` processes. The first EDSense.exe is the one you started: it runs the effects, the tray, the settings and the log, and it never loads WebView2. The second only shows the window. The two talk through the window's standard input and output, one line of JSON per message. The window process ends when the window closes, when EDSense quits, or when the first one goes away. The window closing or crashing never stops the effects, and the effects never wait for the window: the loop leaves its status where the window can take it without a lock, only while the window can be seen, and the window gets it at most 4 times a second.

It is a [Wails](https://github.com/wailsapp/wails) v3 app showing a page through Microsoft Edge WebView2, built in Wails' production mode:

- The page (HTML, CSS and JavaScript modules) is inside the exe and served from `http://wails.localhost`. Nothing is fetched from anywhere else.
- Every response carries a strict Content Security Policy: no inline script, no `eval`, trusted types on, connections only back to the window process, no frames, no forms. Anything from the game (names of commanders, ships and stations) is shown as text only.
- Every WebView2 permission is denied: camera, microphone, location, notifications, clipboard, downloads, file access, MIDI and the rest. DevTools and the browser's context menu are off.
- The page has no links. It can ask the window process for a fixed list of actions only (the status, pause, the demo, calibrating the gyro, reading the settings and changing them by name, choosing the controller app, **Apply now**, the setup checks, installing or resetting EDSense's DSX or DS4Windows profile and cancelling a wait, opening `edsense.json`, the log or a folder of copies by its name, opening a link by its name from a fixed list, quitting), checked in the window process and again in the tray one. Each setting the page changes is checked against its type and range before it is written. File paths and web addresses never come from the page.
- The window process starts with an environment cleaned of the variables that could redirect WebView2 or Wails (`WAILS_*`, `WEBVIEW2_*`, `COREWEBVIEW2_*`, `FRONTEND_DEVSERVER_URL`). It and its WebView2 processes run in a job object: what is left of them ends a few seconds after the window closes, before it opens again, and when EDSense quits.
- The window process loads Windows' own DLLs from System32 only, never from EDSense's folder.
- When EDSense runs as administrator, a link opens through the desktop, so your browser does not run as administrator too.
- About checks the exe's signature with Windows, offline (no revocation check, no download).

WebView2 keeps its cache and settings for the window in `%LOCALAPPDATA%\EDSense\WebView2` (`WebView2-admin` when EDSense runs as administrator, so the two never share it). Nothing of yours is in there; it is safe to delete while EDSense is closed. The window's size, position, last page and theme are saved in `ui_state.json` next to `edsense.json`. It is light or dark as Windows is, unless **Advanced -> Appearance** says Light or Dark. Its text follows Windows' text size (**Settings -> Accessibility -> Text size**).

Before it starts the window, EDSense looks for the WebView2 Runtime in the registry, the way WebView2's loader does. Without it, EDSense offers Microsoft's download page (it never downloads anything itself) and the tray works as before, see [What you need](setup.md#what-you-need).

### The first run

When EDSense has just created `edsense.json` (so `backend` is still `""`), the window shows a first run in place of its pages: Welcome, your controller app (the app EDSense detects is picked, and each one says whether it runs), the checks of that app with its profile card, gyro aim, and Ready with **Play demo** and **Finish**. **Install...** on the profile card writes EDSense's profile for Elite in that app when you confirm it; DSX's profile is not added by itself before the first run is over. Nothing is written to `edsense.json` before **Finish**, which saves `gyro_aim` (and `gyro_by` with DSX), then switches to the app you picked and saves it as `backend`. **Set up later** saves `"auto"` (started with `-backend`, that run keeps its app). Either way the first run does not come back. A settings file from an earlier version gets `"auto"` and no first run. Started with `-tray`, EDSense shows the first run when you open the window. A choice in the tray meanwhile ends it.

### The Controller page

- **Controller app**: Auto, DSX or DS4Windows. A choice applies at once, as in the tray, and the page asks first while EDSense drives the controller. The arrow keys only move between the apps; Space, Enter or a click chooses one. While Auto is sure of no app (neither runs, both run and neither answers, or a DS4Windows before 5), the page says why. With `-backend`, the page says so; a choice replaces it.
- **Connection**: where EDSense sends, and who answers there and elsewhere. The port of the app in use is typed here (0 for automatic). A new port, and `journal_dir`, `bindings_dir` or `poll_ms` changed in `edsense.json`, wait for **Apply now**.
- **Setup**: the checks of the app in use (while Auto is sure of no app, of the apps that run), asked again every 2 s while the page is shown: whether it runs and answers, the controller, the virtual DualSense, with DS4Windows the profile in use (DualSense emulation, gyro, touchpad, Trigger Lab) and where the gyro's motion comes from, and with DSX its "Elite Dangerous" profile. Each row says what to do under **How**, and a row EDSense's own profile would fix has **Let EDSense fix it**, which goes to the profile card. Checks that need EDSense on that app wait until it is.
- **DS4Windows profile** and **DSX profile**: what EDSense finds of its own profile for Elite in that app, with **Install...**, **Reset...**, **Cancel** while it waits for the app to close, and **Open backups**. Each asks first, and says what it writes and where. The DS4Windows card also shows the HidHide check, which stays yours to set up. See [The install service](#the-install-service).
- With DS4Windows, where native haptics play (`ds4windows_haptics`).
- While `edsense.json` has an error, the page shows it with its line and column, and changes no setting.

## The install service

EDSense's profiles for Elite are written by an install service in the EDSense you started: never in the window process, and never on the loop that drives the controller. It starts once that EDSense is the one that runs, so a second start never writes. It has one job per app. While a job waits for its app to close, it looks every second; otherwise it sleeps, apart from a look every 5 seconds at which DS4Windows runs, which tells it that DS4Windows' settings folder (kept in memory only). A job lives in memory too: a wait ends when EDSense quits, and nothing is written at the next start.

When it writes:

- **DSX**: when you press **Install...** or **Reset...** on the DSX profile card (or the tray's **Reset DSX profile...** while the window cannot open), and once by itself: when DSX has no "Elite Dangerous" profile, 3 seconds after EDSense starts using DSX, once the first run is over (or when there is none: no WebView2 Runtime, or `-console`) and DSX is chosen or surely runs (never while Auto only falls back to DSX). While DSX's folder is not found, it looks again every 3 seconds. This happens once per start of EDSense, not after you pressed **Install...** or **Cancel** on the card, and it never replaces a profile. A DSX found only while it runs (outside the Steam libraries) is written into the folder it was found in once it is closed.
- **DS4Windows**: only when you press **Install...** or **Reset...** on the DS4Windows profile card. What it writes is in [Let EDSense set up DS4Windows](ds4windows.md#let-edsense-set-up-ds4windows).

How it waits: DSX and DS4Windows save their settings when they exit, so EDSense writes only while the app is closed. The questions carry what they ask (the folder and the files): an answer to a question the card no longer asks writes nothing. While a write runs, the card says so and has no **Cancel**. DSX is closed when no `DSX.exe` runs. DS4Windows is closed when no `DS4Windows.exe` runs (nor the exe named in `custom_exe_name.txt` next to it), no DS4Windows window is open, and DS4Windows' single-instance event is gone. EDSense opens that event only for a moment to see whether it is there, closes it at once and never sets it, so a DS4Windows that starts meanwhile starts as usual. DS4Windows must stay closed for 2 seconds, so its own exit save has ended, and before each file EDSense checks again that it is closed and that the file is still the one it read and copied.

How it writes:

- Copies first: DSX's profile and `GameProfilesUpdates.json` into `dsx_profile_backups`, DS4Windows' `Profiles.xml`, `Auto Profiles.xml` and EDSense's profile there into a folder of `ds4windows_backups`, both next to `edsense.json`. Each copy is flushed to the disk before its file is replaced. When a copy fails, the file it is for is not written (with DS4Windows, nothing is). Copies are never deleted.
- Each file is written to a temporary file in the same folder and renamed over the old one.
- In DS4Windows' files EDSense changes only what it adds, at the places its XML reader found; the rest of each file, its line endings and its byte order mark stay as they were. Each file is read back and checked the way DS4Windows reads it, exact names and values; one that does not pass is put back from the copy, and nothing after it is written; the files written before it stay, and the card names them. A DS4Windows install writes only the steps its question listed: when DS4Windows' files changed while it waited and it would now write another one, it writes nothing.
- DSX's `GameProfilesUpdates.json` keeps its byte order mark, and its numbers are written back exactly as they were.

The log says what was asked and what was done: `DSX profile requested`, `DSX profile reset requested`, `DS4Windows profile install requested, in <folder>`, `DSX profile: waiting for DSX to be closed`, `DS4Windows profile: waiting for DS4Windows to be closed`, `DSX profile: cancelled`, then what was written or why not. When a write is done, EDSense says so in the window, or in a box while the window is closed.

## HUD reader

Elite does not write the shield % or the heat % for tools. The cockpit HUD shows them, and EDSense can read them from the screen. It is optional: `"hud_reader": false` turns it off, and the effects then use what the journal says.

What it reads:

- **Shield %** under your ship's hologram, and the hit flashes on the hologram (which side was hit).
- **Hull %**, below left of the shield %.
- **Capacitors** SYS, ENG and WEP.
- **Heat %**, next to the flame at the top left of the radar.
- **The throttle's blue zone**, right of the radar.
- **The target panel**: the target's shield and hull %, and hit flashes.
- **Fire group lists**: PRIMARY (right strut) is what R2 fires, SECONDARY (left strut) is what L2 fires. Entries are matched to your ship's modules from the journal by their width, mount icon and ammo line. RELOADING on an ammo line means that weapon reloads.

The log says what it found, for example `Fire group 1, hardpoints out: R2 (primary) fires BEAM LASER x2 (read from the HUD)`.

### When it captures

- Only while EDSense is active, Elite is the window in front and you are in your ship or fighter: not docked, not in hyperspace, no panel or map open. It also reads in supercruise and when landed on a planet. Nothing is read on foot or in the SRV.
- The fire group lists only in your main ship, outside analysis mode, and with hardpoints out, "in danger", or a trigger pulled in the last 10 s.
- 10 reads a second with hardpoints out, in danger, within 20 s of a shield hit, with the shields down or below 100%, or with heat at 50% or more. About 3 a second otherwise. Once it has found the numbers, it captures only small windows around them.
- It uses Windows' standard screen capture (GDI `BitBlt`) on parts of the Elite window. It does not open the game process for this, read or write its memory, inject anything, draw over the game or send it input.
- Captures are read in memory and dropped. With `"hud_debug": true` it saves them as PNG files in `hud_debug\`, named after what was read (the newest 600 files).
- Elite has to run **borderless or windowed**. Exclusive fullscreen can't be captured.
- The log shows the cost after 300 reads, for example `about N% of one CPU core`.

### How reliable it is

The numbers are found by colour and shape and read on every frame, so resolution, field of view, ship and cockpit sway don't matter. A small drop or a one-step rise is taken at once. Any other change is believed only when several reads agree.

On hand-labelled 4K captures from real flights, about 99% of shield reads and 90% of heat reads are right. The fire group lists found the heat sinks in all 82 captures and the multi-cannons in 77 (in the other 5 they were out of range or out of view). The same captures at 1440p and 1080p read about the same. The details are in `tools/hud/README.md` in the source.

A module stays on a fire group list through missed reads (out of range, reloading, out of view) until it has been missing for a minute while the list is in view. Each fire group's lists, with hardpoints in and out kept apart, are remembered for when you select it again. Another ship or a refit starts over. When nothing can be read, the effects fall back to the journal.

### HUD colours

Recoloured HUDs are handled in this order (a higher one wins):

1. `hud_colors` in `edsense.json`, for example `"hud_colors": {"shield": "#29c8cf", "heat": "#ba6c16"}`. Keys: `shield`, `heat`, `hull` (the HUD's main colour), `flame` (the heat icon), `flash` (hit flashes, or `"off"`).
2. Colours learned from your screen, saved in `hud_palette.json`. For EDHM themes, ReShade and filters.
3. Your colour matrix in `GraphicsConfigurationOverride.xml`, read when EDSense starts and when Elite starts.
4. Elite's standard HUD colours.

Learning: a few seconds after you start flying with shields up, EDSense looks for the shield % with the hull % below left of it in another colour. It looks again whenever the shield % has not been readable for 30 s. After two matching finds it uses and saves them (`HUD: using the colours found on screen`). A new colour matrix means learning again. Setting `shield` or `heat` in `hud_colors` stops the learning.

When the hit-flash colour is too close to the shield colour, flash detection is off and hits are felt from the shield % dropping.

### Checking with screenshots

Elite's F10 screenshots are BMP files in `Pictures\Frontier Developments\Elite Dangerous`. In PowerShell, in the EDSense folder, give `-hudtest` the full path of one or more screenshots:

```
.\EDSense.exe -hudtest "C:\Users\<you>\Pictures\Frontier Developments\Elite Dangerous\Screenshot_0001.bmp"
```

It reads the screenshot with the colours EDSense would use and matches the fire group lists against the ship in your newest journal. Then it searches the screenshot for the colours and reads again. If the colours differ, it prints a `hud_colors` line to paste into `edsense.json`.

## What EDSense reads and writes

EDSense runs as your user and needs no admin rights.

It reads:

- **Elite's files**: the journal and `Status.json` (`Saved Games\Frontier Developments\Elite Dangerous`), your control preset (`Options\Bindings`) and the HUD colour matrix (`Options\Graphics\GraphicsConfigurationOverride.xml`).
- **Mouse**: with EDSense's gyro, relative mouse movement through `SendInput`, only while Elite is in front and you are not in a menu. `-gyrotest` also reads the mouse through Raw Input while it runs.
- **Keys**: with `GetAsyncKeyState`, whether the keys bound to heat sink, chaff, shield cell, boost, mouse reset and head look, and your modifier keys, are held. Only while Elite's window is in front. Nothing is stored or sent.
- **Screen**: small parts of the Elite window, see [HUD reader](#hud-reader).
- **Controller**: the virtual DualSense of DSX or DS4Windows, for its input, rumble and haptics audio device. With DS4Windows and a USB cable, the controller's own audio device for the haptics.
- **Processes**: the process list, to see whether Elite, DSX and DS4Windows run, and the exe path of Elite, DSX, DS4Windows and the window in front, through a query-only handle. Steam's library list (registry and `libraryfolders.vdf`) to find DSX and Elite. While a DS4Windows profile waits to be written, DS4Windows' single-instance event, opened for a moment and closed at once, see [The install service](#the-install-service).
- **DSX**: its port file, and its profile files, to show and add the EDSense profile.
- **DS4Windows**: its `Profiles.xml` (with its UDP server's address, port and smoothing), `Auto Profiles.xml`, `LinkedProfiles.xml`, `custom_exe_name.txt` and profile files, and the profile the controller uses, asked from its window the way its own command line asks (a window message and a small shared memory block).
- **Network**: UDP to DSX or DS4Windows on `127.0.0.1` (or `::1`) only. Nothing is sent anywhere else.
- **The window**: the registry keys of the WebView2 Runtime, to see that it is installed, and the exe's own signature for About.

It writes:

- Next to the exe, or in `%APPDATA%\EDSense`: `edsense.json`, `edsense.log`, `hud_palette.json`, `gyro_calibration.json` (the gyro's drift; `gyro_calibration_ds4windows.json` and `gyro_calibration_ds4windows_udp.json` with DS4Windows), `ui_state.json` (the window's placement and theme), `hud_debug\` (only with `hud_debug` on), `dsx_profile_backups\` and `ds4windows_backups\` (the copies of the DSX and DS4Windows files EDSense changes). `edsense.json` is written to `edsense.json.tmp` first and then renamed over it.
- In `%LOCALAPPDATA%\EDSense\WebView2`: WebView2's cache for the window, see [The window](#the-window).
- In DSX's folder, only while DSX is closed: the "Elite Dangerous" controller profile and Elite's game profile entry.
- In DS4Windows' settings folder, only when you press **Install...** or **Reset...** on the DS4Windows profile card, and only while DS4Windows is closed: the profile `Profiles\Elite Dangerous (EDSense).xml`, a rule for Elite in `Auto Profiles.xml` and `UseDSXUDPServer` and `UseUDPServer` in `Profiles.xml`, see [Let EDSense set up DS4Windows](ds4windows.md#let-edsense-set-up-ds4windows).
- Nothing in Elite's folders.

Your bindings need a custom preset (Elite makes one as soon as you change a binding). Frontier's built-in presets are not read. Without a custom preset, boost is Circle, the left stick turns the ship, and the mouse is taken to spring back (decay on).

## Limits

- Elite reports no firing, thrust, boost or turning. They come from the controller.
- Shield and heat % come only from the HUD. Without it, shields are known only when they collapse or come back, and heat is estimated from weapons fired, silent running and fuel scooping.
- Fire groups come only from the HUD, because the journal lists modules but not fire groups. Special module versions with names EDSense does not know are left out.
- Hull from the journal is coarse: about 20% steps, only when damaged.
- Elite updates `Status.json` only when something changes, so state effects can lag a little.
- The turn feel needs native haptics and, for the blue zone, the HUD reader. Gyro turns are an estimate of Elite's mouse stick.

## Building from source

Go 1.25 or newer.

```
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags production -trimpath -ldflags "-H windowsgui -X main.version=1.0.0" -o EDSense.exe ./cmd/edsense
```

Without `-tags production` everything builds, but the window refuses to open: Wails' development mode has DevTools on and follows a development server.

The HUD tests read recorded gameplay that is not in the repository. Without it they are skipped, see `tools/hud/README.md`.

`cmd/edsense/rsrc_windows_amd64.syso` holds the exe's icon, manifest and version info. After changing them or `assets/winres.json`, make it again:

```
go run github.com/tc-hib/go-winres@v0.3.3 make --in assets/winres.json --out cmd/edsense/rsrc --arch amd64
```

The committed file says version `dev`; release builds set the version from the tag.

Releases are built by GitHub Actions. Pushing a `v*` tag runs the tests and builds `EDSense.exe` on Linux. A Windows job signs it with the Certum certificate; the key stays in Certum's cloud, and the job runs only after the maintainer approves it. Back on Linux, `.github/scripts/same_build.py` checks that the signed exe is the built exe plus the signature and nothing else, byte for byte. The zip and `SHA256SUMS.txt` are then published as the release for the tag, with a build provenance attestation for the signed exe and the zip.

### Checking a download

**Signature.** Right-click `EDSense.exe` -> **Properties** -> **Digital Signatures**, pick the signature, then **Details**: it should say "This digital signature is OK." In PowerShell: `Get-AuthenticodeSignature .\EDSense.exe | Format-List Status, SignerCertificate, TimeStamperCertificate`. Status should be `Valid`, the thumbprint `4A627A06211737A89181F4C0614AE5D29950F59C`, and the timestamp from Certum. The timestamp keeps the signature valid after the certificate expires. The zip itself is not signed.

**Checksums.** `SHA256SUMS.txt` has the SHA-256 of the zip and of `EDSense/EDSense.exe`. Compare with `Get-FileHash .\EDSense.exe` (SHA-256 is the default).

**Provenance.** The attestation shows that GitHub Actions in this repository built the file, and names the ref and commit of the run: the release tag when the release was made by pushing it. It needs the GitHub CLI and a GitHub login (`gh auth login`): `gh attestation verify EDSense.exe --repo tolgahan/ed-sense`, and the same for the zip.

**SmartScreen.** A new certificate has no reputation yet, so SmartScreen may warn on new releases until enough people have downloaded them, which can take weeks. Under **More info** SmartScreen should show a verified publisher; "Unknown publisher" means the file is not the released one.

### Code layout

| Package | |
|---|---|
| `cmd/edsense` | flags, logging, start-up |
| `internal/app` | the main loop: follows the game, drives the backend and the virtual DualSense, the HUD reader, the gyro switching |
| `internal/backend` | the backends (DSX, DS4Windows), what each can do and says, picking one |
| `internal/engine` | the controller app in use: choosing, switching, Apply now and Auto |
| `internal/install` | the setup checks the window's Controller page shows, and the install service that writes EDSense's DSX and DS4Windows profiles |
| `internal/elite` | Status.json, the journal, Loadout modules, game folders |
| `internal/game` | the game as EDSense sees it |
| `internal/lights` | lightbar, triggers, player and mic LEDs |
| `internal/haptics` | the effects, the synthesizer, turn and jump feels, the rumble fallback |
| `internal/gyro` | gyro aim: the drift calibration and the motion to mouse |
| `internal/hud`, `internal/hud/vision` | the HUD reader and its image processing |
| `internal/dsx` | the DSX UDP client, its DS4Windows dialect, and the bundled DSX profile |
| `internal/ds4w` | DS4Windows' settings and profile files, asking it for the profile in use, and writing EDSense's profile, rule, game mod and UDP server settings |
| `internal/dsu` | DS4Windows' UDP motion server (DSU, the cemuhook protocol): the client EDSense's gyro reads |
| `internal/dualsense` | the virtual DualSense of DSX and DS4Windows: input, rumble, the haptics audio device |
| `internal/bindings` | Elite's control bindings |
| `internal/config` | `edsense.json` |
| `internal/platform` | Windows processes, keyboard, dialogs |
| `internal/demo`, `internal/diag` | the demo and the command-line checks |
| `internal/tray` | the tray icon |
| `internal/control` | the protocol between EDSense and its window, and the list of what the window may ask |
| `internal/ui/launch` | starting and serving the window process |
| `internal/ui/window` | the window process (Wails), its security headers and the signature check |
| `internal/ui/web` | the window's page |
| `tools/hud` | Python scripts that build the HUD glyph templates |

## Credits

The gyro's drift calibration follows ideas from [GamepadMotionHelpers](https://github.com/JibbSmart/GamepadMotionHelpers) and [JoyShockMapper](https://github.com/Electronicks/JoyShockMapper) by Julian "Jibb" Smart and contributors (MIT license). DSX's motion to mouse was matched from its behaviour.

The window runs on [Wails](https://github.com/wailsapp/wails) (MIT license). `THIRD_PARTY_NOTICES.txt` has the licenses of everything built into the exe.
