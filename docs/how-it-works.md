# How it works

Back to the [README](../README.md).

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
- **Controller**: DSX's virtual DualSense, for its input, rumble and haptics audio device.
- **Processes**: the process list, to see whether Elite and DSX run, and the exe path of Elite, DSX and the window in front, through a query-only handle. Steam's library list (registry and `libraryfolders.vdf`) to find DSX and Elite.
- **DSX**: its port file, and its profile files when adding the EDSense profile.
- **Network**: UDP to DSX on `127.0.0.1` only. Nothing is sent anywhere else.

It writes:

- Next to the exe, or in `%APPDATA%\EDSense`: `edsense.json`, `edsense.log`, `hud_palette.json`, `gyro_calibration.json` (the gyro's drift), `hud_debug\` (only with `hud_debug` on) and `dsx_profile_backups\`.
- In DSX's folder, only while DSX is closed: the "Elite Dangerous" controller profile and Elite's game profile entry.
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

Go 1.23 or newer.

```
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -X main.version=1.0.0" -o EDSense.exe ./cmd/edsense
```

The HUD tests read recorded gameplay that is not in the repository. Without it they are skipped, see `tools/hud/README.md`.

`cmd/edsense/rsrc_windows_amd64.syso` holds the exe's icon, manifest and version info. After changing them or `assets/winres.json`, make it again:

```
go run github.com/tc-hib/go-winres@v0.3.3 make --in assets/winres.json --out cmd/edsense/rsrc --arch amd64
```

The committed file says version `dev`; release builds set the version from the tag.

Releases are built by GitHub Actions. Pushing a `v*` tag runs the tests, builds `EDSense.exe`, zips it, and attaches the zip and `SHA256SUMS.txt` to a draft release, with a build provenance attestation.

### Code layout

| Package | |
|---|---|
| `cmd/edsense` | flags, logging, start-up |
| `internal/app` | the main loop: follows the game, drives DSX and the virtual DualSense, the HUD reader, the gyro switching |
| `internal/elite` | Status.json, the journal, Loadout modules, game folders |
| `internal/game` | the game as EDSense sees it |
| `internal/lights` | lightbar, triggers, player and mic LEDs |
| `internal/haptics` | the effects, the synthesizer, turn and jump feels, the rumble fallback |
| `internal/gyro` | gyro aim: the drift calibration and the motion to mouse |
| `internal/hud`, `internal/hud/vision` | the HUD reader and its image processing |
| `internal/dsx` | the DSX UDP client and the bundled DSX profile |
| `internal/dualsense` | DSX's virtual DualSense: input, rumble, the haptics audio device |
| `internal/bindings` | Elite's control bindings |
| `internal/config` | `edsense.json` |
| `internal/platform` | Windows processes, keyboard, dialogs |
| `internal/demo`, `internal/diag` | the demo and the command-line checks |
| `internal/tray` | the tray icon |
| `tools/hud` | Python scripts that build the HUD glyph templates |

## Credits

The gyro's drift calibration follows ideas from [GamepadMotionHelpers](https://github.com/JibbSmart/GamepadMotionHelpers) and [JoyShockMapper](https://github.com/Electronicks/JoyShockMapper) by Julian "Jibb" Smart and contributors (MIT license). DSX's motion to mouse was matched from its behaviour.
