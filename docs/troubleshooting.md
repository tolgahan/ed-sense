# Troubleshooting

Back to the [README](../README.md).

Open the log first (tray -> **Open log**). It says what EDSense found and what is missing. The window's **Controller** page also checks how DSX or DS4Windows is set up, and says what to do for each problem.

The `.\EDSense.exe` checks below run in PowerShell in the EDSense folder, see [Command-line options](settings.md#command-line-options).

## DSX

- **Red tray icon**: DSX is closed, or **Settings -> Networking -> Incoming UDP** is off.
- **No haptics**: set the DSX profile's virtual device to **DualSense Emulation**, with **Haptic Motors** on. Without it the log says `no virtual DualSense audio device`. `.\EDSense.exe -padtest` and `.\EDSense.exe -hapticstest` check the connection.
- **Elite sees no controller**: see [DSX Native Mode](settings.md#dsx-native-mode).

## Elite

- **No shield or heat effects, or turns felt in the blue zone**: run Elite borderless or windowed. `.\EDSense.exe -hudtest <screenshots>` reads the HUD from your F10 screenshots, see [Checking with screenshots](how-it-works.md#checking-with-screenshots). For a recoloured HUD, see [HUD colours](how-it-works.md#hud-colours).
- **Heat sink, chaff, shield cell or boost not felt**: EDSense needs a custom control preset. Without one the log says `is a built-in preset`. Change any binding in Elite once and Elite saves one.
- **Journal folder not found**: type your journal folder under **Advanced -> Folders** in the window (or set `journal_dir`), then press **Apply now**, or restart EDSense.

## Feel

- **A trigger feels like the wrong weapon**: set it per fire group under **Feel -> Fire groups** in the window, or with [fire_groups](settings.md#fire-groups).
- **A feel is too much**: on the window's **Feel** page, pick **Push** or **Off** for **Turn feel**, **Off** for **Jump feel**, or lower **Turns**, **Hyperspace swell** or **FSD charge** under **Effect levels**. In `edsense.json`: `"turn_feel": "push"` or `"off"`, `"jump_feel": "off"`, or lower `maneuver`, `hyperspace` or `fsd_charge` in `haptics_gain`. See [Turns](feel.md#turns) and [Jumps](feel.md#jumps).

## Gyro aim

- **The ship turns by itself with the controller on the desk**: tray -> **Calibrate gyro...** (or **Calibrate gyro** on the window's **Home** or **Gyro aim** page), or keep the controller still for 2 seconds in a menu.
- **Gyro aim does nothing, or moves twice as far**: look for lines starting with `Gyro:` in the log, and run `.\EDSense.exe -gyrotest`. With DSX, unticking **EDSense gyro** in the tray or on the **Gyro aim** page gives you DSX's gyro back.
- **With DS4Windows, slow aim does nothing and faster turns jump**: DS4Windows' virtual DualSense drops turns under 2 degrees per second. Turn on **Settings -> UDP Server -> Enable Server** in DS4Windows, or press **Install...** on the DS4Windows profile card. The log then says `Gyro: reading the motion from DS4Windows' UDP server`. If the Controller page says the server sends no motion for the controller, press **Stop** and **Start** in DS4Windows. See [The gyro](ds4windows.md#the-gyro).
- **With DS4Windows, unticking EDSense gyro leaves no gyro aim**: EDSense cannot switch DS4Windows' gyro on. For DS4Windows' own gyro aim, set the profile's **Gyro -> Output Mode** to **Mouse**.
- **Gyro turns not felt**, and the log says `no motion data`: turn on **Passthrough** on DSX's Motion page, or run `.\EDSense.exe -gyrotest`.

## The window

- **The window does not open**: it needs Microsoft's WebView2 Runtime. EDSense offers to open Microsoft's download page, and the tray menu works without the window. See [What you need](setup.md#what-you-need).
- **The window opens at every sign-in**: add ` -tray` at the end of your Startup shortcut's **Target**, see [Start with Windows](setup.md#start-with-windows).

## With DS4Windows

See [When something does not work](ds4windows.md#when-something-does-not-work) in the DS4Windows doc.
