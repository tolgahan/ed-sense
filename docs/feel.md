# What you feel

Back to the [README](../README.md).

EDSense follows the game through the journal, `Status.json` and, if it is on, the [HUD reader](how-it-works.md#hud-reader). Effects marked (HUD) need the HUD reader.

Elite does not report firing, thrust, boost or turning, so EDSense reads them from the controller: R2, L2, R1 (thrust), Circle (boost, when your preset has no boost binding), the sticks, the gyro and the touchpad.

In the main menu, when the game closes, when you pause EDSense and when it quits, the controller goes back to your DSX profile.

## Adaptive triggers

| Situation | R2 and L2 |
|---|---|
| Flying, hardpoints in | no resistance |
| Hardpoints out | a weapon click on both. R2 fires primary, L2 secondary |
| Every weapon on a trigger reloading (HUD) | that trigger is slack until a clip is back |
| Firing with the weapons capacitor empty (HUD) | both slack until WEP is back to 30% |
| Hardpoints out and overheating | both vibrate |
| Analysis mode, hardpoints out | light resistance, for the scanners |
| Attacked (Elite logs this only now and then) | a short buzz |
| Jet cone boost | a buzz for 1 s |
| Being interdicted | a strong vibration |
| FSD charging, hyperspace | no resistance |
| Docked, landed, or a panel or map open | no resistance |
| On foot (not in social spaces, stations or hangars) | R2 a weapon click, L2 light resistance for aiming |
| SRV turret | R2 a weapon click, L2 light resistance |

Vibration is felt only while the trigger is pulled past the point where the effect starts. The window's **Triggers** page changes the mode and strength of each one; they are listed in [settings](settings.md#triggers).

## Lightbar

The base colour:

- In a ship: your hull. Green at 100%, amber at 50%, red at 20% and below.
- On foot: the same colours for your health. Low oxygen blinks blue.
- SRV: teal.
- Supercruise: blue.
- Hyperspace countdown and jump: blue-white, breathing.
- Docked or landed: a dim pale blue-white.

When not on foot, docked or landed, these go on top (a later one wins):

1. Combat (combat music, or "in danger"): the colour breathes.
2. Fuel scooping: amber, breathing.
3. Silent running: the lightbar is off.
4. Shields down: red blinks (not in supercruise or hyperspace).
5. FSD charging: white, rising over 5 s.
6. Overheating: orange blinks.
7. Being interdicted: magenta, breathing.

Short flashes: hull damage, lost shields and attacks (red), shields back up (blue), kills, bounties and combat bonds (green), heat warnings (orange), jet cone boost (cyan), interdiction (magenta), docking granted (green) or denied (red-orange), death (red, 5 s).

The window's **Lights** page changes these colours and the lightbar's brightness.

## Player LEDs and mic LED

- Player LEDs, in a ship: your fire group, 1 to 5 LEDs.
- Hyperspace jump: 5 LEDs down to 1, from the start of the jump countdown until you arrive (about 18 s).
- On foot and in the SRV: off.
- Mic LED: pulses on low fuel, on in silent running.

## Haptics

EDSense plays its own waveforms on the haptic channels of DSX's virtual DualSense (its audio device). Left and right are separate, and each effect has its own texture. Without that audio device EDSense falls back to rumble, and DSX turns the rumble into haptics. The turn feel needs the native haptics.

**Weapons**

- R2 is felt on the right and L2 on the left, with the feel of the weapons on that trigger. Beam lasers hum. Pulse and burst lasers pulse. Multi-cannons whir while they spin up, then rattle. Cannons, fragment cannons and plasma thump at their own rate. Mining lasers buzz. Railguns charge while held and crack on release. Missiles thump once per press.
- Which weapons a trigger fires comes from the fire groups you set (**Feel -> Fire groups** in the window, or `fire_groups` in the [settings](settings.md#fire-groups)), from the HUD's fire group lists, or from a guess based on your loadout. The HUD lists are remembered for each fire group (see [how reliable it is](how-it-works.md#how-reliable-it-is)).
- Reloading weapons fall silent (HUD). A clip dropping out is a double clack, a clip seating a heavy thunk.
- A trigger with only utilities: heat sink, chaff, shield cell, ECM and limpets are felt on a press, and scanners hum while held.
- Your shots landing (HUD): a small tick on the side you fire with, deeper once the target's shields are down, and a shatter when they collapse.

**Flying**

- Thrust (R1 held): a light rumble.
- Boost: a surge. A hollow dud when the engine capacitor is too low (HUD). EDSense learns how much charge your ship needs.
- A sudden flick of the controller: a soft push, outside the blue zone.
- Heat: a slow throb above about 40% that speeds up as heat rises, and boiling when overheating. With the HUD, a thud at 60% and at every 10% after. Without it, heat is estimated.
- Planets: hitting the atmosphere, the glide, leaving it, and the ground rushing up when you drop fast below 2500 m.
- One-shots: flight assist, FSD ready, mass lock, supercruise entry and drop, hardpoints, landing gear, cargo scoop, silent running, pips, fire group, target lock, docking, touchdown, liftoff, SRV and fighter launch.
- Also: fuel scooping, interdiction, cargo scooped and ejected, limpets, the FSS honk, jet cone boost, and messages from players or your wing.

**Combat**

- Shields down and back up, hull hits, kills, a cockpit breach, death. A low rattle while the shields are down.
- With the HUD: each shield hit is a crackle on the side that was hit, and sustained fire a sizzle. Low shields (40% or less) crackle, refilling shields tick. Each hull drop is a heavy metallic blow, and below 30% the hull creaks.
- Scanned by another ship: a scan line sweeps from the left grip to the right.
- Thargoids: a slow throb while their music plays. Their shutdown field powers everything down, lights and triggers too, and after about 30 s the systems come back.

**On foot**: a shot on the right for each R2 press, repeating while held (kinetic, laser or plasma). Not in social spaces or stations.

## Turns

**Turn feel** under **Feel -> Turns and jumps** in the window, or `turn_feel` in `edsense.json`:

| Value | Feel |
|---|---|
| `"waves"` (default) | a soft swell about every 2 seconds while the ship turns, stronger the harder the turn |
| `"push"` | a soft push when a turn starts, changes or ends, quiet while it holds |
| `"off"` | nothing |

- The turn comes from a stick bound to yaw, pitch or roll (the left stick without a custom preset) or from gyro aim.
- Felt only outside the throttle's blue zone (HUD). With the HUD reader off, turns are felt everywhere.
- Not felt in the hyperspace tunnel, in panels, docked or landed.
- Glides out when the turn ends, slower with flight assist off. With flight assist off every turn is felt a little more.
- Steps back while another effect plays and while you fire.
- Its level is **Turns** under **Feel -> Effect levels** (`maneuver` in `haptics_gain`). The flick push is **Flick** (`maneuver_kick`).

### Gyro turns

Gyro aim reaches Elite as mouse movement, and Elite turns the mouse into a virtual stick. EDSense estimates that stick from how far the controller rotates, turning sideways and rolling (60% of the roll, `gyro_roll_mix`) for sideways, tilting for up and down. It assumes full deflection at 20 degrees with `gyro_sensitivity_x` and `_y` at 1.

- **Relative Mouse** off in Elite (no mouse decay): a controller held tilted keeps the ship turning, so the turn feel stays until you turn the controller back or press the mouse reset key. The estimate fades by itself over about a minute, in case it went wrong.
- **Relative Mouse** on (mouse decay): the virtual stick springs back, so a gyro turn is felt while the controller moves. Without a custom preset EDSense assumes this.
- If the mouse turns nothing in your preset, the gyro part is off.
- While a finger rests on the touchpad (the bundled DSX profile pauses the gyro then) or mouse headlook has the mouse, moving the controller turns nothing. A turn you already hold goes on.

### Gyro aim

EDSense turns the controller's rotation into mouse movement for Elite, the way DSX's motion to mouse does, with the same strength and low-speed handling as the bundled DSX profile. While it does, DSX's own motion to mouse is off. Untick **EDSense gyro** in the tray or on the window's **Gyro aim** page (or set `"gyro_by": "dsx"`) to let DSX do it again. With DS4Windows, unticking only stops EDSense's gyro, see [The gyro](ds4windows.md#the-gyro). EDSense only replaces motion to mouse: if the DSX profile Elite uses has the gyro on a stick, on keys or off, EDSense leaves it alone, and the log says so.

- It moves the mouse only while Elite is in front, you are not in a menu, and no finger rests on the touchpad.
- Turning the controller sideways moves the mouse sideways, and rolling it adds 60% of the roll (`gyro_roll_mix`). Tilting it moves the mouse up and down. **Sideways** and **Up and down** on the **Gyro aim** page (`gyro_sensitivity_x` and `gyro_sensitivity_y`) scale each, and **Roll** sets how much the roll adds.
- Very slow movement (under about 1 degree per second) moves nothing, as with DSX. **Slow movement: Exact** on the **Gyro aim** page (`"gyro_low_speed": "exact"`) lets every bit through: finer aim, but the controller must be well calibrated. With DS4Windows, slow movement reaches EDSense only through DS4Windows' UDP server: its virtual DualSense drops every turn under 2 degrees per second, see [The gyro](ds4windows.md#the-gyro).
- Gyros drift a little. EDSense learns the drift whenever the controller lies still for 2 seconds (at once in menus and loading screens, more carefully while you fly) and remembers it in `gyro_calibration.json` (with DS4Windows, one file for its virtual DualSense and one for its UDP server). **Calibrate gyro...** in the tray, or **Calibrate gyro** on the window's **Home** or **Gyro aim** page, does it on request while Elite runs, in menus and paused too; for those 2 seconds DSX's gyro does not move the mouse. See [Calibrating the gyro](using.md#calibrating-the-gyro).
- **Pause effects** gives the gyro to your DSX profile until you untick it. So do Elite closing and quitting EDSense.
- If Elite runs as administrator, Windows keeps EDSense's mouse movement from it, and DSX's gyro aims instead. If no motion data arrives, DSX's gyro aims too. The log says so.
- `.\EDSense.exe -gyrotest` compares EDSense's gyro with DSX's and prints the settings that match your DSX profile (see [settings](settings.md#command-line-options)).

**Gyro aim off**: untick **Gyro aim** in the tray or on the window's **Gyro aim** page, or set `"gyro_aim": false`. While Elite runs, the gyro moves nothing (EDSense's and DSX's), and the turn feel follows the sticks. Your DSX profile's gyro setting comes back when Elite closes, or when EDSense pauses or quits.

**Gyro off in menus**: in the main menu, the side panels, station services, the maps, the orrery and the codex, the gyro stops, so the cursor stays still. The FSS and the surface scanner keep the gyro. Change this under **Gyro aim -> Gyro off in menus** in the window, or with `gyro_off_in_menus` and `gyro_off_gui_focus`. The pause (Esc) menu can't be detected, because Elite writes nothing for it, so the gyro still moves the cursor there.

## Jumps

**Jump feel** under **Feel -> Turns and jumps** in the window, or `jump_feel` in `edsense.json`:

| Value | Feel |
|---|---|
| `"swell"` (default) | one soft swell into the hyperspace tunnel, then quiet for the rest of the jump |
| `"calm"` | the swell, plus a soft pulse every second while the FSD charges |
| `"off"` | nothing |

There is no thump at the start of a jump, and the FSD charge does not vibrate the triggers. The lightbar rises white and the player LEDs count down. The levels are **Hyperspace swell** and **FSD charge** under **Feel -> Effect levels** (`hyperspace` and `fsd_charge` in `haptics_gain`).

## Trying the feels

- Close Elite, open PowerShell in the EDSense folder and run `.\EDSense.exe -feeltest`. It plays four plain tones (80, 120, 170 and 250 Hz), then the turn feels `"waves"` and `"push"`, then the jump feels `"swell"` and `"calm"`. It takes about a minute and needs native haptics.
- **Play demo** in the tray or on the window's **Home** page, or `.\EDSense.exe -demo`, plays every effect once, see [The demo](using.md#the-demo).
- Each effect's level is under **Feel -> Effect levels** in the window: 100% is normal, 0% turns it off. In `edsense.json` it is `haptics_gain`: 1 is normal, 0.5 is half, 0 turns it off. See [settings](settings.md).
