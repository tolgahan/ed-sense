# Using EDSense

Back to the [README](../README.md).

## The window

EDSense opens its window when you start it, unless you start it with `-tray`. Closing the window keeps EDSense running in the tray. To open it again, click the tray icon, or start `EDSense.exe` again while it runs.

While the window is open or opening, messages EDSense would show in a box appear in the window instead.

The window is light or dark as Windows is, or as you pick under **Advanced -> Appearance**. It needs Microsoft's WebView2 Runtime, see [What you need](setup.md#what-you-need). How the window works and what it may do: [The window](how-it-works.md#the-window).

The first time, the window shows a first run in place of its pages, see [Install](setup.md#install).

### Home

It shows what EDSense does now: the connection to DSX or DS4Windows, the controller, the haptics, the gyro, Elite and the HUD reader, and under **Activity** what happened lately. It has **Pause effects**, **Play demo** and **Calibrate gyro**.

### Controller

It picks the controller app, shows where EDSense sends and who answers, and checks how DSX or DS4Windows is set up, with what to do for each problem. Its profile cards install or reset EDSense's profile for Elite in DSX or DS4Windows, and open the folders with the copies EDSense keeps. Some settings wait for **Apply now**. More: [The Controller page](how-it-works.md#the-controller-page).

### Feel

The haptics: on or off, their strength, and the mode (**Auto** or **Rumble**). Then the turn and jump feels, the weapon feel of each fire group on R2 and L2 (the group in use is marked **Now**), how long multi-cannons spin up by hardpoint size, and the level of each effect, in six groups with a filter. More: [What you feel](feel.md).

### Triggers

The adaptive triggers on or off, then the resistance of R2 and L2 in each situation, in three cards: **In the ship**, **Both triggers** (interdiction, hits and jet cone boosts, one setting for both triggers) and **SRV and on foot**. Each trigger has a mode and its values, with a line under it that says what they do.

### Lights

The lightbar, the player LEDs and the mic LED on or off, and the lightbar's brightness. The colour of each lightbar state is set with a colour picker or a hex code such as `#ff1e1e`.

### Gyro aim

**Gyro aim** and **EDSense gyro**, as in the tray, then how far the mouse moves sideways and up and down (linked, or each on its own), roll, and slow movement. **Calibration** has **Calibrate gyro** and the drift EDSense knows. **Gyro off in menus** picks the panels where the gyro stops. With DS4Windows, the page also shows the profile's gyro and motion checks. More: [Gyro aim](feel.md#gyro-aim).

### HUD reader

The HUD reader on or off, with what it reads now, and the HUD colours for EDHM themes and filters: an empty colour is automatic. **Save HUD captures** asks first, and **Open folder** opens the `hud_debug` folder they go to. More: [HUD reader](how-it-works.md#hud-reader).

### Advanced

The journal and bindings folders (empty for Elite's standard ones), the update interval, and the rumble fallback: the left and right strength and the length of each effect. Then **Appearance**, **Open edsense.json** and **Open log**, and **Reset all settings...**.

### Changing settings

Changes on the window's pages apply at once and are saved in `edsense.json`. A slider saves when you let go of it, a text field when you press Enter or leave it. The journal and bindings folders, the update interval and the two ports wait for **Apply now**, on the Controller and Advanced pages. A change EDSense refuses, such as a folder that is not there, shows under its control.

On Feel, Triggers, Lights, Gyro aim, HUD reader and Advanced, each card of settings has **Reset**, which asks first, then puts the card's settings back to their defaults. The groups of **Effect levels** and **Rumble fallback** have **Reset group**. **Reset all settings...** on **Advanced** does it for every page. The controller app stays as it is, and the ports, the folders and the update interval wait for **Apply now**.

`edsense.json` still works in Notepad, and a change there shows on the pages about 2 s after you save. A value a control does not take, such as an effect level above 200%, shows as the nearest one, with a line that says what the file has. The file keeps it until you change that setting. While `edsense.json` has an error, the pages show it and change nothing. Every setting: [Settings](settings.md).

### About

It shows the version and whether the exe's signature is valid. Windows checks the signature offline. The links on the page open in your browser when you click them.

## The tray icon

EDSense's icon sits in the Windows tray, next to the clock (you may need to click the ^ arrow).

- Orange: EDSense drives the controller, or plays the demo.
- Grey: EDSense waits for Elite, you are in the main menu, or EDSense is paused.
- Red: DSX (or DS4Windows) does not answer. See [Troubleshooting](troubleshooting.md).

Its tooltip and the first line of its menu say what EDSense does now. Click the icon to open the window. Right-click it for the menu.

## The tray menu

- **Open EDSense**: opens the window.
- **Pause effects**: gives the controller back to your DSX (or DS4Windows) profile until you untick it, see [Pausing](#pausing).
- **Play demo**: plays every effect once, see [The demo](#the-demo).
- **Gyro aim**: untick it to fly with the sticks only. Also on the window's **Gyro aim** page.
- **EDSense gyro**: also on the window's **Gyro aim** page. Ticked, EDSense turns the controller's motion into mouse movement for Elite; unticked, DSX's gyro aims, as before. Both feel the same by default. EDSense only takes over motion to mouse, as in the bundled DSX profile: a gyro set to a stick or keys in DSX is left alone. With DS4Windows, unticked only turns EDSense's gyro off: DS4Windows' own gyro aims only when its profile uses it, see [The gyro](ds4windows.md#the-gyro). More: [Gyro aim](feel.md#gyro-aim).
- **Calibrate gyro...**: teaches EDSense the controller's drift, see [Calibrating the gyro](#calibrating-the-gyro).
- **Open settings**: opens `edsense.json` in Notepad. Most changes apply about 2 s after you save, see [Settings](settings.md#edsensejson).
- **Open log**: opens `edsense.log` in Notepad. Look here first when something does not work, see [Troubleshooting](troubleshooting.md).
- **Controller app**: **Auto**, **DSX** or **DS4Windows**, the `backend` setting, as on the window's **Controller** page. It applies at once: the controller goes back to your profile for about a second while EDSense switches.
- **Reset DSX profile...**: puts the bundled "Elite Dangerous" profile back in DSX. Yours is backed up. Shown only with DSX, and only while the window cannot open (no WebView2 Runtime). Otherwise **Reset...** on the **Controller** page's **DSX profile** card does it.
- **Quit**: gives the controller back and closes EDSense.

## Pausing

**Pause effects**, in the tray menu or on the window's **Home** page, gives the controller back to your DSX (or DS4Windows) profile until you untick it or press **Resume effects**. The gyro goes back to your profile too.

The controller also goes back to your profile in the main menu, when Elite closes and when you quit EDSense.

## The demo

**Play demo**, in the tray menu or on the window's **Home** page, plays every effect once. Elite does not need to run. While it plays, the button on the **Home** page shows the step and stops the demo. `.\EDSense.exe -demo` does the same from PowerShell.

To compare the turn and jump feels, run `.\EDSense.exe -feeltest`, see [Trying the feels](feel.md#trying-the-feels).

## Calibrating the gyro

Gyros drift a little. EDSense learns the drift by itself whenever the controller lies still (**Calibrate by itself** on the window's **Gyro aim** page). To teach it on request, start Elite, then:

- Tray menu -> **Calibrate gyro...**: put the controller down on a flat surface and let go, press **Yes**, and leave it still for 2 seconds.
- Or **Calibrate gyro** on the window's **Home** or **Gyro aim** page: put the controller down, press **Start**, and leave it still for 2 seconds.

How gyro aim works, and its settings: [Gyro aim](feel.md#gyro-aim).
