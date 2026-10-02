// Settings as the pages show them and write them back: units, rounding,
// colours, trigger values, reset patches and the texts of refused
// changes. Pure functions, with no DOM, so node can test them.
import { capital, fixed, level, ms, pct, sens } from "./format.js";
import { MODE_START, PAGE_NOTES, PANELS } from "./catalog.js";

function has(obj, k) {
  return obj !== null && typeof obj === "object" && Object.prototype.hasOwnProperty.call(obj, k);
}

// put sets obj[k] as an own property, whatever k is ("__proto__" too).
function put(obj, k, v) {
  Object.defineProperty(obj, k, { value: v, enumerable: true, writable: true, configurable: true });
}

function clamp(v, lo, hi) {
  return Math.min(Math.max(v, lo), hi);
}

// valueAt is the setting at path in config; undefined when there is none.
export function valueAt(config, path) {
  return valueIn(config, path.split("."));
}

function valueIn(v, parts) {
  for (const k of parts) {
    if (!has(v, k)) {
      return undefined;
    }
    v = v[k];
  }
  return v;
}

// keyOf is the settings schema's row for path: its own, or its map's
// ("haptics_gain.*" for "haptics_gain.hit"); null when there is none.
export function keyOf(schema, path) {
  const rows = schema || [];
  const own = rows.find((k) => k.path === path);
  if (own) {
    return own;
  }
  const dot = path.indexOf(".");
  return dot < 0 ? null : rows.find((k) => k.path === path.slice(0, dot) + ".*") || null;
}

// rowFor is the schema row that holds path, a whole map too, and the
// rest of the path under that row's value: "haptics_gain" is the whole
// "haptics_gain.*", "colors.hit" its entry "hit".
function rowFor(schema, path) {
  const rows = schema || [];
  const parts = path.split(".");
  const own = rows.find((k) => k.path === path);
  if (own) {
    return { row: own, rest: [] };
  }
  const map = rows.find((k) => k.path === parts[0] + ".*") || rows.find((k) => k.path === parts[0]);
  return map ? { row: map, rest: parts.slice(1) } : null;
}

// decimals is how many decimals step has: 2 for 0.05, 0 for 5.
function decimals(step) {
  let d = 0;
  while (d < 10 && Math.abs(Math.round(step * 10 ** d) - step * 10 ** d) > 1e-6) {
    d++;
  }
  return d;
}

function roundTo(v, digits) {
  return Number(v.toFixed(digits)) || 0; // no -0
}

// roundStep rounds v to a whole number of steps, without float noise:
// 0.85, never 0.8500000000000001.
export function roundStep(v, step) {
  if (!step) {
    return v;
  }
  return roundTo(Math.round(v / step) * step, decimals(step));
}

// shownOf is v as shown: v x key.scale, to the shown step's decimals (78
// for a brightness of 200 with 100/255; 85 for 0.85 with 100).
export function shownOf(key, v) {
  if (!key || !key.scale || typeof v !== "number") {
    return v;
  }
  return roundTo(v * key.scale, key.step ? decimals(key.step * key.scale) : 0);
}

// valueOf is the value back from shown, rounded to the key's step: 85 is
// 0.85. A whole number setting without a step takes a whole number.
export function valueOf(key, shown) {
  const k = key || {};
  const v = k.scale ? shown / k.scale : shown;
  if (k.step) {
    return roundStep(v, k.step);
  }
  return k.type === "int" ? Math.round(v) : roundTo(v, 10);
}

// logPos is the position of v on a log slider from min to max with steps
// positions; logValue is the value at pos.
export function logPos(v, min, max, steps = 1000) {
  if (!(v > 0)) {
    return 0;
  }
  return clamp(Math.round(steps * Math.log(v / min) / Math.log(max / min)), 0, steps);
}

export function logValue(pos, min, max, steps = 1000) {
  return min * (max / min) ** (clamp(pos, 0, steps) / steps);
}

// sensRound is a gyro sensitivity as it is written: within 2% of 1 it is
// 1, below 10 it has 2 decimals, from 10 one, and it stays within key's
// range (the Schema's gyro_sensitivity_x row; 0.05 to 20 without it).
// null when v is not a number.
export function sensRound(v, key) {
  if (typeof v !== "number" || !Number.isFinite(v)) {
    return null;
  }
  const r = roundTo(v, v < 10 ? 2 : 1);
  const [lo, hi] = key && key.max ? [key.min || 0, key.max] : [0.05, 20];
  return clamp(Math.abs(r - 1) <= 0.02 + 1e-9 ? 1 : r, lo, hi);
}

// parseNumber reads typed text: "0,5" is 0.5 when there is no ".", "85%"
// is 85, "25 ms" is 25. whole: only a whole number, which may have
// grouping commas ("1,500"). null when the text is not such a number.
export function parseNumber(text, whole) {
  let s = String(text === null || text === undefined ? "" : text).trim().replace(/\s*(%|ms)$/i, "").trim();
  if (whole && /^[1-9]\d{0,2}(,\d{3})+$/.test(s)) {
    s = s.replace(/,/g, "");
  } else if (!s.includes(".") && s.split(",").length === 2) {
    s = s.replace(",", ".");
  }
  if (!/^[-+]?(\d+(\.\d*)?|\.\d+)$/.test(s)) {
    return null;
  }
  const n = Number(s) || 0; // no -0
  if (!Number.isFinite(n) || (whole && !Number.isInteger(n))) {
    return null;
  }
  return n;
}

// plain is a number without grouping, with at most 2 decimals: "0.05".
function plain(v) {
  return String(roundTo(v, 2));
}

// asShown is v in the key's unit and scale: "300%", "150 ms", "20".
function asShown(key, v) {
  const s = shownOf(key, v);
  if (key.unit === "%") {
    return pct(s);
  }
  if (key.unit === "ms") {
    return ms(s);
  }
  return plain(s);
}

// valueText is a slider's value text and aria-valuetext: "100%", "250 ms",
// "1.00", "78%". kind "level": an effect level, "Off" at 0; "sens": a gyro
// sensitivity (a log key is one too).
export function valueText(key, v, kind) {
  if (typeof v !== "number" || !Number.isFinite(v)) {
    return "";
  }
  const k = key || {};
  if (kind === "level") {
    return level(v);
  }
  if (kind === "sens" || k.log) {
    return sens(v);
  }
  return asShown(k, v);
}

function hex2(n) {
  return n.toString(16).padStart(2, "0");
}

// rgbToHex is a colour [r, g, b] as "#rrggbb", each part kept to 0-255.
export function rgbToHex(rgb) {
  const parts = Array.isArray(rgb) ? rgb : [];
  return "#" + [0, 1, 2].map((i) => {
    const n = Math.round(Number(parts[i]));
    return hex2(Number.isFinite(n) ? clamp(n, 0, 255) : 0);
  }).join("");
}

// normHex reads a typed colour: #rgb or #rrggbb, with or without #, in
// any case. It is "#rrggbb" in lower case; null when it is not a colour.
export function normHex(text) {
  const s = String(text === null || text === undefined ? "" : text).trim().replace(/^#/, "");
  if (/^[0-9a-f]{3}$/i.test(s)) {
    return "#" + s.split("").map((c) => c + c).join("").toLowerCase();
  }
  if (/^[0-9a-f]{6}$/i.test(s)) {
    return "#" + s.toLowerCase();
  }
  return null;
}

// hexToRgb is a typed colour as [r, g, b]; null when it is not a colour.
export function hexToRgb(text) {
  const c = normHex(text);
  return c ? [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16)) : null;
}

// fileHex is a HUD colour from the file as the app reads it
// (vision.ParseHex): 6 hex digits, after spaces and one "#" are taken
// off. "#rrggbb" in lower case; null when the app cannot read it.
export function fileHex(text) {
  if (typeof text !== "string") {
    return null;
  }
  const s = text.trim().replace(/^#/, "");
  return /^[0-9a-f]{6}$/i.test(s) ? "#" + s.toLowerCase() : null;
}

// isOff: a HUD colour that turns the hit flashes off: "off" in any case,
// spaces around allowed.
export function isOff(text) {
  return typeof text === "string" && text.trim().toLowerCase() === "off";
}

// outside is the line that says what the file has when a control cannot
// show it, or "". key is the schema row; for a map, v is the entry.
// field: "left", "right" or "ms" of a rumble entry; the entry's name for
// a HUD colour. A word a select lacks shows in the select, so it has no
// line here.
export function outside(key, v, field) {
  if (!key) {
    return "";
  }
  switch (key.type) {
    case "int":
    case "float": {
      const lo = key.min || 0;
      if (typeof v !== "number" || key.max === undefined || (v >= lo && v <= key.max)) {
        return "";
      }
      return "edsense.json has " + asShown(key, v) + ". This takes " + asShown(key, lo) + " to " + asShown(key, key.max) + ".";
    }
    case "rumble": {
      // left and right show x100 as %; the length has no range here
      const n = has(v, field) ? v[field] : undefined;
      const lo = key.min || 0;
      if ((field !== "left" && field !== "right") || typeof n !== "number" || (n >= lo && n <= key.max)) {
        return "";
      }
      return "edsense.json has " + pct(n * 100) + ". This takes " + pct(lo * 100) + " to " + pct(key.max * 100) + ".";
    }
    case "rgb": {
      const lo = key.min || 0;
      if (!Array.isArray(v) || v.every((n) => typeof n !== "number" || (n >= lo && n <= key.max))) {
        return "";
      }
      return "edsense.json has " + v.join(", ") + ". Each part takes " + lo + " to " + key.max + ".";
    }
    case "intset": {
      if (!Array.isArray(v)) {
        return "";
      }
      const seen = new Set();
      const extra = v.filter((n) => {
        const out = !Number.isInteger(n) || n < key.min || n > key.max || seen.has(n);
        seen.add(n);
        return out;
      });
      return extra.length > 0 ? "edsense.json also has " + extra.join(", ") + " in this list. A change here drops them." : "";
    }
    case "hex": {
      if (typeof v !== "string" || v === "" || fileHex(v) || (field === "flash" && isOff(v))) {
        return "";
      }
      if ((field === "shield" || field === "heat") && v.trim() !== "") {
        // any text there stops EDSense learning the colours from the screen
        return "edsense.json has \"" + v + "\", which is not a colour. While it is set, EDSense learns no HUD colours from your screen. Clear it to make the colour automatic.";
      }
      return "edsense.json has \"" + v + "\", which is not a colour, so it is automatic.";
    }
    case "trigger": {
      const mode = modeRow([key], v && v.mode);
      if (!mode || fits(mode, v.params)) {
        return "";
      }
      const list = Array.isArray(v.params) ? v.params : [];
      const values = list.length > 0 ? " " + list.join(", ") : " with no values";
      return "edsense.json has " + mode.name + values + ", which this mode does not take. A change here fixes it.";
    }
  }
  return "";
}

// refusal says why EDSense did not take a change, from a settings.patch
// problem.
export function refusal(p) {
  if (p && p.code === "broken") {
    return "edsense.json has an error, so nothing was changed. Fix the file and save it, then try again.";
  }
  const msg = p && p.msg ? String(p.msg).replace(/\.$/, "") : "";
  return msg ? "EDSense did not take it: " + msg + "." : "EDSense did not take it.";
}

// sentence is msg with a capital and a full stop.
function sentence(msg) {
  return capital(String(msg).replace(/\.$/, "")) + ".";
}

// problemText is the line under a control for a settings.patch problem p;
// key is the control's schema row.
export function problemText(p, key) {
  const msg = p && typeof p.msg === "string" ? p.msg : "";
  const k = key || {};
  switch (p && p.code) {
    case "missing":
      return "There is no such folder.";
    case "type":
      if (!msg) {
        return refusal(p);
      }
      // "a full path, such as D:\Elite\Journal", "a colour as #rrggbb"
      return /^a (full path|colour as)/.test(msg) ? "Type " + msg.replace(/\.$/, "") + "." : sentence(msg);
    case "range": {
      const field = String(p.path || "").split(".").pop();
      if (k.type === "rumble" && (field === "left" || field === "right")) {
        return "Use a value from " + pct((k.min || 0) * 100) + " to " + pct(k.max * 100) + ".";
      }
      if ((k.scale || k.unit) && k.max !== undefined) {
        return "Use a value from " + asShown(k, k.min || 0) + " to " + asShown(k, k.max) + ".";
      }
      if (msg.startsWith("from ")) {
        return "Use a value " + msg.replace(/\.$/, "") + ".";
      }
      if (msg.startsWith("at most ")) {
        return "Use " + msg.replace(/\.$/, "") + ".";
      }
      return msg ? sentence(msg) : refusal(p);
    }
  }
  return refusal(p);
}

// resetPatch is the merge patch that puts paths back to their defaults:
// ["haptics_gain.boost", "turn_feel"] is
// {"haptics_gain": {"boost": null}, "turn_feel": null}.
export function resetPatch(paths) {
  const patch = {};
  for (const path of paths || []) {
    const parts = path.split(".");
    let at = patch;
    for (const k of parts.slice(0, -1)) {
      if (!has(at, k)) {
        put(at, k, {});
      }
      at = at[k];
      if (at === null) {
        break; // a whole setting above it is reset already
      }
    }
    if (at !== null) {
      put(at, parts[parts.length - 1], null);
    }
  }
  return patch;
}

function empty(v) {
  return v === null || v === undefined || (Array.isArray(v) && v.length === 0);
}

function same(a, b, name) {
  if (name === "params" && empty(a) && empty(b)) {
    return true; // a trigger's null params are []
  }
  if (a === b) {
    return true;
  }
  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((x, i) => same(x, b[i], ""));
  }
  if (a === null || b === null || typeof a !== "object" || typeof b !== "object") {
    return false;
  }
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  for (const k of keys) {
    if (!same(a[k], b[k], k)) {
      return false;
    }
  }
  return true;
}

// sameValue: a and b are the same setting value, a trigger's null
// "params" the same as [].
export function sameValue(a, b) {
  return same(a, b, "");
}

// defaultAt is the Schema default at path: a setting's, a whole map's, or
// a map entry's from its map's default (undefined for an entry with none,
// as fire group 3).
export function defaultAt(schema, path) {
  const at = rowFor(schema, path);
  return at ? valueIn(at.row.default, at.rest) : undefined;
}

// atDefault: every path in config equals its Schema default.
export function atDefault(schema, config, paths) {
  return (paths || []).every((p) => sameValue(valueAt(config, p), defaultAt(schema, p)));
}

// gyroNote is the Gyro aim page's note while the connection passes no
// gyro, or "". Before the full status, has means nothing.
export function gyroNote(status) {
  return status && status.full && !(status.gyro && status.gyro.has) ? PAGE_NOTES.gyro : "";
}

// hudNote is the HUD reader page's note while the screen cannot be read
// here, or "".
export function hudNote(status) {
  return status && status.full && !(status.hud && status.hud.can) ? PAGE_NOTES.hud : "";
}

// modeRow is the trigger mode name from the Schema's triggers.* row:
// {name, params: [[min, max], ...], above}; null when there is none.
// schema: the settings schema, or the row itself in a list.
export function modeRow(schema, name) {
  const row = (schema || []).find((k) => k.path === "triggers.*");
  const modes = row && Array.isArray(row.modes) ? row.modes : [];
  return modes.find((m) => m.name === name) || null;
}

// startParams are the values trigger key takes when it changes to mode:
// its default's when its default has that mode, else MODE_START's.
export function startParams(schema, key, mode) {
  const def = defaultAt(schema, "triggers." + key);
  if (def && def.mode === mode) {
    return Array.isArray(def.params) ? def.params.slice() : [];
  }
  return has(MODE_START, mode) ? MODE_START[mode].slice() : [];
}

// keepAbove keeps the value at mode.above over the one before it.
function keepAbove(mode, out) {
  const a = mode.above || 0;
  if (a > 0 && a < out.length) {
    const [lo, hi] = mode.params[a];
    out[a] = clamp(out[a], Math.min(Math.max(lo, out[a - 1] + 1), hi), hi);
  }
  return out;
}

// fitParams is params as mode's selects show them: missing values from
// MODE_START, extra ones dropped, each within its range.
export function fitParams(mode, params) {
  if (!mode) {
    return [];
  }
  const list = Array.isArray(params) ? params : [];
  const start = has(MODE_START, mode.name) ? MODE_START[mode.name] : [];
  return keepAbove(mode, mode.params.map(([lo, hi], i) => {
    const v = typeof list[i] === "number" && Number.isFinite(list[i]) ? Math.round(list[i]) : Number.isInteger(start[i]) ? start[i] : lo;
    return clamp(v, lo, hi);
  }));
}

// fits: params are what mode takes, as Go checks them: the count, each
// value's range, and the value at above over the one before it.
export function fits(mode, params) {
  if (!mode) {
    return false;
  }
  const list = params === null || params === undefined ? [] : params;
  if (!Array.isArray(list) || list.length !== mode.params.length) {
    return false;
  }
  return list.every((v, i) => {
    let [lo, hi] = mode.params[i];
    if (mode.above > 0 && i === mode.above) {
      const [plo, phi] = mode.params[i - 1];
      const prev = list[i - 1];
      if (prev >= plo && prev <= phi) {
        lo = Math.max(lo, prev + 1);
      }
    }
    return Number.isInteger(v) && v >= lo && v <= hi;
  });
}

// setParam is params (fitted) with value i set to v. A value that reaches
// the one at above moves that one up.
export function setParam(mode, params, i, v) {
  const out = fitParams(mode, params);
  if (!mode || i < 0 || i >= out.length) {
    return out;
  }
  const [lo, hi] = mode.params[i];
  out[i] = clamp(Math.round(v), lo, hi);
  const a = mode.above || 0;
  if (a > 0 && i === a - 1 && out[a] <= out[i]) {
    out[a] = out[i] + 1;
  }
  return keepAbove(mode, out);
}

// triggerSummary says what trigger t does: "A click between 2 and 5,
// strength 6". A mode Go does not know is used as Off.
export function triggerSummary(t) {
  const mode = t && t.mode;
  if (!has(MODE_START, mode) || mode === "OFF") {
    return "No resistance";
  }
  const list = Array.isArray(t.params) ? t.params : [];
  const p = MODE_START[mode].map((s, i) => (typeof list[i] === "number" ? list[i] : s));
  switch (mode) {
    case "FEEDBACK":
      return "Resistance from " + p[0] + ", strength " + p[1];
    case "WEAPON":
      return "A click between " + p[0] + " and " + p[1] + ", strength " + p[2];
    case "VIBRATION":
      return "Vibrates from " + p[0] + ", amplitude " + p[1] + ", frequency " + p[2];
    case "SLOPE_FEEDBACK":
      return "Resistance from " + p[0] + " to " + p[1] + ", strength " + p[2] + " to " + p[3];
    case "MULTIPLE_POSITION_FEEDBACK":
      return "Strength by position: " + p.join(" ");
    default:
      return "Frequency " + p[0] + ", amplitude by position: " + p.slice(1).join(" ");
  }
}

// groupRange is the fire group numbers from key, the Schema's
// fire_groups.* row; 1 to 99 without it.
function groupRange(key) {
  return key && key.max ? [key.min || 1, key.max] : [1, 99];
}

// groupOrder lists the fire groups in fireGroups by number: ["1", "2",
// "10"]. A key that is not a group number is left out.
export function groupOrder(fireGroups, key) {
  const [lo, hi] = groupRange(key);
  return Object.keys(fireGroups || {})
    .filter((k) => /^[1-9][0-9]*$/.test(k) && Number(k) >= lo && Number(k) <= hi)
    .sort((a, b) => Number(a) - Number(b));
}

// nextGroup is the lowest fire group number not in fireGroups, as text;
// "" when every one is.
export function nextGroup(fireGroups, key) {
  const [lo, hi] = groupRange(key);
  for (let n = lo; n <= hi; n++) {
    if (!has(fireGroups, String(n))) {
      return String(n);
    }
  }
  return "";
}

// focusList is the gyro_off_gui_focus list with panel n added (on) or
// taken out: sorted, each panel once, and only panels.
export function focusList(list, n, on) {
  const panels = PANELS.map(([p]) => p);
  const set = new Set((Array.isArray(list) ? list : []).filter((p) => panels.includes(p)));
  if (on) {
    set.add(n);
  } else {
    set.delete(n);
  }
  return [...set].filter((p) => panels.includes(p)).sort((a, b) => a - b);
}
