# DS4Windows

Back to the [README](../README.md).

EDSense can drive the controller through DS4Windows 5 in place of DSX. DS4Windows 5 listens for the same game-mod packets as DSX, its virtual DualSense carries the controller's input and motion, and its UDP server carries the motion without the virtual DualSense's dead band (see [The gyro](#the-gyro)). The triggers, lightbar, player LEDs, mic LED, native haptics and EDSense's gyro aim all work through it. Rumble works too, with `"haptics_mode": "rumble"` only, see [Native haptics and rumble](#native-haptics-and-rumble).

Only the DS4Windows 5 builds have this listener (they run on VIIPER and usbip-win2). Older DS4Windows versions have no game mod support.

## Get DS4Windows 5

1. Quit DSX (its tray icon -> Exit). Only one program can listen on the port.
2. Download from the DS4Windows 5 releases page: <https://github.com/hbashton/DS4Windows/releases>. Tested with release **VIIPERRC4.6.6** (DS4Windows 5.0.12.0): the installer `DS4Windows_5.0.12.0_Setup_x64.exe`, or the portable `DS4Windows_VIIPER_x64.zip`.
3. The DS4Windows 5 builds are release candidates and are not code-signed, so Windows SmartScreen warns before the first start. Check the download against the `SHA256SUMS.txt` on the same release page (`Get-FileHash` in PowerShell) before you run it.
4. Include HidHide when the installer offers it. It keeps games from seeing the real controller next to the virtual one.

## Set up DS4Windows

EDSense can do steps 1, 2 and 4 for Elite itself: see [Let EDSense set up DS4Windows](#let-edsense-set-up-ds4windows). By hand, in DS4Windows:

1. **Settings -> Game mod support (DSX)**: tick **Let game mods control triggers and lights**. The status below the box should say **Listening**. The address and port are under **Connection details**; EDSense reads them from DS4Windows' settings file, so leave them as they are unless another program uses the port. After a change, press **Apply / Retry**.
2. **Settings -> UDP Server**: tick **Enable Server**. Leave the address at 127.0.0.1 and the port at 26760 unless another program uses the port, and leave **Use Smoothing** off. EDSense's gyro aim reads the controller's motion from it, see [The gyro](#the-gyro). At that address and port EDSense finds it at once, without a restart. DS4Windows writes these settings to its settings file, where EDSense reads the port and **Use Smoothing**, only when it exits: after changing them, exit DS4Windows (its tray icon -> **Exit**) and start it again. If the Controller page then says the server sends no motion for the controller, press **Stop** and **Start** in DS4Windows.
3. **Settings -> Use HidHide to Prevent Double Input**: tick it, then plug the controller in again.
4. Edit the profile your controller uses:
   - **Advanced -> Emulated Controller: DualSense**. EDSense reads the controller and plays the haptics through the virtual DualSense.
   - **Gyro -> Output Mode: Passthru** for EDSense's gyro aim (or **Controls** with nothing set on the gyro). With **Mouse** or **Mouse Joystick**, DS4Windows' own gyro aims and EDSense's stays off, so the two never move the mouse together.
   - **Touchpad -> Output Mode: Passthru**. With **Mouse**, DS4Windows' default, touching the touchpad moves the cursor in Elite.
   - **Trigger Lab** off. Trigger Lab wins over EDSense's triggers on each trigger where it is on.
5. Press **Start** and connect the DualSense. A USB cable is best: then EDSense can play the haptics straight to the controller's own audio device.

The window's Controller page checks these while it is open, every 2 seconds: a row per check, green once it is set up, with what to do under **How**. A row that EDSense's own profile would fix has **Let EDSense fix it**, which takes you to the **DS4Windows profile** card.

## Let EDSense set up DS4Windows

The **DS4Windows profile** card on the window's Controller page (and in the first run's **Set it up** step) shows what EDSense finds in DS4Windows' settings. **Install...** asks first, then writes what is missing of these four, and the card lists each as **To do**, **Done** or **Not needed**:

1. A profile called **Elite Dangerous (EDSense)**, the file `Profiles\Elite Dangerous (EDSense).xml`. It holds DS4Windows' defaults and four settings: DualSense emulation, the gyro and the touchpad passed through, and a blue lightbar while EDSense does not set it:

   ```xml
   <Color>0,0,255</Color>
   <OutputContDevice>ViiperDualSense</OutputContDevice>
   <GyroOutputMode>Passthru</GyroOutputMode>
   <TouchpadOutputMode>Passthru</TouchpadOutputMode>
   ```

   So while Elite is in front, your usual profile's button mapping, stick settings, lightbar and gyro mouse are not used. Change the new profile in DS4Windows as you like: EDSense leaves it as it is.
2. An Auto Profiles rule that loads it whenever Elite is in front, added as the last rule in `Auto Profiles.xml`. The rest of the file stays byte for byte as it was:

   ```xml
   <Program path="EliteDangerous64.exe$" title="" device="DualSense" applyToAllControllers="true">
     <Controller1>Elite Dangerous (EDSense)</Controller1>
     <Controller2>(none)</Controller2>
     (Controller3 to Controller8 the same)
     <TurnOff>False</TurnOff>
   </Program>
   ```
3. **Let game mods control triggers and lights** on: `<UseDSXUDPServer>True</UseDSXUDPServer>` in `Profiles.xml`. The port and address under **Connection details** stay as they are when DS4Windows can use them, else they become 6969 and 127.0.0.1. Nothing else in the file changes.
4. **UDP Server -> Enable Server** on: `<UseUDPServer>True</UseUDPServer>` in `Profiles.xml`, for EDSense's gyro aim. A port DS4Windows cannot read becomes 26760, and an address other than 127.0.0.1, localhost or 0.0.0.0 becomes 127.0.0.1, this PC's network address too (the question says so; when the address changes before EDSense writes, it writes nothing, and **Install...** asks again). Nothing else in the file changes. When both settings are to do, `Profiles.xml` is written once.

EDSense writes in DS4Windows' settings folder: the one of the DS4Windows it saw running since it started (a portable DS4Windows keeps its settings next to its exe), else `%APPDATA%\DS4Windows`. The question names the folder, and **Files** on the card lists the files. It writes only where DS4Windows already made `Profiles.xml`, `Auto Profiles.xml` and the `Profiles` folder, so start DS4Windows once if the card says its settings were not found. It does not set up a DS4Windows before 5, DS4Windows' portable lab, or a file it cannot read (saved as UTF-16, or with a value DS4Windows would not read either). When DS4Windows has settings both next to its exe and in `%APPDATA%\DS4Windows`, it asks at each start which one to use, so EDSense writes in neither, and the card names both: keep only the one you use.

DS4Windows saves its settings when it exits, so it **must be closed** while EDSense writes: use its tray icon -> **Exit**, since closing its window may only hide it. The card says "Waiting for DS4Windows to close" until DS4Windows has been closed for 2 seconds, then EDSense writes and says so, also when the window is closed by then. **Cancel** stops the wait; while EDSense writes, the card says so and the wait can no longer be cancelled. EDSense writes only what the question listed: if DS4Windows' files changed while it waited and it would now write more, it writes nothing, and the card says so. Then start DS4Windows again: the **Auto Profiles** tab shows the rule, and the profile list has "Elite Dangerous (EDSense)". If DS4Windows starts while EDSense writes, EDSense stops, and the card says only some of it is written: exit DS4Windows and press **Install...** again. A wait ends when EDSense quits; nothing is written at the next start.

**Backups.** Before it writes, EDSense copies `Profiles.xml`, `Auto Profiles.xml` and an `Elite Dangerous (EDSense).xml` already there into `ds4windows_backups\YYYYMMDD-HHMMSS\` next to `edsense.json`. If a copy fails, nothing is written. Each file is written to a temporary file in the same folder and renamed over the old one, then read back and checked the way DS4Windows reads it. A file that does not pass is put back from the copy, and nothing after it is written; the files written before it stay as written, and the card names them. EDSense never deletes the copies. **Open backups** on the card opens the folder.

**Your own setup stays.** When DS4Windows already has an Auto Profiles rule for Elite (its path points at `EliteDangerous64.exe`, or its path or title has "elite" in it), EDSense adds no profile and no rule. The card names that rule's profile with its checks (DualSense emulation, gyro, touchpad, Trigger Lab), and offers only the game mod and UDP server settings when they are off. The same goes when the profile your controller uses already works with EDSense: it emulates a DualSense, leaves the gyro free, the touchpad is not a mouse and Trigger Lab is off. A profile file called "Elite Dangerous (EDSense)" is never replaced, except by **Reset...**, which the card offers once Elite uses EDSense's profile: it writes EDSense's version again, after a copy. DS4Windows picks a rule made for a DualSense before one for any controller, whatever their order: so once EDSense's rule is there, it wins over your own Elite rule for any controller, and the card says so. To use yours, delete EDSense's rule in the **Auto Profiles** tab.

**HidHide** stays yours to set up, as in step 3 above. The card shows whether games see only the virtual DualSense, with what to do under **How**.

**To undo** what EDSense wrote:

- In DS4Windows' **Auto Profiles** tab, remove the rule for `EliteDangerous64.exe$`, and delete the profile "Elite Dangerous (EDSense)" in **Profiles**. Untick **Let game mods control triggers and lights** and **UDP Server -> Enable Server** if you no longer use EDSense with DS4Windows.
- Or exit DS4Windows (tray icon -> **Exit**) and copy `Profiles.xml` and `Auto Profiles.xml` from a folder in `ds4windows_backups` back into DS4Windows' settings folder, and `Elite Dangerous (EDSense).xml`, when the folder has it, into its `Profiles` folder. When it does not, delete `Profiles\Elite Dangerous (EDSense).xml`.

The log says `DS4Windows profile install requested`, `DS4Windows profile: waiting for DS4Windows to be closed`, then what was written and where the copies are, or `DS4Windows profile not written:` and why.

## Set up EDSense

On a fresh install, the EDSense window asks which app you use the first time it opens. Later, pick **DS4Windows** on the window's Controller page, or tray -> **Controller app -> DS4Windows**. The choice applies at once: EDSense hands the controller back to your profile for about a second while it switches. The window asks first while EDSense drives the controller. Or set `"backend": "ds4windows"` in `edsense.json` and press **Apply now** on the Controller page.

With `"auto"` (settings files from earlier versions get it) EDSense uses the app that runs: DS4Windows when only DS4Windows runs, DSX when only DSX runs, the one that answers when both run. When neither runs, it uses DS4Windows if DS4Windows answers on its port, else DSX. A DS4Windows older than 5 counts as not running, since it has no game mod support, and a DS4Windows whose exe was renamed is found by its window.

Auto looks at the apps again every 3 seconds while EDSense is not driving the controller (in the main menu, paused, with Elite closed, or while the app in use does not answer). When the other app surely runs, it switches by itself. When it can only guess (neither app runs, or both run and neither answers), it stays on the app it uses and the log says so.

| Key | Default | What it does |
|---|---|---|
| `backend` | `""` | `"auto"`, `"dsx"` or `"ds4windows"`. Empty means not chosen yet, and works as `"auto"`. A change in the file applies with **Apply now** in the window, or at the next start |
| `ds4windows_port` | `0` | 0 uses DS4Windows' own address and port (from its `Profiles.xml`, else 127.0.0.1:6969), read when EDSense connects, and again when DS4Windows starts after EDSense and does not answer yet (a portable DS4Windows keeps its settings in its own folder). Any other number is used as the port (the game mod listener; DS4Windows' UDP server for the gyro is always found from its own settings). A change applies with **Apply now** in the window, or at the next start |
| `ds4windows_haptics` | `"auto"` | where native haptics go. `"auto"`: the controller's own audio device when it is wired, else DS4Windows' virtual DualSense. `"controller"`: always the controller's own (USB only). `"virtual"`: always the virtual DualSense's, which DS4Windows passes on when its controller audio support is on. Applies at once; also on the Controller page |

`-backend ds4windows` (or `dsx`, `auto`) on the command line picks the app for one run and is not saved. A choice in the tray or the window replaces it.

The log says which app EDSense uses and why: `Controller app: DS4Windows (auto: DS4Windows runs)`, then `DS4Windows connected` once DS4Windows answers. A switch starts with `Controller app: switching to DS4Windows (chosen in the window)`, and the window's Activity says `Now using DS4Windows`.

## The gyro

EDSense cannot switch DS4Windows' own gyro aim on and off, as it does with DSX. So EDSense aims only while the controller's DS4Windows profile leaves the gyro alone: Output Mode **Passthru**, or **Controls** with no gyro direction on a stick or the mouse. When the profile moves the mouse or a stick with the gyro, or EDSense cannot tell what the profile does, EDSense's gyro stays off and DS4Windows' gyro works as set.

The window's **Gyro aim** page shows the profile's gyro check and the motion check under **EDSense gyro**, as the Controller page does, with what to do under **How**.

**Slow movement.** DS4Windows' virtual DualSense passes no rotation of 2 degrees per second or less on each axis: DS4Windows sets every gyro value of up to 32 counts (16 per degree per second) to 0 before the virtual pad sees it. Slow aiming then does nothing, and a turn that speeds up past 2 degrees per second starts with a jump. No EDSense setting brings back what never arrives, `"gyro_low_speed": "exact"` neither. DS4Windows' UDP server (Settings -> UDP Server, the motion server emulators such as Cemu use) sends the same motion with nothing dropped. So EDSense reads the motion from the UDP server whenever it sends, and from the virtual DualSense otherwise. Both give the same degrees per second, so the sensitivity settings feel the same, and with the UDP server `"exact"` lets the slowest movement through.

The log says `Gyro: reading the motion from DS4Windows' UDP server (127.0.0.1:26760, controller 1): no dead band` when it starts, and `Gyro: DS4Windows' UDP server sends no motion, ...` when EDSense goes back to the virtual DualSense. With the UDP server off, EDSense says once, after 10 seconds of gyro aim, that slow gyro movement under 2 degrees per second is lost. The Controller page's **motion** row shows the same, and the DS4Windows profile card can turn the UDP server on.

EDSense reads the UDP server's address, port and **Use Smoothing** from DS4Windows' `Profiles.xml` (127.0.0.1:26760 when it has none), again when the file changes. DS4Windows writes that file only when it exits, so after changing these settings in DS4Windows, exit it (its tray icon -> **Exit**) and start it again. DS4Windows holds the port to 1024 to 65535, and EDSense asks it there too. EDSense reads the server on this PC only, at 127.0.0.1, localhost or 0.0.0.0: with any other address, this PC's network address too, the Controller page says EDSense does not use it. EDSense still asks 127.0.0.1 at that port, so setting the address to 127.0.0.1 in DS4Windows and unticking and ticking **Enable Server** works at once. The UDP server serves DS4Windows' controllers 1 to 4 only. When it answers for the controller and sends none of its motion, which can happen when **Enable Server** is ticked while the controllers run, the Controller page and a message say so: press **Stop** and **Start** in DS4Windows. **Use Smoothing** delays the motion; leave it off. The profile's Gyro Output Mode does not change what the UDP server sends; **Passthru** still gives the turn feel its motion on the virtual DualSense.

**EDSense gyro**, in the tray or on the window's **Gyro aim** page: unticked, EDSense does not aim. EDSense cannot switch DS4Windows' own gyro, so DS4Windows' gyro aims only when the profile uses it (Gyro -> Output Mode **Mouse**). With a profile that leaves the gyro alone, nothing aims then, and EDSense says so once. It keeps checking the profile for this, and says it once Elite has been in front for a few seconds, since DS4Windows gives the window in front its own profile. To turn gyro aim off on purpose, untick **Gyro aim**.

To find the profile, EDSense asks the running DS4Windows, the way DS4Windows' own `-command Query` does. When that gets no answer (DS4Windows running as administrator, or its portable lab), it reads DS4Windows' files: `Auto Profiles.xml` for Elite while Elite runs, else the profile linked to the controller in `LinkedProfiles.xml` (**Link profile**), else the controller's profile in `Profiles.xml`. It checks again every 3 seconds while Elite runs with gyro aim on, at once when Elite comes to the front, and every 2 seconds while the window shows its checks. The log names the profile and where it was found, for example `DS4Windows: controller 1, profile "Elite" (DS4Windows says so; gyro output Passthru): the gyro is free`.

DS4Windows answers in plain ASCII, so a profile name with letters such as Turkish ones comes back with `?` in their place. EDSense then looks for the one profile file that fits; if two fit, it cannot tell, and its gyro stays off. Renaming the profile to plain letters avoids this.

EDSense keeps a gyro calibration for each source: `gyro_calibration_ds4windows.json` for the virtual DualSense and `gyro_calibration_ds4windows_udp.json` for the UDP server. The virtual DualSense's dead band hides the small drift the UDP server shows. For a source with no calibration yet, EDSense starts from no drift and learns it as it does a saved one: in full while the gyro holds (menus, pause, Elite behind), and by at most 0.25 degrees per second per still stretch while you aim, so a slow pan is not taken for drift. **Calibrate gyro...** learns it at once; one under way when the source changes starts again on the new source.

## Native haptics and rumble

Rumble through DS4Windows switches the controller to rumble emulation, which mutes its native haptics. DS4Windows repeats that switch in every report it sends to the controller after it, until the controller is plugged in again or a program switches rumble emulation off.

So with DS4Windows, EDSense rumbles only when `haptics_mode` is `"rumble"` (**Mode: Rumble** on the window's **Feel** page). When the native haptics cannot start, there are no haptics, and the log says why. `-padtest` plays its left and right test through the native haptics.

Each time EDSense opens DS4Windows' virtual DualSense, and each time it stops rumbling, it sends a report that switches rumble emulation off, so the native haptics come back. EDSense opens the virtual DualSense while Elite runs, for the demo, and in `-padtest`, `-hapticstest` and `-feeltest`. After that report, DS4Windows' own rumble (from a special action, for example) stays off until you press **Stop** and **Start** in DS4Windows, or plug the controller in again.

If the haptics stay silent after another program rumbled the controller, run `.\EDSense.exe -hapticstest`, or start Elite with EDSense running. If that does not help, press **Stop** and **Start** in DS4Windows, or plug the controller in again.

With `"haptics_mode": "rumble"`, if EDSense crashes or is killed while it rumbles, the controller keeps rumbling until EDSense opens the virtual DualSense again, or until you press **Stop** and **Start** in DS4Windows.

## When something does not work

Open the log (tray -> **Open log**), or the window's Controller page, whose setup list says what is missing.

- **Red tray icon, "DS4Windows not connected"**: DS4Windows is closed or stopped, or **Let game mods control triggers and lights** is off.
- **"DSX answers on DS4Windows' port"**: quit DSX, then press **Apply / Retry** in DS4Windows.
- **"does not emulate a DualSense"**: set the profile's **Advanced -> Emulated Controller** to **DualSense**.
- **"Games can see your real DualSense"**: tick **Use HidHide to Prevent Double Input** in DS4Windows' settings.
- **"Waiting for DS4Windows to close" does not end**: DS4Windows still runs, maybe only in the tray. Use its tray icon -> **Exit**, and look for `DS4Windows.exe` in Task Manager.
- **Gyro aim does nothing**: look for `Gyro:` and `DS4Windows:` lines. Set the profile's **Gyro -> Output Mode** to **Passthru** and save it, or turn on the UDP server. `.\EDSense.exe -gyrotest` shows more; with DS4Windows it runs only while the profile leaves the gyro alone.
- **Slow gyro aim does nothing, or turns start with a jump**: DS4Windows' UDP server is off. Tick **Settings -> UDP Server -> Enable Server**, see [The gyro](#the-gyro). If the Controller page says it sends no motion for the controller, press **Stop** and **Start** in DS4Windows.
- **Unticking EDSense gyro gives no gyro aim**: DS4Windows' gyro follows only its profile. Set the profile's **Gyro -> Output Mode** to **Mouse** for DS4Windows' own gyro aim.
- **No haptics**: `.\EDSense.exe -hapticstest` plays them on the device `ds4windows_haptics` picks. Try `"controller"` with a USB cable, or `"virtual"`. If they stopped after something rumbled the controller, see [Native haptics and rumble](#native-haptics-and-rumble).

More fixes: [Troubleshooting](troubleshooting.md).

### Triggers stay stiff after EDSense crashed

DS4Windows keeps what EDSense set until it is handed back. EDSense hands the controller back when you pause it, in the main menu, when Elite closes, when you quit it, and when it starts and finds DS4Windows. If EDSense was killed, start it again, or in DS4Windows untick and tick **Let game mods control triggers and lights**, or press **Stop** and **Start**.

## Limits

- DS4Windows takes a stricter set of trigger values than DSX: every value in range and the right number of values for each mode. EDSense brings each value in range before sending, logs each one it had to change once, and sends a mode DS4Windows lacks as `OFF`. See [triggers](settings.md#triggers).
- The gyro cannot be switched off in menus through DS4Windows. EDSense's own gyro aim stops in menus by itself.
- DS4Windows is GPL-3 software. EDSense only talks to it, through its game-mod listener, its UDP server and its window message, and reads its settings files. It writes them only when you press **Install...** or **Reset...** on the DS4Windows profile card, as [above](#let-edsense-set-up-ds4windows).
