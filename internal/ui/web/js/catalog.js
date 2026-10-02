// The words the pages show for each setting: labels, help and choices.
// Types, ranges, defaults and restart flags come from Go (settings.schema).
// Every key is written as a quoted string, so web_test.go can find it.

// The lead line under each page's title.
export const PAGE_LEADS = {
  "feel": "How the controller's haptics answer the game.",
  "triggers": "The resistance of R2 and L2 in each situation.",
  "lights": "The lightbar, the player LEDs and the mic LED.",
  "gyro": "Aim by turning the controller.",
  "hud": "Reads shields, heat and fire groups from the cockpit HUD on your screen.",
  "advanced": "Folders, timing, the rumble fallback, the window and the settings file.",
};

// The notes under a page's lead while its settings do nothing now
// (values.gyroNote and values.hudNote).
export const PAGE_NOTES = {
  "gyro": "This controller connection passes no gyro, so these settings do nothing now.",
  "hud": "The screen cannot be read on this PC, so the HUD reader does not run here.",
};

// The two choices of haptics_mode. "native" works as "auto", so it shows
// as Auto.
export const HAPTICS_MODES = [["auto", "Auto"], ["rumble", "Rumble"]];
export const MODE_ALIAS = { "native": "auto" };

const HAPTICS_MODE_HELP = {
  "auto": "Auto: native haptics when the controller's audio works, else rumble.",
  "rumble": "Rumble: always rumble.",
};
// With DS4Windows, rumble would mute the native haptics, so Auto never
// falls back to it.
const HAPTICS_MODE_DS4W_HELP = {
  "auto": "Auto: native haptics when the controller's audio works, else no haptics. With DS4Windows, EDSense rumbles only in Rumble mode.",
};
const TURN_FEEL_HELP = {
  "waves": "Waves: a soft swell about every 2 seconds while the ship turns.",
  "push": "Push: a soft push when a turn starts, changes or ends.",
  "off": "Off: nothing.",
};
const JUMP_FEEL_HELP = {
  "swell": "Swell: one soft swell into the hyperspace tunnel.",
  "calm": "Calm: the swell, and a soft pulse every second while the FSD charges.",
  "off": "Off: nothing.",
};
const LOW_SPEED_HELP = {
  "dsx": "Like DSX: very slow movement (under about 1 degree per second) moves nothing, and slow movement a little less, as with DSX.",
  "exact": "Exact: every bit of rotation moves the mouse; finer aim, but the controller must be well calibrated.",
};
const GYRO_BY_HELP = {
  "dsx": "On: EDSense turns the controller's motion into mouse movement, and DSX's motion to mouse is off while Elite runs. Off: DSX does it, as before. Same as the tray's EDSense gyro.",
  "ds4windows": "On: EDSense aims while the DS4Windows profile leaves the gyro alone (Gyro > Output Mode Passthru), and reads the motion from DS4Windows' UDP server (Settings > UDP Server > Enable Server). Off: DS4Windows' own gyro aims only if its profile uses it. Same as the tray's EDSense gyro.",
};

// SETTINGS: every Schema key but config_version, by its name in the file.
// A map's entry is the card's: its rows take their words from the tables
// below. Extra fields:
// - options: a choice row's [value, label] list; alias: file values shown
//   as another choice;
// - byValue: the help for each value, in place of help;
// - byKind: the help by the controller app (status.kind); "dsx" also
//   when the app is not known;
// - ds4windowsByValue: the help for a value while the app is DS4Windows,
//   in place of byValue's;
// - ds4windows: a line added to the help while the app is DS4Windows;
// - udpSilent: a line added while DS4Windows' UDP server does not answer;
// - entryHelp: the help of each row of a map.
export const SETTINGS = {
  "backend": { page: "controller", card: "Controller app", label: "Controller app", help: "Auto uses the one that runs. With both, the one that answers." },
  "journal_dir": { page: "advanced", card: "Folders", label: "Journal folder", help: "Where Elite writes its journal and Status.json. Empty: Elite's standard folder. Applies with Apply now." },
  "bindings_dir": { page: "advanced", card: "Folders", label: "Bindings folder", help: "Where Elite keeps your control presets. Empty: Elite's standard folder. Applies with Apply now." },
  "dsx_port": { page: "controller", card: "Connection", label: "DSX port", help: "0 reads DSX's port file (6969 when there is none)." },
  "ds4windows_port": { page: "controller", card: "Connection", label: "DS4Windows port", help: "0 uses DS4Windows' own setting (127.0.0.1:6969 when it has none)." },
  "poll_ms": { page: "advanced", card: "Timing", label: "Update interval", help: "How often EDSense updates the controller, in ms. 25 is the default. Applies with Apply now." },
  "control_lightbar": { page: "lights", card: "What EDSense sets", label: "Lightbar", help: "Off leaves the lightbar to your DSX or DS4Windows profile." },
  "control_triggers": { page: "triggers", card: "Adaptive triggers", label: "Adaptive triggers", help: "Off leaves the triggers to your DSX or DS4Windows profile." },
  "control_player_leds": { page: "lights", card: "What EDSense sets", label: "Player LEDs", help: "Your fire group, and a countdown through a jump. Off leaves them to your profile." },
  "control_mic_led": { page: "lights", card: "What EDSense sets", label: "Mic LED", help: "Pulses on low fuel, and is on in silent running. Off leaves it to your profile." },
  "control_haptics": { page: "feel", card: "Haptics", label: "Haptics", help: "Off: no haptics. EDSense then stops reading the controller too, so the trigger slack on an empty weapons capacitor stops as well." },
  "haptics_strength": { page: "feel", card: "Haptics", label: "Strength", help: "All effects together, rumble too. 100% is normal." },
  "haptics_mode": {
    page: "feel", card: "Haptics", label: "Mode", help: HAPTICS_MODE_HELP["auto"],
    options: HAPTICS_MODES, alias: MODE_ALIAS, byValue: HAPTICS_MODE_HELP, ds4windowsByValue: HAPTICS_MODE_DS4W_HELP,
  },
  "ds4windows_haptics": { page: "controller", card: "Haptics with DS4Windows", label: "Haptics with DS4Windows", help: "Where native haptics play. Auto: the controller's own audio device when it is wired, else the virtual DualSense's." },
  "haptics_gain": { page: "feel", card: "Effect levels", label: "Effect levels", help: "The level of each effect. 100% is normal, 0% turns it off." },
  "turn_feel": {
    page: "feel", card: "Turns and jumps", label: "Turn feel", help: TURN_FEEL_HELP["waves"],
    options: [["waves", "Waves"], ["push", "Push"], ["off", "Off"]], byValue: TURN_FEEL_HELP,
  },
  "jump_feel": {
    page: "feel", card: "Turns and jumps", label: "Jump feel", help: JUMP_FEEL_HELP["swell"],
    options: [["swell", "Swell"], ["calm", "Calm"], ["off", "Off"]], byValue: JUMP_FEEL_HELP,
  },
  "fire_groups": {
    page: "feel", card: "Fire groups", label: "Fire groups",
    help: "The weapon feel per fire group. Auto uses what the HUD showed for that group, else a guess from your loadout. A group not listed here is Auto.",
    entryHelp: "R2 and L2: Auto, or a weapon type.",
  },
  "spin_up_ms": { page: "feel", card: "Multi-cannon spin-up", label: "Multi-cannon spin-up", help: "How long multi-cannons spin up before they fire, by hardpoint size. If the rattle starts before or after your guns, change the size you fly." },
  "gyro_aim": { page: "gyro", card: "Gyro aim", label: "Gyro aim", help: "Move the controller to aim, as with a mouse. Off: the gyro moves nothing while Elite runs, and the turn feel follows the sticks. Same as the tray's Gyro aim." },
  "gyro_off_in_menus": { page: "gyro", card: "Gyro off in menus", label: "Gyro off in menus", help: "The gyro stops in the main menu and in the panels below, so the cursor stays still. The pause menu cannot be detected." },
  "gyro_by": { page: "gyro", card: "Gyro aim", label: "EDSense gyro", help: GYRO_BY_HELP["dsx"], byKind: GYRO_BY_HELP },
  "gyro_sensitivity_x": { page: "gyro", card: "Aim", label: "Sideways", help: "How far the mouse moves when you turn or roll the controller sideways. 1 matches DSX's bundled profile; 2 is twice as far. .\\EDSense.exe -gyrotest prints the values that match your DSX profile." },
  "gyro_sensitivity_y": { page: "gyro", card: "Aim", label: "Up and down", help: "How far the mouse moves when you tilt the controller." },
  "gyro_roll_mix": { page: "gyro", card: "Aim", label: "Roll", help: "How much rolling the controller turns sideways. 0% ignores roll." },
  "gyro_low_speed": {
    page: "gyro", card: "Aim", label: "Slow movement", help: LOW_SPEED_HELP["dsx"],
    options: [["dsx", "Like DSX"], ["exact", "Exact"]], byValue: LOW_SPEED_HELP,
    ds4windows: "Slow movement reaches EDSense only through DS4Windows' UDP server.",
    udpSilent: "DS4Windows' UDP server is off, so movement under 2 degrees per second is lost.",
  },
  "gyro_auto_calibrate": { page: "gyro", card: "Calibration", label: "Calibrate by itself", help: "Learn the gyro's drift whenever the controller lies still for 2 seconds." },
  "gyro_off_gui_focus": { page: "gyro", card: "Gyro off in menus", label: "Panels with the gyro off", help: "The main menu is always included." },
  "hud_reader": { page: "hud", card: "HUD reader", label: "HUD reader", help: "Reads shields, heat and fire groups from the cockpit HUD. Run Elite borderless or windowed: EDSense cannot read a fullscreen game." },
  "hud_debug": { page: "hud", card: "Debug captures", label: "Save HUD captures", help: "Saves small captures of the HUD to the hud_debug folder next to edsense.json, to report a problem. Turn it off when you are done." },
  "hud_colors": {
    page: "hud", card: "HUD colours", label: "HUD colours",
    help: "An empty colour is automatic: EDSense learns it from your screen, else uses your colour matrix, else Elite's standard colour. Set colours here for EDHM themes or filters.",
  },
  "lightbar_brightness": { page: "lights", card: "What EDSense sets", label: "Brightness", help: "The lightbar's brightness." },
  // five cards: COLOR_GROUPS
  "colors": { page: "lights", card: "", label: "Lightbar colours", help: "" },
  // three cards: TRIGGER_GROUPS
  "triggers": { page: "triggers", card: "", label: "Trigger situations", help: "" },
  "rumble": {
    page: "advanced", card: "Rumble fallback", label: "Rumble fallback",
    help: "The rumble of each effect: the strength of the left and right motor in %, and the length in ms. EDSense rumbles in Rumble mode, and with DSX when native haptics are not there. Continuous effects use only the strengths. A length of 0 turns a one-shot off.",
  },
};

// The effects of haptics_gain, in groups in the order of defaultGains();
// rumble has 69 of them.
export const EFFECT_GROUPS = [
  {
    title: "Weapons and flying",
    keys: ["fire_primary", "fire_secondary", "spin_up", "rail_crack", "missile_launch", "reload", "reload_done",
      "thrust", "thrust_low", "boost", "boost_empty", "maneuver", "maneuver_kick", "fa_off", "fa_on"],
  },
  {
    title: "Ship systems",
    keys: ["hardpoints", "landing_gear", "cargo_scoop", "silent_running", "pips", "fire_group", "target_locked",
      "heat_sink", "chaff", "shield_cell", "ecm", "scanner", "utility", "limpet",
      "heat_build", "heat_warning", "heat_damage", "heat_notch", "overheat"],
  },
  {
    title: "Travel",
    keys: ["fsd_charge", "fsd_ready", "hyperspace", "supercruise_in", "supercruise_out", "mass_lock", "mass_unlock",
      "interdicted", "interdiction", "interdiction_noise", "escaped", "jet_cone", "fuel_scoop", "honk"],
  },
  {
    title: "Planets and stations",
    keys: ["docked", "undocked", "touchdown", "liftoff", "vehicle", "docking_granted",
      "glide", "glide_low", "glide_start", "glide_end", "ground_rush"],
  },
  {
    title: "Combat",
    keys: ["shields_down", "shields_up", "shields_offline", "shield_hit", "shield_sizzle", "shield_low", "shield_regen",
      "hull_hit", "hull_hit_hud", "hull_creak", "hull_breach", "under_attack", "kill", "died", "scanned",
      "target_hit", "target_hull_hit", "target_shield_break"],
  },
  {
    title: "Cargo, Thargoids and on foot",
    keys: ["cargo_collect", "cargo_eject", "message", "thargoid", "thargoid_pulse", "systems_shutdown", "systems_reboot",
      "shot_kinetic", "shot_laser", "shot_plasma", "onfoot_shot"],
  },
];

// EFFECTS: [label, help] by key.
export const EFFECTS = {
  // Weapons and flying
  "fire_primary": ["Primary fire", "R2's weapons while you fire."],
  "fire_secondary": ["Secondary fire", "L2's weapons while you fire."],
  "spin_up": ["Multi-cannon spin-up", "The whir before multi-cannons fire."],
  "rail_crack": ["Railgun crack", "The crack when a railgun fires."],
  "missile_launch": ["Missile launch", "A thump for each missile press."],
  "reload": ["Clip out", "A double clack when a clip drops out (HUD)."],
  "reload_done": ["Clip in", "A heavy thunk when a clip seats (HUD)."],
  "thrust": ["Thrust", "A light rumble while R1 is held."],
  "thrust_low": ["Thrust, low layer", "The deep layer under thrust."],
  "boost": ["Boost", "The surge of a boost."],
  "boost_empty": ["Boost without charge", "A hollow dud when the engine capacitor is too low (HUD)."],
  "maneuver": ["Turns", "The turn feel."],
  "maneuver_kick": ["Flick", "A soft push on a sudden flick of the controller."],
  "fa_off": ["Flight assist off", "A click when flight assist goes off."],
  "fa_on": ["Flight assist on", "A click when flight assist comes back."],
  // Ship systems
  "hardpoints": ["Hardpoints", "Deploying or retracting the hardpoints."],
  "landing_gear": ["Landing gear", "Lowering or raising the landing gear."],
  "cargo_scoop": ["Cargo scoop", "Opening or closing the cargo scoop."],
  "silent_running": ["Silent running", "Silent running on or off."],
  "pips": ["Pips", "Moving power between systems."],
  "fire_group": ["Fire group", "Changing the fire group."],
  "target_locked": ["Target lock", "Locking a target."],
  "heat_sink": ["Heat sink", "Firing a heat sink."],
  "chaff": ["Chaff", "Firing chaff."],
  "shield_cell": ["Shield cell", "Using a shield cell."],
  "ecm": ["ECM", "Firing the ECM."],
  "scanner": ["Scanners", "A hum while a scanner is held."],
  "utility": ["Other utilities", "A click for the other utilities."],
  "limpet": ["Limpets", "Launching a limpet."],
  "heat_build": ["Heat", "A slow throb above about 40% heat that speeds up as heat rises."],
  "heat_warning": ["Heat warning", "Elite's heat warning."],
  "heat_damage": ["Heat damage", "Damage from heat."],
  "heat_notch": ["Heat steps", "A thud at 60% heat and at every 10% after (HUD)."],
  "overheat": ["Overheating", "Boiling while overheating."],
  // Travel
  "fsd_charge": ["FSD charge", "The soft pulse of the Calm jump feel."],
  "fsd_ready": ["FSD ready", "The FSD is ready to jump."],
  "hyperspace": ["Hyperspace swell", "The swell into the hyperspace tunnel."],
  "supercruise_in": ["Supercruise entry", "Entering supercruise."],
  "supercruise_out": ["Supercruise drop", "Dropping out of supercruise."],
  "mass_lock": ["Mass lock", "Mass locked."],
  "mass_unlock": ["Mass lock clear", "The mass lock ends."],
  "interdicted": ["Interdicted", "An interdiction pulls you out."],
  "interdiction": ["Interdiction", "The pull while you are interdicted."],
  "interdiction_noise": ["Interdiction noise", "The noise layer of the interdiction."],
  "escaped": ["Escaped", "Escaping an interdiction."],
  "jet_cone": ["Jet cone boost", "A jet cone boost."],
  "fuel_scoop": ["Fuel scooping", "While you scoop fuel."],
  "honk": ["FSS honk", "The discovery scanner's honk."],
  // Planets and stations
  "docked": ["Docked", "The docking clamps."],
  "undocked": ["Undocked", "Leaving the pad."],
  "touchdown": ["Touchdown", "Landing on a planet."],
  "liftoff": ["Liftoff", "Lifting off a planet."],
  "vehicle": ["SRV and fighter", "Launching or docking an SRV or a fighter."],
  "docking_granted": ["Docking granted", "Docking permission granted."],
  "glide": ["Glide", "The glide into a planet."],
  "glide_low": ["Glide, low layer", "The deep layer under the glide."],
  "glide_start": ["Atmosphere", "Hitting the atmosphere."],
  "glide_end": ["Glide end", "Leaving the glide."],
  "ground_rush": ["Ground rush", "Dropping fast below 2500 m."],
  // Combat
  "shields_down": ["Shields down", "The shields drop."],
  "shields_up": ["Shields up", "The shields come back."],
  "shields_offline": ["Shields offline", "A low rattle while the shields are down."],
  "shield_hit": ["Shield hit", "A crackle on the side that was hit (HUD)."],
  "shield_sizzle": ["Sustained fire", "A sizzle under sustained fire on the shields (HUD)."],
  "shield_low": ["Shields low", "A crackle at 40% shields or less (HUD)."],
  "shield_regen": ["Shields refilling", "Ticks while the shields refill (HUD)."],
  "hull_hit": ["Hull hit", "Hull damage from the journal."],
  "hull_hit_hud": ["Hull hit (HUD)", "A heavy blow on each hull drop the HUD shows."],
  "hull_creak": ["Hull creak", "Below 30% hull (HUD)."],
  "hull_breach": ["Cockpit breach", "The canopy breaks."],
  "under_attack": ["Under attack", "Elite says you are under attack."],
  "kill": ["Kill", "A kill, bounty or combat bond."],
  "died": ["Death", "Your ship is destroyed."],
  "scanned": ["Scanned", "A scan line from the left grip to the right when a ship scans you."],
  "target_hit": ["Your hits", "A tick when your shots land (HUD)."],
  "target_hull_hit": ["Your hull hits", "Deeper, once the target's shields are down (HUD)."],
  "target_shield_break": ["Target shields down", "A shatter when the target's shields collapse (HUD)."],
  // Cargo, Thargoids and on foot
  "cargo_collect": ["Cargo scooped", "Scooping cargo."],
  "cargo_eject": ["Cargo ejected", "Ejecting cargo."],
  "message": ["Messages", "Messages from players or your wing."],
  "thargoid": ["Thargoids", "A slow throb while their music plays."],
  "thargoid_pulse": ["Thargoid pulse", "The noise layer of that throb."],
  "systems_shutdown": ["Shutdown field", "A Thargoid shutdown field powers everything down."],
  "systems_reboot": ["Reboot", "The systems come back."],
  "shot_kinetic": ["On foot: kinetic", "Each shot of a kinetic weapon on foot."],
  "shot_laser": ["On foot: laser", "Each shot of a laser weapon on foot."],
  "shot_plasma": ["On foot: plasma", "Each shot of a plasma weapon on foot."],
  "onfoot_shot": ["On foot: rumble", "A shot on foot with the rumble fallback."],
};

// The lightbar colours, a card each: [label, help] by key.
export const COLOR_GROUPS = [
  {
    title: "Hull and health",
    keys: {
      "hull_full": ["Hull full", "Your hull at 100%, or your health on foot."],
      "hull_half": ["Hull half", "At 50%."],
      "hull_low": ["Hull low", "At 20% and below."],
    },
  },
  {
    title: "Travel",
    keys: {
      "supercruise": ["Supercruise", "The colour in supercruise."],
      "hyperspace": ["Hyperspace", "The jump countdown and the jump, breathing."],
      "fsd_charge": ["FSD charging", "Rises over 5 s while the FSD charges."],
      "fuel_scoop": ["Fuel scooping", "Breathing while you scoop fuel."],
      "interdiction": ["Interdiction", "Breathing while you are interdicted."],
      "jet_cone": ["Jet cone boost", "A flash on a jet cone boost."],
    },
  },
  {
    title: "Combat",
    keys: {
      "shields_down": ["Shields down", "Blinks while the shields are down, and flashes when they drop."],
      "shields_up": ["Shields up", "A flash when the shields come back."],
      "hit": ["Hit", "A flash on hull damage and attacks."],
      "kill": ["Kill", "A flash on kills, bounties and combat bonds."],
      "overheat": ["Overheating", "Blinks while overheating, and flashes on heat warnings."],
      "died": ["Death", "Blinks for 5 s when you die."],
    },
  },
  {
    title: "Docking",
    keys: {
      "docked": ["Docked", "A dim colour while docked or landed."],
      "docking_ok": ["Docking granted", "A flash."],
      "docking_no": ["Docking denied", "A flash."],
    },
  },
  {
    title: "SRV and on foot",
    keys: {
      "srv": ["SRV", "The colour in the SRV."],
      "low_oxygen": ["Low oxygen", "Blinks on foot when oxygen runs low."],
    },
  },
];

// The trigger situations, a card each. A row's keys are R2's then L2's;
// a row with one key sets both triggers.
export const TRIGGER_GROUPS = [
  {
    title: "In the ship",
    rows: [
      { label: "Hardpoints out", help: "R2 fires primary, L2 secondary.", keys: ["ship_weapons_r", "ship_weapons_l"] },
      { label: "Reloading", help: "Every weapon on that trigger reloading (HUD).", keys: ["ship_reload_r", "ship_reload_l"] },
      { label: "Weapons capacitor empty", help: "Firing with the weapons capacitor empty (HUD).", keys: ["ship_wep_empty_r", "ship_wep_empty_l"] },
      { label: "Overheating", help: "Hardpoints out and overheating.", keys: ["ship_overheat_r", "ship_overheat_l"] },
      { label: "Analysis mode", help: "Analysis mode, hardpoints out, for the scanners.", keys: ["ship_scanner_r", "ship_scanner_l"] },
    ],
  },
  {
    title: "Both triggers",
    rows: [
      { label: "Interdiction", help: "Being interdicted.", keys: ["interdiction"] },
      { label: "Hit", help: "Attacked: both triggers for 0.25 s.", keys: ["hit"] },
      { label: "Jet cone boost", help: "Both triggers for 1 s.", keys: ["jet_cone"] },
    ],
  },
  {
    title: "SRV and on foot",
    rows: [
      { label: "SRV turret", help: "The SRV's turret view.", keys: ["srv_turret_r", "srv_turret_l"] },
      { label: "On foot", help: "Outside social spaces, stations and hangars.", keys: ["onfoot_r", "onfoot_l"] },
    ],
  },
];

// The ten points of a "by position" mode, counted as Start and End are:
// 0 is the trigger at rest.
const POSITIONS = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"];

// The trigger modes: the label, and a label for each value. The value
// ranges are the Schema's (modes). group: the values from index "from"
// on sit in a group with this label.
export const MODES = {
  "OFF": { label: "Off", params: [] },
  "FEEDBACK": { label: "Feedback", params: ["Start", "Strength"] },
  "WEAPON": { label: "Weapon", params: ["Start", "End", "Strength"] },
  "VIBRATION": { label: "Vibration", params: ["Start", "Amplitude", "Frequency"] },
  "SLOPE_FEEDBACK": { label: "Slope", params: ["Start", "End", "Start strength", "End strength"] },
  "MULTIPLE_POSITION_FEEDBACK": { label: "Feedback by position", params: POSITIONS, group: { label: "Strength at each position", from: 0 } },
  "MULTIPLE_POSITION_VIBRATION": { label: "Vibration by position", params: ["Frequency", ...POSITIONS], group: { label: "Amplitude at each position", from: 1 } },
};

// A mode's values when a situation's default has another mode.
export const MODE_START = {
  "OFF": [],
  "FEEDBACK": [2, 3],
  "WEAPON": [2, 5, 5],
  "VIBRATION": [1, 5, 25],
  "SLOPE_FEEDBACK": [2, 7, 2, 6],
  "MULTIPLE_POSITION_FEEDBACK": [0, 0, 1, 2, 3, 4, 5, 6, 7, 8],
  "MULTIPLE_POSITION_VIBRATION": [20, 0, 0, 2, 4, 6, 8, 8, 8, 8, 8],
};

// The weapon types of a fire group's R2 and L2, in the Schema's order.
export const WEAPONS = {
  "auto": "Auto",
  "beam": "Beam laser",
  "pulse": "Pulse laser",
  "burst": "Burst laser",
  "multicannon": "Multi-cannon",
  "cannon": "Cannon",
  "fragment": "Fragment cannon",
  "railgun": "Railgun",
  "plasma": "Plasma accelerator",
  "missile": "Missiles",
  "mining": "Mining laser",
  "generic": "Generic",
};

// The panels of gyro_off_gui_focus, by Elite's GuiFocus number.
export const PANELS = [
  [1, "Right panel"],
  [2, "Left panel"],
  [3, "Comms panel"],
  [4, "Role panel"],
  [5, "Station services"],
  [6, "Galaxy map"],
  [7, "System map"],
  [8, "Orrery"],
  [9, "FSS"],
  [10, "Surface scanner"],
  [11, "Codex"],
];

// The hardpoint sizes of spin_up_ms.
export const SPIN_SIZES = { "small": "Small", "medium": "Medium", "large": "Large", "huge": "Huge" };

// The HUD colours in the order the page shows them: [label, help, Elite's
// standard colour (hud.DefaultPalette)].
export const HUD_COLORS = {
  "shield": ["Shield %", "The shield % and the ship hologram. Setting it stops the learning.", "#29c8cf"],
  "heat": ["Heat %", "The heat %. Setting it stops the learning.", "#ba6c16"],
  "hull": ["HUD main colour", "The hull %, the capacitors and the weapon lists.", "#d6870c"],
  "flame": ["Heat icon", "The heat icon, and out-of-range weapons.", "#931e1b"],
  "flash": ["Hit flashes", "Hit flashes on the hologram. Off: hits are felt from the shield % dropping.", "#3862c1"],
};

function has(obj, k) {
  return Object.prototype.hasOwnProperty.call(obj, k);
}

// TRIGGER_LABELS: each trigger key's label, with R2 or L2 when the row
// has two keys: "Hardpoints out R2", "Hit".
const TRIGGER_LABELS = {};
for (const g of TRIGGER_GROUPS) {
  for (const row of g.rows) {
    row.keys.forEach((k, i) => {
      TRIGGER_LABELS[k] = row.keys.length === 1 ? row.label : row.label + (i === 0 ? " R2" : " L2");
    });
  }
}

function colorLabel(key) {
  const g = COLOR_GROUPS.find((c) => has(c.keys, key));
  return g ? g.keys[key][0] : "";
}

function entryLabel(name, entry) {
  switch (name) {
    case "haptics_gain":
    case "rumble":
      return has(EFFECTS, entry) ? EFFECTS[entry][0] : "";
    case "colors":
      return colorLabel(entry);
    case "triggers":
      return has(TRIGGER_LABELS, entry) ? TRIGGER_LABELS[entry] : "";
    case "spin_up_ms":
      return has(SPIN_SIZES, entry) ? SPIN_SIZES[entry] : "";
    case "hud_colors":
      return has(HUD_COLORS, entry) ? HUD_COLORS[entry][0] : "";
    case "fire_groups":
      return /^[1-9][0-9]*$/.test(entry) ? "Fire group " + entry : "";
  }
  return null; // not a map
}

// labelOf is the label of a setting ("poll_ms": "Update interval"), of a
// map entry ("colors.hit": "Hit", "triggers.hit": "Hit",
// "triggers.onfoot_r": "On foot R2") or of a part of one (its entry's);
// "" when there is none.
export function labelOf(path) {
  const parts = String(path || "").split(".");
  const name = parts[0];
  if (!has(SETTINGS, name)) {
    return "";
  }
  if (parts.length === 1) {
    return SETTINGS[name].label;
  }
  const entry = entryLabel(name, parts[1]);
  return entry === null ? SETTINGS[name].label : entry;
}
