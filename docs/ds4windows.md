# DS4Windows

Back to the [README](../README.md).

EDSense can drive the controller through DS4Windows 5 in place of DSX. DS4Windows 5 listens for the same game-mod packets as DSX, and its virtual DualSense carries the controller's input and motion. The triggers, lightbar, player LEDs, mic LED, native haptics and EDSense's gyro aim all work through it. Rumble works too, with `"haptics_mode": "rumble"` only, see [Native haptics and rumble](#native-haptics-and-rumble).

Only the DS4Windows 5 builds have this listener (they run on VIIPER and usbip-win2). Older DS4Windows versions have no game mod support.

## Get DS4Windows 5

1. Quit DSX (its tray icon -> Exit). Only one program can listen on the port.
2. Download from the DS4Windows 5 releases page: <https://github.com/hbashton/DS4Windows/releases>. Tested with release **VIIPERRC4.6.6** (DS4Windows 5.0.12.0): the installer `DS4Windows_5.0.12.0_Setup_x64.exe`, or the portable `DS4Windows_VIIPER_x64.zip`.
3. The DS4Windows 5 builds are release candidates and are not code-signed, so Windows SmartScreen warns before the first start. Check the download against the `SHA256SUMS.txt` on the same release page (`Get-FileHash` in PowerShell) before you run it.
4. Include HidHide when the installer offers it. It keeps games from seeing the real controller next to the virtual one.

## Set up DS4Windows

In DS4Windows:

1. **Settings -> Game mod support (DSX)**: tick **Let game mods control triggers and lights**. The status below the box should say **Listening**. The address and port are under **Connection details**; EDSense reads them from DS4Windows' settings file, so leave them as they are unless another program uses the port. After a change, press **Apply / Retry**.
2. **Settings -> Use HidHide to Prevent Double Input**: tick it, then plug the controller in again.
3. Edit the profile your controller uses:
   - **Advanced -> Emulated Controller: DualSense**. EDSense reads the controller and plays the haptics through the virtual DualSense.
   - **Gyro -> Output Mode: Passthru** for EDSense's gyro aim (or **Controls** with nothing set on the gyro). With **Mouse** or **Mouse Joystick**, DS4Windows' own gyro aims and EDSense's stays off, so the two never move the mouse together.
   - **Touchpad -> Output Mode: Passthru**. With **Mouse**, DS4Windows' default, touching the touchpad moves the cursor in Elite.
   - **Trigger Lab** off. Trigger Lab wins over EDSense's triggers on each trigger where it is on.
4. Press **Start** and connect the DualSense. A USB cable is best: then EDSense can play the haptics straight to the controller's own audio device.

The window's Controller page checks these while it is open, every 2 seconds: a row per check, green once it is set up, with what to do under **How**.

## Set up EDSense

On a fresh install, the EDSense window asks which app you use the first time it opens. Later, pick **DS4Windows** on the window's Controller page, or tray -> **Controller app -> DS4Windows**. The choice applies at once: EDSense hands the controller back to your profile for about a second while it switches. The window asks first while EDSense drives the controller. Or set `"backend": "ds4windows"` in `edsense.json` and press **Apply now** on the Controller page.

With `"auto"` (settings files from earlier versions get it) EDSense uses the app that runs: DS4Windows when only DS4Windows runs, DSX when only DSX runs, the one that answers when both run. When neither runs, it uses DS4Windows if DS4Windows answers on its port, else DSX. A DS4Windows older than 5 counts as not running, since it has no game mod support, and a DS4Windows whose exe was renamed is found by its window.

Auto looks at the apps again every 3 seconds while EDSense is not driving the controller (in the main menu, paused, with Elite closed, or while the app in use does not answer). When the other app surely runs, it switches by itself. When it can only guess (neither app runs, or both run and neither answers), it stays on the app it uses and the log says so.

| Key | Default | What it does |
|---|---|---|
| `backend` | `""` | `"auto"`, `"dsx"` or `"ds4windows"`. Empty means not chosen yet, and works as `"auto"`. A change in the file applies with **Apply now** in the window, or at the next start |
| `ds4windows_port` | `0` | 0 uses DS4Windows' own address and port (from its `Profiles.xml`, else 127.0.0.1:6969), read when EDSense connects, and again when DS4Windows starts after EDSense and does not answer yet (a portable DS4Windows keeps its settings in its own folder). Any other number is used as the port. A change applies with **Apply now** in the window, or at the next start |
| `ds4windows_haptics` | `"auto"` | where native haptics go. `"auto"`: the controller's own audio device when it is wired, else DS4Windows' virtual DualSense. `"controller"`: always the controller's own (USB only). `"virtual"`: always the virtual DualSense's, which DS4Windows passes on when its controller audio support is on. Applies at once; also on the Controller page |

`-backend ds4windows` (or `dsx`, `auto`) on the command line picks the app for one run and is not saved. A choice in the tray or the window replaces it.

The log says which app EDSense uses and why: `Controller app: DS4Windows (auto: DS4Windows runs)`, then `DS4Windows connected` once DS4Windows answers. A switch starts with `Controller app: switching to DS4Windows (chosen in the window)`, and the window's Activity says `Now using DS4Windows`.

## The gyro

EDSense cannot switch DS4Windows' own gyro aim on and off, as it does with DSX. So EDSense aims only while the controller's DS4Windows profile leaves the gyro alone: Output Mode **Passthru**, or **Controls** with no gyro direction on a stick or the mouse. When the profile moves the mouse or a stick with the gyro, or EDSense cannot tell what the profile does, EDSense's gyro stays off and DS4Windows' gyro works as set.

To find the profile, EDSense asks the running DS4Windows, the way DS4Windows' own `-command Query` does. When that gets no answer (DS4Windows running as administrator, or its portable lab), it reads DS4Windows' files: `Auto Profiles.xml` for Elite while Elite runs, else the profile linked to the controller in `LinkedProfiles.xml` (**Link profile**), else the controller's profile in `Profiles.xml`. It checks again every 3 seconds while Elite runs with gyro aim on, at once when Elite comes to the front, and every 2 seconds while the window shows its checks. The log names the profile and where it was found, for example `DS4Windows: controller 1, profile "Elite" (DS4Windows says so; gyro output Passthru): the gyro is free`.

DS4Windows answers in plain ASCII, so a profile name with letters such as Turkish ones comes back with `?` in their place. EDSense then looks for the one profile file that fits; if two fit, it cannot tell, and its gyro stays off. Renaming the profile to plain letters avoids this.

EDSense keeps a separate gyro calibration for DS4Windows, `gyro_calibration_ds4windows.json`, since its virtual DualSense counts motion a little differently.

## Native haptics and rumble

Rumble through DS4Windows switches the controller to rumble emulation, which mutes its native haptics. DS4Windows repeats that switch in every report it sends to the controller after it, until the controller is plugged in again or a program switches rumble emulation off.

So with DS4Windows, EDSense rumbles only when `haptics_mode` is `"rumble"`. When the native haptics cannot start, there are no haptics, and the log says why. `-padtest` plays its left and right test through the native haptics.

Each time EDSense opens DS4Windows' virtual DualSense, and each time it stops rumbling, it sends a report that switches rumble emulation off, so the native haptics come back. EDSense opens the virtual DualSense while Elite runs, for the demo, and in `-padtest`, `-hapticstest` and `-feeltest`. After that report, DS4Windows' own rumble (from a special action, for example) stays off until you press **Stop** and **Start** in DS4Windows, or plug the controller in again.

If the haptics stay silent after another program rumbled the controller, run `.\EDSense.exe -hapticstest`, or start Elite with EDSense running. If that does not help, press **Stop** and **Start** in DS4Windows, or plug the controller in again.

With `"haptics_mode": "rumble"`, if EDSense crashes or is killed while it rumbles, the controller keeps rumbling until EDSense opens the virtual DualSense again, or until you press **Stop** and **Start** in DS4Windows.

## When something does not work

Open the log (tray -> **Open log**), or the window's Controller page, whose setup list says what is missing.

- **Red tray icon, "DS4Windows not connected"**: DS4Windows is closed or stopped, or **Let game mods control triggers and lights** is off.
- **"DSX answers on DS4Windows' port"**: quit DSX, then press **Apply / Retry** in DS4Windows.
- **"does not emulate a DualSense"**: set the profile's **Advanced -> Emulated Controller** to **DualSense**.
- **"Games can see your real DualSense"**: tick **Use HidHide to Prevent Double Input** in DS4Windows' settings.
- **Gyro aim does nothing**: look for `Gyro:` and `DS4Windows:` lines. Set the profile's **Gyro -> Output Mode** to **Passthru** and save it. `.\EDSense.exe -gyrotest` shows more; with DS4Windows it runs only while the profile leaves the gyro alone.
- **No haptics**: `.\EDSense.exe -hapticstest` plays them on the device `ds4windows_haptics` picks. Try `"controller"` with a USB cable, or `"virtual"`. If they stopped after something rumbled the controller, see [Native haptics and rumble](#native-haptics-and-rumble).

### Triggers stay stiff after EDSense crashed

DS4Windows keeps what EDSense set until it is handed back. EDSense hands the controller back when you pause it, in the main menu, when Elite closes, when you quit it, and when it starts and finds DS4Windows. If EDSense was killed, start it again, or in DS4Windows untick and tick **Let game mods control triggers and lights**, or press **Stop** and **Start**.

## Limits

- DS4Windows takes a stricter set of trigger values than DSX: every value in range and the right number of values for each mode. EDSense brings each value in range before sending, logs each one it had to change once, and sends a mode DS4Windows lacks as `OFF`. See [triggers](settings.md#triggers).
- The gyro cannot be switched off in menus through DS4Windows. EDSense's own gyro aim stops in menus by itself.
- DS4Windows is GPL-3 software. EDSense only talks to it, through its game-mod listener and its window message, and reads its settings files.
