# EDSense

A DualSense feel for Elite Dangerous, through DSX. EDSense drives the adaptive triggers, the lightbar, the player LEDs, the mic LED and the haptics from what the game is doing, using the [DSX Mod System](https://github.com/Paliverse/DSX/tree/main/Mod%20System%20(DSX%20v3)).

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
| Galaxy map, panels, station menus, main menu | off | | |
| On foot, outside social spaces | R2 WEAPON, L2 light aim FEEDBACK | health colour | |
| SRV turret | R2 WEAPON | teal | |

When the game closes, goes to the main menu, or EDSense is closed, the controller goes back to your DSX profile.

### Haptics

EDSense drives the DualSense's haptics itself, from what happens in the game. It sends left and right rumble levels to DSX's virtual DualSense, which DSX turns into haptics on the controller ("Rumble to Haptics"): the left motor on the left side, the right one on the right.

- R2 fire is felt on the right and L2 fire on the left, thrust (R1) as a light rumble and boost (Circle) as a strong push. On foot, each R2 shot is a tap on the right.
- The FSD charging and jumping, the hyperspace tunnel, supercruise entry and drop, being interdicted and escaping, fuel scooping and overheating.
- Hardpoints, landing gear, cargo scoop, silent running, fire group, pips, a target lock, a limpet launch, docking granted, docking and undocking, touchdown and liftoff, an SRV or fighter launched or docked.
- Shields down and back up, hull hits, being attacked, a kill, a breached cockpit, heat warnings and heat damage, the discovery scanner, a jet cone boost, death.
- Another ship scanning you: a pulse on the left, then on the right.

Elite does not report firing, thrust or boost, so EDSense reads R2, L2, R1 and Circle from DSX's virtual DualSense.

## Setup

1. **DSX v3.1 or newer.** Tested with v3.2.0 BETA 02.
2. In DSX, **Settings -> Networking**: turn **Incoming UDP** on. EDSense reads DSX's port file.
3. Put the EDSense folder anywhere outside Program Files, so it can keep its settings next to the exe, and run `EDSense.exe`.
4. On first run it asks whether to **start with Windows**. It waits in the tray and switches on by itself whenever Elite runs.

In DSX, for the profile you use with Elite:

- Virtual Device: **DualSense Emulation**.
- **Haptics | Rumble -> Game Feedback** on (DSX turns EDSense's rumble into haptics).
- Recommended off: DSX's **Additional Effects** and **Additional Audio** (so they don't layer on top), and the profile's **Adaptive Triggers** (EDSense sets the triggers while you play).

### DSX Native Mode

In Native Mode DSX creates no virtual controller but still hides the physical DualSense with HidHide, so Elite sees no controller and your control preset is not loaded (`BindingLoadingErrors.log`: `Missing devices: DualShock4`). In **DSX -> HidHide -> Add Application**, add `EliteDangerous64.exe` (in `...\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\`).

If Elite does not map the DualSense to its "DualShock4" device, add the DualSense (`0CE6`) and DualSense Edge (`0DF2`) as `<Alternative>` lines under `<DualShock4>` in Elite's `ControlSchemes\DeviceMappings.xml` (game updates can overwrite it), or use DSX's DualShock 4 emulation.

### Tray icon

| Icon | Meaning |
|---|---|
| orange | active, driving the controller |
| grey | waiting for Elite, in the main menu, or paused |
| red | DSX is not answering: closed, or Incoming UDP is off |

The menu: **Pause effects**, **Play demo** (every effect once, without Elite running), **Start with Windows**, **Open settings** (changes apply when you save), **Open log**, **Quit**.

## Settings

`edsense.json` and `edsense.log` live next to the exe, or in `%APPDATA%\EDSense` when that folder is not writable. The file is created with every setting on first run.

- `control_lightbar`, `control_triggers`, `control_player_leds`, `control_mic_led`, `control_haptics`: `false` leaves that output to your DSX profile.
- `lightbar_brightness`: 0-255. `colors`: RGB for each state.
- `triggers`: mode and parameters for each situation, in DSX v3 modes: `OFF`; `FEEDBACK` [start 1-9, strength 1-8]; `WEAPON` [start 2-7, end 3-8, strength 1-8]; `VIBRATION` [start 1-9, amplitude 1-8, frequency 1-40]; `SLOPE_FEEDBACK` [start, end, start strength, end strength]; `MULTIPLE_POSITION_FEEDBACK` [10 x 0-8]; `MULTIPLE_POSITION_VIBRATION` [frequency, 10 x 0-8].
- `haptics_strength`: the master level.
- `haptics_gain`: each effect's level, 0 turns it off. Every effect is listed, for example `fire_primary` (R2 fire), `scanned`, `overheat`.
- `rumble`: each effect's rumble: left and right strength and, for one-shots, length.
- `journal_dir`: empty for the standard folder.
- `dsx_port`: `0` reads DSX's port file, falling back to 6969.
- `poll_ms`: how often the controller is updated.

Command-line options, from PowerShell or cmd:

- `-console`: run in the terminal instead of the tray
- `-demo`: play every effect once
- `-padtest`: find DSX's virtual DualSense, rumble each side, show its input
- `-verbose`: print every packet sent to DSX
- `-config <file>`, `-version`

## Limits

- **Trigger vibration is only felt while a trigger is pulled.** The trigger motors vibrate past the effect's start position; none of the vibration encodings vibrates with the finger just resting.
- **No per-shot data.** Elite reports no firing, throttle, speed or boost; they come from the controller.
- **No shield or heat %.** Shields are known only when they collapse or come back, and heat only once it passes 100%.
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
| `internal/elite` | Status.json, the journal, the journal folder |
| `internal/game` | the game as EDSense sees it: status, journal state |
| `internal/lights` | lightbar, triggers, player and mic LEDs for a game state |
| `internal/haptics` | the haptic effects, as rumble |
| `internal/dsx` | the DSX UDP client |
| `internal/dualsense` | DSX's virtual DualSense: input, rumble |
| `internal/config` | `edsense.json` |
| `internal/platform` | Windows processes, autostart, dialogs |
| `internal/demo`, `internal/diag` | the demo and the command-line checks |
| `internal/tray` | the notification area icon |
