# EDSense

A DualSense feel for Elite Dangerous, through DSX. EDSense drives the adaptive triggers, the lightbar, the player LEDs, the mic LED and the haptic actuators from what the game is doing, using the [DSX Mod System](https://github.com/Paliverse/DSX/tree/main/Mod%20System%20(DSX%20v3)).

It only **reads** what Elite writes for third-party tools (the journal and `Status.json`, the files EDMC and EDDI use). It does not touch the game process, its memory or its files.

## What you feel

| Situation | Triggers (R2 / L2) | Lightbar | Player LEDs / Mic LED |
|---|---|---|---|
| Normal space | off | hull colour: green -> amber -> red | fire group 1-5 |
| Hardpoints deployed | WEAPON click on both (R2 primary, L2 secondary) | | |
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
  - R2 fire is felt on the right and L2 fire on the left, with a texture that matches the weapons on that trigger (guessed from the loadout, or set per fire group in `fire_groups`). A railgun charges while held and cracks on release; multi-cannons whir while their barrels spin up, then rattle once they fire; beam lasers hum, missiles thump.
  - Turning the ship, with gyro aim or with a stick bound to yaw, pitch or roll, is felt for as long as the turn lasts, stronger the harder you turn, as a faint, even hum. A sudden flick of the controller adds a soft push. Gyro aim turns the ship only while the controller moves, so a gyro turn is felt while you move it and a stick turn while you hold the stick. The gyro part is silent while a finger rests on the touchpad (DSX's "motion off while touching") and with gyro aim off.
  - Another ship scanning you: a scan line sweeps once from the left grip to the right. With the shields down, a low rattle.
  - Heat: a slow throb that speeds up as the ship heats, and "boiling" above 100%.
  - Heat sink, chaff and shield cell each have their own feel, from your Elite bindings (see below).
  - Flight assist off and on, FSD ready, mass lock, hitting the atmosphere and gliding, the ground rushing up when dropping fast near it, cargo scooped and ejected, a message from a player or your wing (NPC chatter is ignored).
  - Thargoids: a slow throb while their music plays. Their shutdown field turns the lights off and the triggers slack; after about 30 s the systems come back on.
- **Rumble fallback.** Without the audio device, EDSense sends left and right rumble levels instead, which DSX turns into haptics ("Rumble to Haptics").

Elite does not report firing, thrust, boost or turning, so EDSense reads R2, L2, R1, Circle, the sticks and the gyro from DSX's virtual DualSense.

Heat sink, chaff, shield cell and boost are matched against your **Elite bindings**, read from `%LOCALAPPDATA%\Frontier Developments\Elite Dangerous\Options\Bindings`: controller buttons, combinations with modifier buttons, and keyboard keys (read only while Elite is in front, and only the keys bound to these actions). Frontier's built-in presets are not read; this needs a custom preset, which Elite creates as soon as you change a binding. Without it, boost is Circle.

### Gyro aim on or off

EDSense assumes you aim with the controller's gyro, as the bundled DSX profile does. To fly with the sticks only, untick **Gyro aim** in the tray menu (or set `gyro_aim` to `false`): while Elite runs, DSX's motion output is off, and the turn feel follows the sticks. When Elite closes, your DSX profile's gyro setting comes back.

### Gyro off in menus

In the main menu, the side panels, station services, the maps and the codex, EDSense switches DSX's motion output off, so the mouse cursor stays still and you can use the d-pad. The FSS and surface scanner keep the gyro. Change the list with `gyro_off_gui_focus`, or turn it off with `gyro_off_in_menus`. The pause (Esc) menu can't be detected: Elite writes nothing for it.

This uses DSX's "ToMode" instruction (type 8: `[controller, 2 = motion, 7 = disabled]`), which the public Mod System docs don't list.

## Setup

1. **DSX v3.1 or newer.** Tested with v3.2.0 BETA 02.
2. In DSX, **Settings -> Networking**: turn **Incoming UDP** on. EDSense reads DSX's port file.
3. Put the EDSense folder anywhere outside Program Files, so it can keep its settings next to the exe, and run `EDSense.exe`.
4. On first run it asks whether to **start with Windows**. It waits in the tray and switches on by itself whenever Elite runs.

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

The menu: **Pause effects**, **Play demo** (every effect once, without Elite running), **Gyro aim**, **Start with Windows**, **Open settings** (changes apply when you save), **Open log**, **Reset DSX profile...**, **Quit**.

## Settings

`edsense.json` and `edsense.log` live next to the exe, or in `%APPDATA%\EDSense` when that folder is not writable. The file is created with every setting on first run.

- `control_lightbar`, `control_triggers`, `control_player_leds`, `control_mic_led`, `control_haptics`: `false` leaves that output to your DSX profile.
- `lightbar_brightness`: 0-255. `colors`: RGB for each state.
- `triggers`: mode and parameters for each situation, in DSX v3 modes: `OFF`; `FEEDBACK` [start 1-9, strength 1-8]; `WEAPON` [start 2-7, end 3-8, strength 1-8]; `VIBRATION` [start 1-9, amplitude 1-8, frequency 1-40]; `SLOPE_FEEDBACK` [start, end, start strength, end strength]; `MULTIPLE_POSITION_FEEDBACK` [10 x 0-8]; `MULTIPLE_POSITION_VIBRATION` [frequency, 10 x 0-8].
- `haptics_mode`: `auto` (native, else rumble), `native` or `rumble`. `haptics_strength`: the master level.
- `haptics_gain`: each effect's level, 0 turns it off. Every effect is listed, for example `maneuver` (the turn feel), `maneuver_kick`, `scanned`, `shields_offline`, `heat_build`, `overheat`, `spin_up` (multi-cannons spinning up).
- `spin_up_ms`: how long multi-cannons spin up before they fire, by hardpoint size (`small` 250, `medium` 500, `large` 1500, `huge` 0). The game doesn't show it, so these are estimates: if the rattle starts before or after your guns do, change the size you fly.
- `rumble`: the one-shot effects of the rumble fallback: left and right strength and length.
- `gyro_aim` (`true`): `false` turns DSX's motion output off while Elite runs.
- `gyro_off_in_menus` (`true`) and `gyro_off_gui_focus` (`[1,2,3,4,5,6,7,8,11]`): the menus with the gyro off, as Status.json GuiFocus values: 1-4 panels, 5 station services, 6 galaxy map, 7 system map, 8 orrery, 9 FSS, 10 surface scanner, 11 codex. The main menu is always included.
- `fire_groups`: the weapon feel per fire group (1-based) for `primary` (R2) and `secondary` (L2): `auto` guesses from the loadout (the most common weapon type on R2, the next on L2), or one of `beam`, `pulse`, `burst`, `multicannon`, `cannon`, `fragment`, `railgun`, `plasma`, `missile`, `mining`, `generic`.
- `journal_dir`, `bindings_dir`: empty for the standard folders.
- `dsx_port`: `0` reads DSX's port file, falling back to 6969.
- `poll_ms`: how often the controller is updated.

Command-line options, from PowerShell or cmd:

- `-console`: run in the terminal instead of the tray
- `-demo`: play every effect once
- `-padtest`: find DSX's virtual DualSense, rumble each side, show its input
- `-hapticstest`: play native haptics effect by effect
- `-verbose`: print every packet sent to DSX
- `-config <file>`, `-version`

## Limits

- **Trigger vibration is only felt while a trigger is pulled.** The trigger motors vibrate past the effect's start position; none of the vibration encodings vibrates with the finger just resting.
- **No per-shot data.** Elite reports no firing, throttle, speed or boost; they come from the controller.
- **No shield or heat %.** Shields are known only when they collapse or come back, and heat is estimated from weapons fired, silent running and fuel scooping.
- **No fire group contents.** The journal lists the modules but not the fire groups, so the weapon feel of each trigger is a guess from the loadout unless set in `fire_groups`.
- **Coarse hull data** from the journal: about 20% steps, only when damaged.
- **Latency**: `Status.json` changes when something changes, about 0.25-1 s for state effects.

## Development

Go 1.23 or newer.

```
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=1.0.0" -o EDSense.exe ./cmd/edsense
```

`cmd/edsense/rsrc_windows_amd64.syso` holds the exe's icon and manifest. After changing `assets/edsense.manifest` or the icon, regenerate it with [rsrc](https://github.com/akavel/rsrc):

```
rsrc -arch amd64 -manifest assets/edsense.manifest -ico assets/icons/active.ico -o cmd/edsense/rsrc_windows_amd64.syso
```

### Code layout

| Package | |
|---|---|
| `cmd/edsense` | flags, logging, start-up |
| `internal/app` | the main loop: follows the game, drives DSX and the virtual DualSense |
| `internal/elite` | Status.json, the journal, Loadout modules, game folders |
| `internal/game` | the game as EDSense sees it: status, journal state, fire groups |
| `internal/lights` | lightbar, triggers, player and mic LEDs for a game state |
| `internal/haptics` | the haptic effects, the synthesizer, the rumble fallback |
| `internal/dsx` | the DSX UDP client and the bundled DSX profile |
| `internal/dualsense` | DSX's virtual DualSense: input, rumble, the haptics audio device |
| `internal/bindings` | Elite's control bindings and detecting bound actions |
| `internal/config` | `edsense.json` |
| `internal/platform` | Windows processes, keyboard, autostart, dialogs |
| `internal/demo`, `internal/diag` | the demo and the command-line checks |
| `internal/tray` | the notification area icon |
