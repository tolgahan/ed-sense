# EDSense

[![CI](https://github.com/tolgahan/ed-sense/actions/workflows/ci.yml/badge.svg)](https://github.com/tolgahan/ed-sense/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/tolgahan/ed-sense)](https://github.com/tolgahan/ed-sense/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A DualSense feel for Elite Dangerous, through DSX. EDSense drives the adaptive triggers, the lightbar, the player LEDs, the mic LED and the haptic actuators from what the game is doing, using the [DSX Mod System](https://github.com/Paliverse/DSX/tree/main/Mod%20System%20(DSX%20v3)).

It only **reads** what Elite writes for third-party tools (the journal and `Status.json`, the files EDMC and EDDI use) and, optionally, small parts of the screen for the HUD (see [Reading the HUD](#reading-the-hud)). It does not touch the game process, its memory or its files.

## What you feel

| Situation | Triggers (R2 / L2) | Lightbar | Player LEDs / Mic LED |
|---|---|---|---|
| Normal space | off | hull colour: green -> amber -> red | fire group 1-5 |
| Hardpoints deployed | WEAPON click on both (R2 primary, L2 secondary) | | |
| Firing with the weapons capacitor empty (HUD) | slack (OFF) until it is back to 30% | | |
| Every weapon on a trigger reloading (HUD) | that trigger slack (OFF) until a clip is back | | |
| Analysis mode, hardpoints out (scanners) | light FEEDBACK | | |
| Weapons overheating | VIBRATION | orange blink | |
| Hit by enemy fire | short buzz | red flash | |
| Kill / bounty | | green flash | |
| Combat (combat music or "in danger") | | breathing | |
| Shields down | | red blink | |
| FSD charging | rising VIBRATION | white, rising | |
| Hyperspace jump | | blue-white pulse | 18 s countdown, 5 -> 1 LEDs |
| Supercruise | | blue | |
| Being interdicted | strong VIBRATION | magenta pulse | |
| Fuel scooping | | amber breathing | |
| Low fuel | | | mic LED pulses |
| Silent running | | off | mic LED on |
| Docked / landed | off | dim | |
| Galaxy map, panels, station menus, main menu | off | | gyro off (see below) |
| On foot, outside social spaces | R2 WEAPON, L2 light aim FEEDBACK | health colour | |
| SRV turret | R2 WEAPON | teal | |

When the game closes, goes to the main menu, or EDSense is closed, the controller goes back to your DSX profile.

### Haptics

EDSense drives the DualSense's haptic actuators itself, from what happens in the game.

- **Native haptics.** EDSense writes its own waveforms to the haptic channels of DSX's virtual DualSense audio device, and DSX passes them to the controller. Every effect has its own frequency, texture and envelope, and the left and right actuators are independent:
  - R2 fire is felt on the right and L2 fire on the left, with a texture that matches the weapons the fire group puts on that trigger (read from the HUD, see below). A railgun charges while held and cracks on release; multi-cannons whir while their barrels spin up, then rattle once they fire; beam lasers hum, missiles thump.
  - Turning the ship, with gyro aim or with a stick bound to yaw, pitch or roll, is felt for as long as the turn lasts, stronger the harder you turn, as a faint, even hum. In the throttle's blue zone, where the ship turns best, turns are not felt. A sudden flick of the controller adds a soft push outside the blue zone. Gyro aim turns the ship only while the controller moves, so a gyro turn is felt while you move it and a stick turn while you hold the stick. The gyro part is silent while a finger rests on the touchpad (DSX's "motion off while touching") and with gyro aim off.
  - Another ship scanning you: a scan line sweeps once from the left grip to the right. With the shields down, a low rattle.
  - Heat: a slow throb that speeds up as the ship heats, and "boiling" above 100%.
  - Heat sink, chaff and shield cell each have their own feel, from your Elite bindings (see below).
  - Flight assist off and on, FSD ready, mass lock, hitting the atmosphere and gliding, the ground rushing up when dropping fast near it, cargo scooped and ejected, a message from a player or your wing (NPC chatter is ignored).
  - Thargoids: a slow throb while their music plays. Their shutdown field turns the lights off and the triggers slack; after about 30 s the systems come back on.
- **Rumble fallback.** Without the audio device, EDSense sends left and right rumble levels instead, which DSX turns into haptics ("Rumble to Haptics").

Elite does not report firing, thrust, boost or turning, so EDSense reads R2, L2, R1, Circle, the sticks and the gyro from DSX's virtual DualSense.

Heat sink, chaff, shield cell and boost are matched against your **Elite bindings**, read from `%LOCALAPPDATA%\Frontier Developments\Elite Dangerous\Options\Bindings`: controller buttons, combinations with modifier buttons, and keyboard keys (read only while Elite is in front, and only the keys bound to these actions and the keys your preset uses as modifiers). Frontier's built-in presets are not read; this needs a custom preset, which Elite creates as soon as you change a binding. Without it, boost is Circle.

### Reading the HUD

Elite does not write the shield % or the heat % for tools; the cockpit HUD shows them. EDSense can read them from the screen while you fly. It is optional: `hud_reader: false` turns it off, and everything else keeps working from the journal.

What it reads:

- **Shield %**, under your ship's hologram. Hits flash on the hologram: you feel each one, weighted to the side that was hit, and sustained fire as a sizzle. Below 40% the shields crackle; refilling, they tick.
- **Hull %**, below left of the shield %. Each drop is a heavy, metallic blow; below 30% the hull creaks.
- **Heat %**, next to the flame at the top left of the radar. The heat throb follows it, with a thud at 60, 70, 80, 90 and 100%.
- **The target's shield and hull %**: while you fire, each hit that lands is a small tick on the side of the trigger you fire with. When its shields collapse you feel them shatter.
- **The throttle's blue zone**, right of the radar: turns are felt only outside it.
- **Capacitors**: firing with the weapons capacitor empty, the triggers go slack until it refills. Boosting without enough engine capacitor is a hollow dud; EDSense learns how much your ship needs from what happens after each press.
- **Fire groups**: the PRIMARY list (right strut) is what R2 fires and the SECONDARY list (left strut) is what L2 fires, for every fire group, hardpoints out or in. Entries are matched against your ship's modules (from the journal's Loadout) by their width, the mount icon and the ammo line. The log says what it found: `Fire group 1, hardpoints out: R2 (primary) fires BEAM LASER x2 (read from the HUD)`. A trigger with only utilities gets their feel: heat sink, chaff, shield cell, ECM, limpets on a press, a scanner hum while held.
- **Reloads**: when a weapon's ammo line reads RELOADING, you feel the clip drop out on that side, that weapon falls silent, and when every weapon on the trigger reloads the trigger goes slack. A heavy thunk when the clip seats.

#### Screen capture

- It looks at the screen the way screenshot and recording tools do, with Windows' standard screen capture (GDI `BitBlt`). It does not open the game process, read or write its memory, inject anything, draw over the game or send it input.
- It captures only small parts of the Elite window, and only while Elite is the window in front and you are in the cockpit: not in the main menu, on the maps, panels or station screens, docked or in hyperspace. Other windows and the rest of the screen are not captured.
- Captures are read in memory and dropped; EDSense's only network traffic is to DSX on `127.0.0.1`.
- With `hud_debug: true` it saves every capture to the `hud_debug` folder, named after what it read in it.
- Once it has found the numbers, it captures small windows around them, 10 times a second in a fight and about 3 times a second otherwise. The log shows the measured cost after 300 reads (`HUD: 300 reads ... about N% of one CPU core`).

The game has to run **borderless or windowed**; exclusive fullscreen can't be captured.

#### How reliable it is

The numbers are found by colour and shape, straightened and read on every frame, so resolution, field of view, ship and the cockpit swaying don't affect them.

The reader is tested against hand-labelled 4K captures from real flights: the shield % is read right in 144 of 146, the heat % in 131 of 145. The fire group lists find the heat sinks in all 82 captures and the multi-cannons in 77 (in the other 5 they are out of range or out of view), and the same captures scaled down to 1440p and 1080p read almost the same. A big jump is believed only when several frames agree. A module stays on a fire group list through reads that miss it (out of range, reloading, out of view) until it has been missing for a minute, and each fire group's lists are remembered for when you select it again. When nothing can be read, the effects fall back to what the journal says. When nothing can be read, the effects fall back to what the journal says.

#### HUD colours

Recoloured HUDs are handled in this order:

1. **Colour matrix presets** (`GraphicsConfigurationOverride.xml`, including the online HUD editors' presets): EDSense reads your matrix when it starts and when Elite starts, and works out the colours from it.
2. **Anything else** (EDHM themes, ReShade, filters): once per session, a few seconds after you start flying with shields up, EDSense checks the colours on screen, and again whenever the shield % has not been readable for 30 s. It looks for the shield % with the hull % below left of it in another colour. After two matching finds it switches to them and saves them in `hud_palette.json` (`HUD: using the colours found on screen`); a new colour matrix means learning again.
3. **By hand**: `"hud_colors": {"shield": "#29c8cf", "heat": "#ba6c16"}` in `edsense.json`. Keys: `shield`, `heat`, `hull` (the HUD's main colour: hull %, capacitors, fire group lists), `flame` (the heat icon), `flash` (hit flashes on the hologram, or `"off"`). These override the learned colours, and learning stops.

When the hit-flash colour is too close to the shield colour, flash detection is off and hits are felt from the shield % dropping.

To check with screenshots (Elite's F10 screenshots are BMP files in `Pictures\Frontier Developments\Elite Dangerous`):

```
EDSense.exe -hudtest Screenshot_0001.bmp
```

It reads the screenshot with the colours EDSense would use, matching the fire group lists against the ship in your newest journal, then searches for the colours and reads again with what it found, printing `hud_colors` ready to paste.

### Gyro aim on or off

EDSense assumes you aim with the controller's gyro, as the bundled DSX profile does. To fly with the sticks only, untick **Gyro aim** in the tray menu (or set `gyro_aim` to `false`): while Elite runs, DSX's motion output is off, and the turn feel follows the sticks. When Elite closes, your DSX profile's gyro setting comes back.

### Gyro off in menus

In the main menu, the side panels, station services, the maps and the codex, EDSense switches DSX's motion output off, so the mouse cursor stays still and you can use the d-pad. The FSS and surface scanner keep the gyro. Change the list with `gyro_off_gui_focus`, or turn it off with `gyro_off_in_menus`. The pause (Esc) menu can't be detected: Elite writes nothing for it.

This uses DSX's "ToMode" instruction (type 8: `[controller, 2 = motion, 7 = disabled]`), which the public Mod System docs don't list.

## Setup

1. **DSX v3.1 or newer.** Tested with v3.2.0 BETA 02.
2. In DSX, **Settings -> Networking**: turn **Incoming UDP** on. EDSense reads DSX's port file.
3. Download the zip from the [latest release](https://github.com/tolgahan/ed-sense/releases/latest), put the EDSense folder anywhere outside Program Files, so it can keep its settings next to the exe, and run `EDSense.exe`. The exe is not code-signed yet, so Windows SmartScreen may warn on the first start: **More info -> Run anyway**.
4. It waits in the tray and switches on by itself whenever Elite runs. To start it with Windows, put a shortcut to `EDSense.exe` in your Startup folder (Win+R, `shell:startup`).

In DSX, for the profile you use with Elite:

- Virtual Device: **DualSense Emulation**.
- **Haptics | Rumble -> Game Feedback** on.
- **Advanced -> Audio Routing and Volume -> Haptic Motors** on.
- Recommended off: DSX's **Additional Effects** and **Additional Audio** (so they don't layer on top), and the profile's **Adaptive Triggers** (EDSense sets the triggers while you play).

### DSX profile

EDSense comes with a DSX controller profile for Elite (gyro aim, touchpad and trigger setup). If DSX has no "Elite Dangerous" profile, EDSense adds it and makes it Elite's game profile. An existing one is left alone; **Reset DSX profile...** in the tray menu replaces it, keeping yours in `dsx_profile_backups`.

DSX keeps its profiles in memory and saves them when it exits, so the profile is only written while DSX is closed: EDSense waits until you close DSX (tray icon -> Exit), then writes it. Start DSX again to use it.

### DSX Native Mode

In Native Mode DSX creates no virtual controller but still hides the physical DualSense with HidHide, so Elite sees no controller and your control preset is not loaded (`BindingLoadingErrors.log`: `Missing devices: DualShock4`). In **DSX -> HidHide -> Add Application**, add `EliteDangerous64.exe` (in `...\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\`).

If Elite does not map the DualSense to its "DualShock4" device, add the DualSense (`0CE6`) and DualSense Edge (`0DF2`) as `<Alternative>` lines under `<DualShock4>` in Elite's `ControlSchemes\DeviceMappings.xml` (game updates can overwrite it), or use DSX's DualShock 4 emulation.

### Tray icon

| Icon | Meaning |
|---|---|
| orange | active, driving the controller |
| grey | waiting for Elite, in the main menu, or paused |
| red | DSX is not answering: closed, or Incoming UDP is off |

The menu: **Pause effects**, **Play demo** (every effect once, without Elite running), **Gyro aim**, **Open settings** (changes apply when you save), **Open log**, **Reset DSX profile...**, **Quit**.

## What EDSense reads and writes

EDSense runs as your user and needs no admin rights. What it reads:

- **Files**: the journal and `Status.json` (`Saved Games\Frontier Developments\Elite Dangerous`), your control preset (`Options\Bindings`), the HUD colour matrix (`Options\Graphics\GraphicsConfigurationOverride.xml`) and DSX's port file.
- **Keyboard**: with `GetAsyncKeyState`, whether the keys bound to heat sink, chaff, shield cell and boost, and the keys your preset uses as modifiers, are held. Only while Elite's window is in front; nothing is stored or sent ([process_windows.go](internal/platform/process_windows.go), [detector.go](internal/bindings/detector.go)).
- **Screen**: small parts of the Elite window, with GDI `BitBlt`, only while Elite is in front and you are in the cockpit. Captures are read in memory and dropped, and saved only with `hud_debug` on ([capture_windows.go](internal/hud/capture_windows.go), [Screen capture](#screen-capture)).
- **Controller**: DSX's virtual DualSense, for its input, rumble and the haptics audio device ([internal/dualsense](internal/dualsense)).
- **Processes**: the process list, to see whether Elite and DSX run, and the exe path of the window in front and of Elite and DSX, through a query-only handle. Steam's library list (registry and `libraryfolders.vdf`) is read to find DSX and Elite.
- **Network**: UDP to DSX on `127.0.0.1` only ([client.go](internal/dsx/client.go)). Nothing is sent anywhere else.

What it writes:

- Next to the exe, or in `%APPDATA%\EDSense` when that folder is not writable: `edsense.json`, `edsense.log`, `hud_palette.json` (the HUD colours found on screen), `hud_debug\` if `hud_debug` is on, and `dsx_profile_backups\`.
- In DSX's folder, only while DSX is closed: the "Elite Dangerous" controller profile and Elite's entry in DSX's game profiles, when DSX has no such profile or you choose **Reset DSX profile...** (the old profile is kept in `dsx_profile_backups`).

## Uninstall

1. In the tray menu, click **Quit**.
2. If you start it with Windows, delete its shortcut from the Startup folder (`shell:startup`). Versions up to 0.4.2 could add themselves to startup: turn that off in **Task Manager -> Startup apps**.
3. Delete the EDSense folder, and `%APPDATA%\EDSense` if it exists.
4. If you no longer want it, delete the "Elite Dangerous" controller profile in DSX.

## Settings

`edsense.json` and `edsense.log` live next to the exe, or in `%APPDATA%\EDSense` when that folder is not writable. The file is created with every setting on first run.

- `control_lightbar`, `control_triggers`, `control_player_leds`, `control_mic_led`, `control_haptics`: `false` leaves that output to your DSX profile.
- `lightbar_brightness`: 0-255. `colors`: RGB for each state.
- `triggers`: mode and parameters for each situation, in DSX v3 modes: `OFF`; `FEEDBACK` [start 1-9, strength 1-8]; `WEAPON` [start 2-7, end 3-8, strength 1-8]; `VIBRATION` [start 1-9, amplitude 1-8, frequency 1-40]; `SLOPE_FEEDBACK` [start, end, start strength, end strength]; `MULTIPLE_POSITION_FEEDBACK` [10 x 0-8]; `MULTIPLE_POSITION_VIBRATION` [frequency, 10 x 0-8]. `ship_wep_empty_r/l` and `ship_reload_r/l` are `OFF`: the trigger goes slack.
- `haptics_mode`: `auto` (native, else rumble), `native` or `rumble`. `haptics_strength`: the master level.
- `haptics_gain`: each effect's level, 0 turns it off. Every effect is listed, for example `maneuver` (the turn feel), `maneuver_kick`, `scanned`, `shields_offline`, `heat_build`, `overheat`, `hull_hit_hud`, `hull_creak`, `target_hit`, `boost_empty`, `reload`, `reload_done`, `spin_up` (multi-cannons spinning up).
- `spin_up_ms`: how long multi-cannons spin up before they fire, by hardpoint size (`small` 250, `medium` 500, `large` 1500, `huge` 0). The game doesn't show it, so these are estimates: if the rattle starts before or after your guns do, change the size you fly.
- `rumble`: the one-shot effects of the rumble fallback: left and right strength and length.
- `gyro_aim` (`true`): `false` turns DSX's motion output off while Elite runs.
- `gyro_off_in_menus` (`true`) and `gyro_off_gui_focus` (`[1,2,3,4,5,6,7,8,11]`): the menus with the gyro off, as Status.json GuiFocus values: 1-4 panels, 5 station services, 6 galaxy map, 7 system map, 8 orrery, 9 FSS, 10 surface scanner, 11 codex. The main menu is always included.
- `fire_groups`: the weapon feel per fire group (1-based) for `primary` (R2) and `secondary` (L2): `auto` reads the HUD's lists (until a fire group has been seen there, or with `hud_reader` off, it is guessed from the loadout: the most common weapon type on R2, the next on L2), or one of `beam`, `pulse`, `burst`, `multicannon`, `cannon`, `fragment`, `railgun`, `plasma`, `missile`, `mining`, `generic`.
- `hud_reader` (`true`), `hud_colors` (`{}`), `hud_debug` (`false`: saves the captures and what was read to `hud_debug`, the newest 600 files).
- `journal_dir`, `bindings_dir`: empty for the standard folders.
- `dsx_port`: `0` reads DSX's port file, falling back to 6969.
- `poll_ms`: how often the controller is updated.

Command-line options, from PowerShell or cmd:

- `-console`: run in the terminal instead of the tray
- `-demo`: play every effect once
- `-padtest`: find DSX's virtual DualSense, rumble each side, show its input
- `-hapticstest`: play native haptics effect by effect
- `-hudtest <screenshots>`: see above
- `-verbose`: print every packet sent to DSX
- `-config <file>`, `-version`

## Limits

- **Trigger vibration is only felt while a trigger is pulled.** The trigger motors vibrate past the effect's start position; none of the vibration encodings vibrates with the finger just resting.
- **No per-shot data.** Elite reports no firing, throttle, speed or boost; they come from the controller.
- **Shield and heat % only from the HUD.** Without it, shields are known only when they collapse or come back, and heat is estimated from weapons fired, silent running and fuel scooping.
- **Fire groups only from the HUD.** The journal lists the modules but not the fire groups. Special versions with names EDSense doesn't know are left out.
- **Coarse hull data** from the journal: about 20% steps, only when damaged.
- **Latency**: `Status.json` changes when something changes, about 0.25-1 s for state effects.

## Development

Go 1.23 or newer.

```
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -X main.version=1.0.0" -o EDSense.exe ./cmd/edsense
```

`cmd/edsense/rsrc_windows_amd64.syso` holds the exe's icon, manifest and version info, made from `assets/winres.json` with [go-winres](https://github.com/tc-hib/go-winres). After changing the manifest, the icon or `winres.json`, regenerate it:

```
go run github.com/tc-hib/go-winres@v0.3.3 make --in assets/winres.json --out cmd/edsense/rsrc --arch amd64
```

The committed file says version `dev`; release builds set the version from the tag.

The HUD tests read recorded gameplay that is not in the repository; without it they are skipped (see `tools/hud/README.md`).

Releases are built by GitHub Actions: pushing a `v*` tag builds `EDSense.exe`, zips it and attaches the zip and `SHA256SUMS.txt` to a draft release for that tag, with a build provenance attestation.

### Code layout

| Package | |
|---|---|
| `cmd/edsense` | flags, logging, start-up |
| `internal/app` | the main loop: follows the game, drives DSX and the virtual DualSense |
| `internal/elite` | Status.json, the journal, Loadout modules, game folders |
| `internal/game` | the game as EDSense sees it: status, journal state, fire groups, the HUD's latest reading |
| `internal/lights` | lightbar, triggers, player and mic LEDs for a game state |
| `internal/haptics` | the haptic effects, the synthesizer, the rumble fallback |
| `internal/hud` | the HUD reader: numbers, fire group lists, colours, the background watcher |
| `internal/hud/vision` | image processing under it: colour matching, components, text lines |
| `internal/dsx` | the DSX UDP client and the bundled DSX profile |
| `internal/dualsense` | DSX's virtual DualSense: input, rumble, the haptics audio device |
| `internal/bindings` | Elite's control bindings and detecting bound actions |
| `internal/config` | `edsense.json` |
| `internal/platform` | Windows processes, keyboard, dialogs |
| `internal/demo`, `internal/diag` | the demo and the command-line checks |
| `internal/tray` | the notification area icon |

## Disclaimer

EDSense is not affiliated with or endorsed by Frontier Developments, Paliverse (DSX) or Sony. Elite Dangerous is a trademark of Frontier Developments plc; DualSense is a trademark of Sony Interactive Entertainment.
