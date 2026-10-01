# Setup

Back to the [README](../README.md).

## What you need

- Windows, Elite Dangerous, and a DualSense or DualSense Edge.
- DSX v3.1 or newer (tested with v3.2.0 BETA 02), or DS4Windows 5, see [Get DS4Windows 5](ds4windows.md#get-ds4windows-5).
- Microsoft's WebView2 Runtime, for the window. It comes with Windows 11 and nearly every Windows 10 PC. If it is missing, EDSense offers to open Microsoft's download page. It never downloads anything itself, and the tray menu keeps working without the window.

## Install

1. Download the zip from the [latest release](https://github.com/tolgahan/ed-sense/releases/latest).
2. Unzip the EDSense folder somewhere you can write to, such as Documents. Do not use Program Files. EDSense keeps its settings and log next to the exe, or in `%APPDATA%\EDSense` when it cannot write there.
3. Run `EDSense.exe`. The exe is code-signed with a Certum certificate. The certificate is new, so Windows SmartScreen may still warn the first time: **More info** shows the verified publisher, then **Run anyway**. To check the download yourself, see [Checking a download](how-it-works.md#checking-a-download).

The first time, the window asks which controller app you use and checks how it is set up. Its steps are **Welcome**, **Your controller app**, **Set it up** (the checks of that app, with its profile card), **Gyro aim** and **Ready**, which has **Play demo** and **Finish**. **Finish** saves your choices. **Set up later** skips the steps and saves **Auto**. The first run does not come back, and the **Controller** page has the same checks. More: [The first run](how-it-works.md#the-first-run).

Then set up the controller app you use: [DSX](#with-dsx) or [DS4Windows](#with-ds4windows).

## With DSX

1. In DSX, open **Settings -> Networking** and turn on **Incoming UDP**.
2. Add EDSense's "Elite Dangerous" profile to DSX. It comes with gyro aim, touchpad and triggers set up. Press **Install...** in the first run's **Set it up** step, or on the **DSX profile** card of the window's **Controller** page. Once the first run is over and EDSense uses DSX, it also adds the profile by itself, unless you pressed **Install...** or **Cancel** on the card before.
3. DSX saves its profiles when it exits, so EDSense writes the profile only while DSX is closed. If DSX is running, close it when EDSense asks (DSX tray icon -> **Exit**).
4. When EDSense says the profile is in DSX ("Start DSX again to use it"), start DSX again.

- An "Elite Dangerous" profile you already have stays as it is, and so does a profile you set for Elite. **Reset...** on the **DSX profile** card replaces it, see [DSX profile](settings.md#dsx-profile).
- If you keep your own profile, it needs at least **DualSense Emulation** as its virtual device. The full checklist: [DSX profile](settings.md#dsx-profile).

## With DS4Windows

DS4Windows 5 works in place of DSX. Its game mod support takes the triggers and lights, and its virtual DualSense the haptics and the gyro.

1. Get DS4Windows 5 and set it up: [Get DS4Windows 5](ds4windows.md#get-ds4windows-5) and [Set up DS4Windows](ds4windows.md#set-up-ds4windows).
2. Pick **DS4Windows** in the first run, on the window's **Controller** page, or in the tray menu under **Controller app -> DS4Windows**. It applies at once. With **Auto**, EDSense uses the app that runs, and switches by itself when you change apps while it is not driving the controller.
3. The window can set DS4Windows up for Elite. **Install...** on the **DS4Windows profile** card adds a profile with DualSense emulation and the gyro and touchpad passed through, loaded whenever Elite is in front, and turns game mod support on. It writes while DS4Windows is closed (its tray icon -> **Exit**) and keeps copies of the files it changes. See [Let EDSense set up DS4Windows](ds4windows.md#let-edsense-set-up-ds4windows).

The DS4Windows settings, the profile and the gyro rules are in [DS4Windows](ds4windows.md).

## What EDSense writes in DSX and DS4Windows

In DSX's and DS4Windows' settings, EDSense writes only its own profile for Elite: with DSX also Elite's game profile entry, with DS4Windows also its Auto Profiles rule and the game mod setting. It writes only while that app is closed, and copies the files it changes first. The full list of what it reads and writes: [How it works](how-it-works.md#what-edsense-reads-and-writes).

## Play

Run Elite borderless or windowed, so EDSense can read the HUD. EDSense waits in the tray and switches on by itself when you are in the game. **Play demo** plays every effect once, without Elite. The window, the tray menu and the gyro: [Using EDSense](using.md).

## Start with Windows

Put a shortcut to `EDSense.exe` in your Startup folder (Win+R, `shell:startup`). Add ` -tray` at the end of the shortcut's **Target**, so EDSense starts in the tray without opening its window.

A shortcut you made for an older version needs the same ` -tray`, or the window now opens at every sign-in.

## Uninstall

1. Tray menu -> **Quit**.
2. Delete the EDSense folder, and `%APPDATA%\EDSense` if it exists. The window keeps a cache in `%LOCALAPPDATA%\EDSense`: delete that too.
3. Delete your Startup shortcut if you made one. Versions up to 0.4.2 could add themselves to startup: turn that off in **Task Manager -> Startup apps**.
4. If you like, delete the "Elite Dangerous" profile in DSX. If EDSense set up DS4Windows, remove its Auto Profiles rule and the "Elite Dangerous (EDSense)" profile there: [how to undo it](ds4windows.md#let-edsense-set-up-ds4windows).
