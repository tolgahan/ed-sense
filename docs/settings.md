# Settings

Back to the [README](../README.md).

## edsense.json

`edsense.json` sits next to `EDSense.exe`, or in `%APPDATA%\EDSense` when that folder is not writable. EDSense creates it with every setting on first run. Open it with **Open settings** in the tray menu.

- Changes apply about 2 s after you save. `dsx_port`, `journal_dir`, `bindings_dir` and `poll_ms` are read only at start: restart EDSense after changing them.
- If the file has a JSON error, the change is not loaded and the log says why.
- Missing keys get their default. Out-of-range `haptics_strength`, `lightbar_brightness`, `poll_ms` and `spin_up_ms`, and unknown words in `turn_feel`, `jump_feel` and `haptics_mode`, are set back to a valid value. Trigger parameters and colours are used as written. Leave `config_version` alone.

| Key | Default | What it does |
|---|---|---|
| `turn_feel` | `"waves"` | `"waves"`, `"push"` or `"off"`, see [turns](feel.md#turns) |
| `jump_feel` | `"swell"` | `"swell"`, `"calm"` or `"off"`, see [jumps](feel.md#jumps) |
| `haptics_strength` | `1` | master level for the haptics, 0 to 3 |
| `haptics_gain` | `1` each | the level of each effect: 1 is normal, 0.5 is half, 0 turns it off. The file lists every effect |
| `haptics_mode` | `"auto"` | `"auto"` and `"native"`: native haptics when DSX's audio device works, else rumble. `"rumble"`: always rumble |
| `control_lightbar`, `control_triggers`, `control_player_leds`, `control_mic_led` | `true` | `false` leaves that part to your DSX profile |
| `control_haptics` | `true` | `false`: no haptics. EDSense then stops reading the controller too, so the trigger slack on an empty weapons capacitor stops as well |
| `fire_groups` | groups 1 and 2 on `"auto"` | the weapon feel per fire group, see below |
| `spin_up_ms` | small 250, medium 500, large 1500, huge 0 | how long multi-cannons spin up before they fire, by hardpoint size, in ms (0 to 5000). Estimates: if the rattle starts before or after your guns, change the size you fly |
| `gyro_aim` | `true` | `false` turns DSX's motion output off while Elite runs |
| `gyro_off_in_menus` | `true` | turns the gyro off in the main menu and the menus below |
| `gyro_off_gui_focus` | `[1,2,3,4,5,6,7,8,11]` | the menus with the gyro off: 1-4 side, comms and role panels, 5 station services, 6 galaxy map, 7 system map, 8 orrery, 9 FSS, 10 surface scanner, 11 codex. The main menu is always included |
| `hud_reader` | `true` | read the HUD from the screen, see [HUD reader](how-it-works.md#hud-reader) |
| `hud_colors` | `{}` | HUD colours by hand, see [HUD colours](how-it-works.md#hud-colours) |
| `hud_debug` | `false` | save HUD captures to `hud_debug\` |
| `lightbar_brightness` | `200` | 0 to 255 |
| `colors` | | `[red, green, blue]` for each lightbar state |
| `triggers` | | DSX trigger mode and parameters for each situation, see [triggers](#triggers) |
| `rumble` | | the one-shot effects of the rumble fallback: `left` and `right` strength (0 to 1) and `ms` |
| `journal_dir`, `bindings_dir` | `""` | empty for Elite's standard folders |
| `dsx_port` | `0` | 0 reads DSX's port file (6969 if there is none). Any other number is used as the port |
| `poll_ms` | `25` | how often the controller is updated, in ms (20 or more) |

### Fire groups

`fire_groups` sets the weapon feel per fire group (1 is the first): `primary` is R2, `secondary` is L2. Each is `"auto"` or one of `beam`, `pulse`, `burst`, `multicannon`, `cannon`, `fragment`, `railgun`, `plasma`, `missile`, `mining`, `generic`. Groups you don't list are `"auto"`.

```json
"fire_groups": {"1": {"primary": "multicannon", "secondary": "auto"}}
```

With `"auto"`, EDSense uses what the HUD showed for that fire group (hardpoints in and out count separately). Until it has seen that, or with the HUD reader off, it guesses from your loadout: the most common weapon type on R2, the next one on L2.

### Triggers

Each entry in `triggers` is a DSX v3 mode and its parameters, for example `"ship_weapons_r": {"mode": "WEAPON", "params": [2, 5, 6]}`.

| Key | When | Default |
|---|---|---|
| `ship_weapons_r`, `ship_weapons_l` | hardpoints out | `WEAPON [2,5,6]`, `WEAPON [2,5,4]` |
| `ship_reload_r`, `ship_reload_l` | every weapon on that trigger reloading (HUD) | `OFF` |
| `ship_wep_empty_r`, `ship_wep_empty_l` | firing with the weapons capacitor empty (HUD) | `OFF` |
| `ship_overheat_r`, `ship_overheat_l` | hardpoints out and overheating | `VIBRATION [2,6,12]` |
| `ship_scanner_r`, `ship_scanner_l` | analysis mode, hardpoints out | `FEEDBACK [2,3]` |
| `interdiction` | being interdicted, both triggers | `VIBRATION [1,8,30]` |
| `hit` | attacked, both triggers for 0.25 s | `VIBRATION [1,5,25]` |
| `jet_cone` | jet cone boost, both triggers for 1 s | `VIBRATION [1,8,8]` |
| `srv_turret_r`, `srv_turret_l` | SRV turret view | `WEAPON [2,5,5]`, `FEEDBACK [2,2]` |
| `onfoot_r`, `onfoot_l` | on foot, outside social spaces, stations and hangars | `WEAPON [2,6,5]`, `FEEDBACK [3,2]` |

The modes and their parameters:

- `OFF`: no resistance
- `FEEDBACK` [start 1-9, strength 1-8]
- `WEAPON` [start 2-7, end 3-8, strength 1-8]
- `VIBRATION` [start 1-9, amplitude 1-8, frequency 1-40]
- `SLOPE_FEEDBACK` [start 1-8, end 2-9, start strength 1-8, end strength 1-8]
- `MULTIPLE_POSITION_FEEDBACK` [10 values, 0-8]
- `MULTIPLE_POSITION_VIBRATION` [frequency 1-40, 10 values, 0-8]

An unknown mode counts as `OFF`.

## Command-line options

Run these from PowerShell or cmd in the EDSense folder, for example `.\EDSense.exe -feeltest`. They print to that window. To open PowerShell there, right-click an empty spot in the folder and choose **Open in Terminal** (Windows 11), or Shift+right-click and choose **Open PowerShell window here** (Windows 10).

| Option | What it does |
|---|---|
| `-feeltest` | plays four plain tones (80, 120, 170 and 250 Hz, 2 s each), then the turn feels `"waves"` and `"push"`, then the jump feels `"swell"` and `"calm"` (a 5 s countdown, then the tunnel). About a minute. Needs native haptics. Close Elite first: it does not run while Elite and the tray EDSense both run |
| `-demo` | plays every effect once, without Elite |
| `-padtest` | lists Sony controllers, finds DSX's virtual DualSense, rumbles left then right, then shows R2, L2, R1, Circle, gyro and touch for 15 s |
| `-hapticstest` | lists the audio outputs and plays 9 native haptics steps: left 60 Hz, right 150 Hz, multi-cannon, beam, boost, shields down, hull hit, hardpoints, docking clamps |
| `-hudtest <screenshots>` | reads the HUD from screenshots, see [checking with screenshots](how-it-works.md#checking-with-screenshots) |
| `-console` | runs in the console instead of the tray. Ctrl+C gives the controller back |
| `-verbose` | prints every packet sent to DSX |
| `-config <file>` | uses another settings file. Its folder then holds the log, `hud_palette.json`, `hud_debug` and the profile backups |
| `-version` | prints the version |

## DSX profile

EDSense brings a DSX controller profile called "Elite Dangerous": DualSense emulation with its audio device, gyro aim (motion to mouse, with passthrough), a touchpad setup, and DSX's own adaptive triggers off.

- If DSX has no "Elite Dangerous" profile, EDSense adds it. It also makes it Elite's game profile, if DSX has none for Elite yet.
- An existing "Elite Dangerous" profile is left alone, and so is a game profile you already set for Elite. So if DSX uses another profile for Elite, it keeps using it.
- **Reset DSX profile...** in the tray menu writes the bundled profile and makes it Elite's game profile. Your old "Elite Dangerous" profile is saved in `dsx_profile_backups`.
- DSX saves its profiles when it exits, so EDSense writes only while DSX is closed. It waits for you to close DSX, then says "Start DSX again to use it".

If you keep your own profile, check these in DSX:

- Virtual Device: **DualSense Emulation**. EDSense plays the haptics and reads the triggers, sticks and gyro through it.
- **Haptics | Rumble -> Game Feedback** on.
- **Advanced -> Audio Routing and Volume -> Haptic Motors** on.
- Motion: **Passthrough** on, for the gyro part of the turn feel.
- Best off: **Additional Effects**, **Additional Audio**, and the profile's **Adaptive Triggers** (EDSense sets the triggers while you play).

## DSX Native Mode

EDSense can still set the triggers and lights in Native Mode. The haptics, and everything EDSense reads from the controller (firing, boost, turns), need DSX's virtual DualSense, which Native Mode does not create.

Native Mode still hides the DualSense with HidHide, so Elite sees no controller and your bindings don't load (`BindingLoadingErrors.log`: `Missing devices: DualShock4`). Add `EliteDangerous64.exe` in **DSX -> HidHide -> Add Application**. It is in `...\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\`.

If Elite does not map the DualSense to its "DualShock4" device, use DSX's DualShock 4 emulation. Or add the DualSense (`0CE6`) and DualSense Edge (`0DF2`) as `<Alternative>` lines under `<DualShock4>` in Elite's `ControlSchemes\DeviceMappings.xml`. Game updates can overwrite that file.

## When something does not work

Open the log (tray -> **Open log**). It says what EDSense found and what is missing.

- **Red tray icon**: DSX is closed, or **Settings -> Networking -> Incoming UDP** is off.
- **No haptics**, and the log says `no virtual DualSense audio device`: set the DSX profile's virtual device to **DualSense Emulation**, with **Haptic Motors** on. `.\EDSense.exe -padtest` and `.\EDSense.exe -hapticstest` check the connection.
- **Gyro turns not felt**, and the log says `no motion data`: turn on **Passthrough** on DSX's Motion page.
- **No shield or heat effects, or turns felt in the blue zone**: run Elite borderless or windowed. `-hudtest` shows what EDSense reads. For a recoloured HUD, see [HUD colours](how-it-works.md#hud-colours).
- **Heat sink, chaff, shield cell or boost not felt**, and the log says `is a built-in preset`: EDSense needs a custom control preset. Change any binding in Elite once and Elite saves one.
- **A trigger feels like the wrong weapon**: set it per fire group with [fire_groups](#fire-groups).
- **A feel is too much**: `"turn_feel": "push"` or `"off"`, `"jump_feel": "off"`, or lower `maneuver`, `hyperspace` or `fsd_charge` in `haptics_gain`.
- **Journal folder not found**: set `journal_dir` to your journal folder and restart EDSense.
- **Elite sees no controller**: see [DSX Native Mode](#dsx-native-mode).
