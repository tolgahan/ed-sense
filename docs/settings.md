# Settings

Back to the [README](../README.md).

## edsense.json

`edsense.json` sits next to `EDSense.exe`, or in `%APPDATA%\EDSense` when that folder is not writable. EDSense creates it with every setting the first time it starts. Open it with **Open settings** in the tray menu, or **Open edsense.json** on the window's **Advanced** page.

The window's pages change the settings too. **Controller** has the controller app, the port of the app in use, and `ds4windows_haptics` while DS4Windows is in use. **Feel**, **Triggers**, **Lights**, **Gyro aim**, **HUD reader** and **Advanced** have the rest, see [Changing settings](using.md#changing-settings). Changes there apply at once, except `journal_dir`, `bindings_dir`, `poll_ms` and the two ports, which wait for **Apply now** (on the Controller or Advanced page).

- Changes apply about 2 s after you save, and the log and the window's Activity say "Settings reloaded". The tray menu's switches and the window's settings apply at once, and the log names them instead (for example "Gyro aim off", or "Settings changed in the window: ds4windows_haptics"). The ports, the folders and `poll_ms` wait for **Apply now**, also when the window changed them.
- `backend`, `ds4windows_port`, `dsx_port`, `journal_dir`, `bindings_dir` and `poll_ms` apply when EDSense reconnects: with **Apply now** on the window's Controller or Advanced page, with a choice of controller app in the tray or the window (it applies them all), or at the next start. Until then the log says, for example, `poll_ms: changed; they apply with Apply now in the EDSense window, or at the next start`, and the Controller and Advanced pages show them next to **Apply now**.
- If the file has a JSON error, EDSense keeps the settings it had (the defaults, when it starts with the error) and does not write the file. The log says why, and the window shows the error with its line and column, and **Open edsense.json**. The tray's switches and the window's settings are refused until the file is fixed.
- A file saved as UTF-8 with a byte order mark, as some editors save it, is read as usual. A file saved as UTF-16 is not: the error says to save it as UTF-8.
- A file from a newer EDSense (a higher `config_version`) is read and left as it is. EDSense writes it, with the keys it knows, only when a setting changes.
- Missing keys get their default. Out-of-range `haptics_strength`, `lightbar_brightness`, `spin_up_ms` and `ds4windows_port`, and `gyro_sensitivity_x`, `gyro_sensitivity_y` and `gyro_roll_mix`, and unknown words in `backend`, `turn_feel`, `jump_feel`, `haptics_mode`, `ds4windows_haptics`, `gyro_by` and `gyro_low_speed`, are set back to a valid value. A `poll_ms` below 20 is raised to 20. Trigger parameters and colours are used as written (with DS4Windows, brought into its range when sent). The window shows a value its control does not take as the nearest one, with a line that says what the file has, and leaves it in the file until you change that setting. Leave `config_version` alone.
- When EDSense writes the file (the tray's switches, the window, or a file from an older version), keys it does not know are dropped and the keys come in its own order.

| Key | Default | What it does |
|---|---|---|
| `turn_feel` | `"waves"` | `"waves"`, `"push"` or `"off"`, see [turns](feel.md#turns) |
| `jump_feel` | `"swell"` | `"swell"`, `"calm"` or `"off"`, see [jumps](feel.md#jumps) |
| `haptics_strength` | `1` | master level for the haptics, 0 to 3 |
| `haptics_gain` | `1` each | the level of each effect: 1 is normal, 0.5 is half, 0 turns it off. The file lists every effect |
| `haptics_mode` | `"auto"` | `"auto"` and `"native"`: native haptics when their audio device works, else rumble (with DS4Windows, else no haptics: rumble through DS4Windows mutes the native haptics, see [DS4Windows](ds4windows.md#native-haptics-and-rumble)). `"rumble"`: always rumble. `"native"` works as `"auto"`, and the window shows it as **Auto** |
| `control_lightbar`, `control_triggers`, `control_player_leds`, `control_mic_led` | `true` | `false` leaves that part to your DSX or DS4Windows profile |
| `control_haptics` | `true` | `false`: no haptics. EDSense then stops reading the controller too, so the trigger slack on an empty weapons capacitor stops as well |
| `fire_groups` | groups 1 and 2 on `"auto"` | the weapon feel per fire group, see below |
| `spin_up_ms` | small 250, medium 500, large 1500, huge 0 | how long multi-cannons spin up before they fire, by hardpoint size, in ms (0 to 5000). Estimates: if the rattle starts before or after your guns, change the size you fly |
| `gyro_aim` | `true` | `false`: no gyro aim while Elite runs |
| `gyro_by` | `"edsense"` | `"edsense"`: EDSense turns the controller's motion into mouse movement, and DSX's motion to mouse is off while Elite runs. `"dsx"`: DSX does it, as before. EDSense takes over only when the DSX profile for Elite has its gyro on motion to mouse. With DS4Windows, EDSense aims only while the DS4Windows profile leaves the gyro alone, and `"dsx"` only turns EDSense's gyro off: DS4Windows' own gyro follows its profile, see [DS4Windows](ds4windows.md#the-gyro). Same as the tray's **EDSense gyro** |
| `gyro_sensitivity_x`, `gyro_sensitivity_y` | `1` | how far the mouse moves sideways and up and down. 1 matches DSX's bundled profile (22.5 mouse counts per degree), 2 is twice as far. `-gyrotest` prints the values that match your DSX profile |
| `gyro_roll_mix` | `0.6` | how much rolling the controller turns sideways, as DSX does. 0 ignores roll |
| `gyro_low_speed` | `"dsx"` | `"dsx"`: very slow movement moves nothing and slow movement a little less, as with DSX. `"exact"`: every bit of rotation moves the mouse. With DS4Windows, slow movement reaches EDSense only through DS4Windows' UDP server, see [DS4Windows](ds4windows.md#the-gyro) |
| `gyro_auto_calibrate` | `true` | learn the gyro's drift whenever the controller lies still |
| `gyro_off_in_menus` | `true` | turns the gyro off in the main menu and the menus below |
| `gyro_off_gui_focus` | `[1,2,3,4,5,6,7,8,11]` | the menus with the gyro off: 1-4 side, comms and role panels, 5 station services, 6 galaxy map, 7 system map, 8 orrery, 9 FSS, 10 surface scanner, 11 codex. The main menu is always included |
| `hud_reader` | `true` | read the HUD from the screen, see [HUD reader](how-it-works.md#hud-reader) |
| `hud_colors` | `{}` | HUD colours by hand, also under **HUD reader -> HUD colours** in the window, see [HUD colours](how-it-works.md#hud-colours) |
| `hud_debug` | `false` | save HUD captures to `hud_debug\`. Also **Save HUD captures** on the window's HUD reader page, which asks first |
| `lightbar_brightness` | `200` | 0 to 255 |
| `colors` | | `[red, green, blue]` for each lightbar state |
| `triggers` | | DSX trigger mode and parameters for each situation, see [triggers](#triggers) |
| `rumble` | | the effects of the rumble fallback: `left` and `right` strength (0 to 1) and `ms`, the length (0 turns a one-shot off; continuous effects use only the strengths). Also under **Advanced -> Rumble fallback** in the window, which shows the strengths as 0% to 100% |
| `journal_dir`, `bindings_dir` | `""` | empty for Elite's standard folders. From the window (**Advanced -> Folders**), a full path to a folder that is there |
| `backend` | `""` | the controller app: `"dsx"`, `"ds4windows"`, or `"auto"` for the one that runs (with both, the one that answers; with neither, DS4Windows if it answers on its port, else DSX). `""` means not chosen yet: the window's first run asks, and it works as `"auto"` meanwhile. Same as the tray's **Controller app** and the window's Controller page, see [DS4Windows](ds4windows.md) |
| `dsx_port` | `0` | 0 reads DSX's port file (6969 if there is none). Any other number is used as the port |
| `ds4windows_port` | `0` | 0 uses DS4Windows' own address and port (127.0.0.1:6969 if it has none), read again when DS4Windows starts after EDSense. Any other number is used as the port (the game mod listener; DS4Windows' UDP server for the gyro is always found from its own settings) |
| `ds4windows_haptics` | `"auto"` | with DS4Windows, where native haptics go: `"auto"` the controller's own audio device when it is wired, else the virtual DualSense's; `"controller"` or `"virtual"` for one of them. Also on the window's Controller page |
| `poll_ms` | `25` | how often the controller is updated, in ms: 20 to 100 in the window. A larger value in the file is used as written |

### Fire groups

`fire_groups` sets the weapon feel per fire group (1 is the first): `primary` is R2, `secondary` is L2. Each is `"auto"` or one of `beam`, `pulse`, `burst`, `multicannon`, `cannon`, `fragment`, `railgun`, `plasma`, `missile`, `mining`, `generic`. Groups you don't list are `"auto"`.

```json
"fire_groups": {"1": {"primary": "multicannon", "secondary": "auto"}}
```

With `"auto"`, EDSense uses what the HUD showed for that fire group (hardpoints in and out count separately). Until it has seen that, or with the HUD reader off, it guesses from your loadout: the most common weapon type on R2, the next one on L2.

The window's **Feel** page sets them too, under **Fire groups**: an R2 and an L2 choice for each group, **Add fire group**, and **Remove** for each group from 3 on.

### Triggers

Each entry in `triggers` is a DSX v3 mode and its parameters, for example `"ship_weapons_r": {"mode": "WEAPON", "params": [2, 5, 6]}`. The window's **Triggers** page sets them too: a mode for each trigger and a choice for each of its values, which offers only what the mode takes.

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

On the Triggers page they are Off, Feedback, Weapon, Vibration, Slope, Feedback by position and Vibration by position. An unknown mode counts as `OFF`.

DS4Windows takes a few more values than these (a start or strength of 0, a vibration frequency up to 255), but refuses a whole packet with one value out of its range. With DS4Windows, EDSense clamps each value into DS4Windows' range before it sends it, sends a mode with the wrong number of values as `OFF`, and logs each changed value once.

## Command-line options

Run these from PowerShell or cmd in the EDSense folder, for example `.\EDSense.exe -feeltest`. They print to that window. To open PowerShell there, right-click an empty spot in the folder and choose **Open in Terminal** (Windows 11), or Shift+right-click and choose **Open PowerShell window here** (Windows 10).

| Option | What it does |
|---|---|
| `-feeltest` | plays four plain tones (80, 120, 170 and 250 Hz, 2 s each), then the turn feels `"waves"` and `"push"`, then the jump feels `"swell"` and `"calm"` (a 5 s countdown, then the tunnel). About a minute. Needs native haptics. Close Elite first: it does not run while Elite and the tray EDSense both run |
| `-demo` | plays every effect once, without Elite |
| `-padtest` | lists Sony controllers, finds the virtual DualSense (DSX's, or DS4Windows' under usbip-win2), tests the left then the right side (rumble with DSX, native haptics with DS4Windows), then shows R2, L2, R1, Circle, gyro and touch for 15 s |
| `-hapticstest` | lists the audio outputs and plays 9 native haptics steps: left 60 Hz, right 150 Hz, multi-cannon, beam, boost, shields down, hull hit, hardpoints, docking clamps. With DS4Windows, on the device `ds4windows_haptics` picks, after switching DS4Windows' rumble emulation off (see [DS4Windows](ds4windows.md#native-haptics-and-rumble)) |
| `-gyrotest` | about 75 seconds: measures how the virtual DualSense reports and drifts, then how far DSX's gyro and EDSense's move the mouse per degree, and what DSX does while EDSense aims. It saves a calibration and prints the settings that match your DSX profile, if DSX is on the profile it uses for Elite (it says so otherwise). Keep your hand off the mouse. Close Elite first, or quit the tray EDSense. With DS4Windows it runs only while the DS4Windows profile leaves the gyro alone (Output Mode Passthru), and measures the motion EDSense's aim reads: the UDP server's when it sends, else the virtual DualSense's |
| `-hudtest <screenshots>` | reads the HUD from screenshots, see [checking with screenshots](how-it-works.md#checking-with-screenshots) |
| `-console` | runs in the console instead of the tray. Ctrl+C gives the controller back |
| `-verbose` | prints every packet sent to DSX or DS4Windows |
| `-backend <app>` | uses `dsx`, `ds4windows` or `auto` for this run, whatever `backend` says. Not saved. A choice in the tray or the window replaces it |
| `-config <file>` | uses another settings file. Its folder then holds the log, `hud_palette.json`, `hud_debug`, the profile backups and `ui_state.json` |
| `-tray` | starts in the tray without opening the window, as for a shortcut in the Startup folder (see [Start with Windows](setup.md#start-with-windows)). Does nothing when EDSense already runs |
| `-version` | prints the version |
| `--window` | how EDSense starts its own window; not for running by hand |

## DSX profile

EDSense brings a DSX controller profile called "Elite Dangerous": DualSense emulation with its audio device, gyro aim (motion to mouse, with passthrough), a touchpad setup, and DSX's own adaptive triggers off.

- If DSX has no "Elite Dangerous" profile, EDSense adds it once it surely uses DSX (chosen, or found running) and the first run is over (or when there is none: no WebView2 Runtime, or `-console`). It also makes it Elite's game profile, if DSX has none for Elite yet. During the first run, **Install...** in its **Set it up** step adds it when you ask. Once you pressed **Install...** or **Cancel** on the card, EDSense leaves it to you, and the card says when **Install...** is the way.
- The window's Controller page has a **DSX profile** card: whether DSX has the profile and whether Elite uses it, with **Install...** while it is missing, **Reset...**, **Cancel** while EDSense waits for DSX to close, and **Open backups** for `dsx_profile_backups`.
- An existing "Elite Dangerous" profile is left alone, and so is a game profile you already set for Elite. So if DSX uses another profile for Elite, it keeps using it, and the Setup card shows it as yours.
- **Reset...** on the window's Controller page, or **Reset DSX profile...** in the tray menu while the window cannot open, writes the bundled profile and makes it Elite's game profile; when Elite has another game profile in DSX, the question says it changes. Your old "Elite Dangerous" profile, and DSX's `GameProfilesUpdates.json` before EDSense changes it, are saved in `dsx_profile_backups`. A profile file EDSense cannot read is left as it is.
- DSX saves its profiles when it exits, so EDSense writes only while DSX is closed. It waits for you to close DSX, then says "Start DSX again to use it".

If you keep your own profile, check these in DSX:

- Virtual Device: **DualSense Emulation**. EDSense plays the haptics and reads the triggers, sticks and gyro through it.
- **Haptics | Rumble -> Game Feedback** on.
- **Advanced -> Audio Routing and Volume -> Haptic Motors** on.
- Motion: **Passthrough** on, for the turn feel with `"gyro_by": "dsx"` (EDSense's gyro does not need it).
- Best off: **Additional Effects**, **Additional Audio**, and the profile's **Adaptive Triggers** (EDSense sets the triggers while you play).

## DSX Native Mode

EDSense can still set the triggers and lights in Native Mode. The haptics, and everything EDSense reads from the controller (firing, boost, turns), need DSX's virtual DualSense, which Native Mode does not create.

Native Mode still hides the DualSense with HidHide, so Elite sees no controller and your bindings don't load (`BindingLoadingErrors.log`: `Missing devices: DualShock4`). Add `EliteDangerous64.exe` in **DSX -> HidHide -> Add Application**. It is in `...\steamapps\common\Elite Dangerous\Products\elite-dangerous-odyssey-64\`.

If Elite does not map the DualSense to its "DualShock4" device, use DSX's DualShock 4 emulation. Or add the DualSense (`0CE6`) and DualSense Edge (`0DF2`) as `<Alternative>` lines under `<DualShock4>` in Elite's `ControlSchemes\DeviceMappings.xml`. Game updates can overwrite that file.

## When something does not work

See [Troubleshooting](troubleshooting.md).
