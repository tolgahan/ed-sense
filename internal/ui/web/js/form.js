// The settings controls of the pages: rows, cards, Reset, and the one way
// a page writes a setting (F.write). A control keeps what the player set
// until the settings show it (edits.js), and takes no value from the
// settings while it has the focus.
import { h, icon, setText } from "./dom.js";
import { brokenCard, locked } from "./cards.js";
import { edits } from "./edits.js";
import { COLOR_GROUPS, EFFECTS, HUD_COLORS, SETTINGS, TRIGGER_GROUPS, labelOf } from "./catalog.js";
import {
  atDefault, defaultAt, fileHex, hexToRgb, isOff, keyOf, logPos, logValue, normHex, outside, parseNumber,
  problemText, refusal, rgbToHex, sameValue, sensRound, shownOf, valueAt, valueOf, valueText,
} from "./values.js";
import { sens } from "./format.js";

// How long a slider waits after its last change before it writes.
export const SLIDER_WAIT = 250;

// How many items a Reset's question lists; more show as "and N more".
const RESET_ITEMS = 6;

// How long a filter waits after the last key before it says how many
// effects match.
export const FILTER_SAY = 500;

// The line of a filter that matches nothing.
const NO_MATCH = "No effect matches.";

// The flush of each form whose page shows, for flushAll.
const showing = new Set();

// flushAll sends the waiting writes of the page shown now, as the window
// closes or hides. It resolves once EDSense answered them.
export function flushAll() {
  return Promise.all(Array.from(showing, (f) => f()));
}

function has(obj, k) {
  return obj !== null && typeof obj === "object" && Object.prototype.hasOwnProperty.call(obj, k);
}

// overlaps: one path is the other, or under it.
function overlaps(a, b) {
  return a === b || a.startsWith(b + ".") || b.startsWith(a + ".");
}

// under: path is one of roots, or under one.
function under(path, roots) {
  return roots.some((r) => path === r || path.startsWith(r + "."));
}

// same: a and b are one setting value; a missing value is null.
function same(a, b) {
  if ((a === undefined || a === null) && (b === undefined || b === null)) {
    return true;
  }
  return sameValue(a, b);
}

// attr sets or removes (null) an attribute, only on a change.
function attr(el, name, v) {
  if (v === null) {
    if (el.hasAttribute(name)) {
      el.removeAttribute(name);
    }
  } else if (el.getAttribute(name) !== v) {
    el.setAttribute(name, v);
  }
}

// setting is the catalog entry of the setting path is in; null for none.
function setting(path) {
  const name = String(path).split(".")[0];
  return has(SETTINGS, name) ? SETTINGS[name] : null;
}

// idOf is the id of a setting's row: "set-colors-hit" for "colors.hit".
export function idOf(path) {
  return "set-" + String(path).replace(/\./g, "-");
}

// helpOf is the catalog's help for a setting or a map entry; "" when
// there is none (a spin-up size: its card says it).
export function helpOf(path) {
  const parts = String(path).split(".");
  const s = setting(path);
  if (!s) {
    return "";
  }
  if (parts.length === 1) {
    return s.help || "";
  }
  const entry = parts[1];
  switch (parts[0]) {
    case "haptics_gain":
    case "rumble":
      return has(EFFECTS, entry) ? EFFECTS[entry][1] : "";
    case "colors": {
      const g = COLOR_GROUPS.find((c) => has(c.keys, entry));
      return g ? g.keys[entry][1] : "";
    }
    case "hud_colors":
      return has(HUD_COLORS, entry) ? HUD_COLORS[entry][1] : "";
    case "triggers": {
      for (const g of TRIGGER_GROUPS) {
        const row = g.rows.find((r) => r.keys.includes(entry));
        if (row) {
          return row.help;
        }
      }
      return "";
    }
    case "fire_groups":
      return s.entryHelp || "";
    case "spin_up_ms":
      return "";
  }
  return s.help || "";
}

// helpNow is a setting's help as things are: by the controller app, by
// the value shown (with DS4Windows, its own text where it has one), and
// with the lines the app or its UDP server add.
export function helpNow(path, value, status) {
  const s = String(path).includes(".") ? null : setting(path);
  if (!s) {
    return helpOf(path);
  }
  const st = status || {};
  let text = s.help || "";
  if (s.byKind) {
    text = s.byKind[st.kind === "ds4windows" ? "ds4windows" : "dsx"];
  } else if (st.kind === "ds4windows" && s.ds4windowsByValue && has(s.ds4windowsByValue, value)) {
    text = s.ds4windowsByValue[value];
  } else if (s.byValue && has(s.byValue, value)) {
    text = s.byValue[value];
  }
  if (s.ds4windows && st.kind === "ds4windows") {
    text += " " + s.ds4windows;
  }
  if (s.udpSilent && st.gyro && st.gyro.udp === "silent") {
    text += " " + s.udpSilent;
  }
  return text;
}

// resetItems lists what a Reset puts back, by the catalog's labels: a
// whole map with its count ("Effect levels (88)"), and past 6 items the
// first 5 and "and N more".
export function resetItems(schema, config, paths) {
  const items = (paths || []).map((p) => {
    const label = labelOf(p) || p;
    if (p.includes(".")) {
      return label;
    }
    const row = (schema || []).find((k) => k.path === p + ".*");
    if (!row) {
      return label;
    }
    const v = valueAt(config, p);
    const n = row.keys && row.keys.length > 0 ? row.keys.length : Object.keys(v && typeof v === "object" ? v : {}).length;
    return label + " (" + n + ")";
  });
  if (items.length > RESET_ITEMS) {
    return items.slice(0, RESET_ITEMS - 1).concat("and " + (items.length - RESET_ITEMS + 1) + " more");
  }
  return items;
}

// sliderScale maps a setting to its slider: the slider's range and step,
// pos(value) and value(pos). log: positions 0-1000 on a log scale; raw:
// the slider runs on the stored value (the lightbar's 0-255); else on the
// value as shown (85 for 0.85 with scale 100).
export function sliderScale(key, opts) {
  const k = key || {};
  const lo = k.min || 0;
  const hi = typeof k.max === "number" ? k.max : 100;
  if (opts.log || k.log) {
    return {
      min: 0, max: 1000, step: 1,
      pos: (v) => logPos(v, lo, hi),
      value: (p) => sensRound(logValue(p, lo, hi), k),
    };
  }
  if (opts.raw || !k.scale) {
    const plain = { ...k, scale: undefined };
    return {
      min: lo, max: hi, step: k.step || 1,
      pos: (v) => v,
      value: (p) => valueOf(plain, p),
    };
  }
  return {
    min: shownOf(k, lo), max: shownOf(k, hi), step: k.step ? Number((k.step * k.scale).toFixed(6)) : 1,
    pos: (v) => shownOf(k, v),
    value: (p) => valueOf(k, p),
  };
}

// plain is a bound as typed: "0.05", "20".
function plain(v) {
  return String(Number(Number(v).toFixed(6)));
}

// defaultParse reads a text field by its Schema type: {value} or {error}.
function defaultParse(key, text) {
  const t = String(text).trim();
  switch (key && key.type) {
    case "int": {
      const n = parseNumber(t, true);
      return n === null ? { error: "Type a whole number." } : { value: n };
    }
    case "float": {
      const n = parseNumber(t);
      return n === null ? { error: "Type a number." } : { value: n };
    }
  }
  return { value: t };
}

// filterField is the Filter box over folded groups of effects, with the
// line that shows when nothing matches.
export function filterField(id, label = "Filter effects") {
  const input = h("input", { class: "text wide", type: "search", id, placeholder: "Filter", autocomplete: "off", spellcheck: "false" });
  const el = h("div", { class: "filter" }, h("label", { class: "sr-only", for: id, text: label }), input);
  const none = h("p", { class: "note", hidden: true, text: NO_MATCH });
  return { el, input, none };
}

// filterGroups filters folded groups of rows as the player types in
// input, with no wait and in any case: a row shows when its text has what
// is typed, a group with a match opens, a group without one hides, and
// none shows when nothing matches. Clearing the filter gives each group
// the open state it had. The row or group with the focus never hides.
// groups: [{el: <details>, rows: [{el, text}]}]; text: the label, help
// and key. announce(text) is told how many effects match once typing
// pauses (FILTER_SAY), for a screen reader.
export function filterGroups(input, groups, none, announce) {
  const words = groups.map((g) => g.rows.map((r) => String(r.text || "").toLowerCase()));
  let saved = null; // each group's own open state while a filter is on
  let timer = 0;
  function run() {
    const q = input.value.trim().toLowerCase();
    const focused = typeof document !== "undefined" ? document.activeElement : null;
    const holds = (el) => Boolean(focused) && el.contains(focused);
    clearTimeout(timer);
    if (!q) {
      groups.forEach((g, i) => {
        g.el.hidden = false;
        for (const r of g.rows) {
          r.el.hidden = false;
        }
        if (saved) {
          g.el.open = saved[i];
        }
      });
      saved = null;
      if (none) {
        none.hidden = true;
      }
      return;
    }
    if (!saved) {
      saved = groups.map((g) => g.el.open);
    }
    let any = false;
    let count = 0; // the rows whose text has what is typed
    groups.forEach((g, i) => {
      let match = false;
      g.rows.forEach((r, j) => {
        const found = words[i][j].includes(q);
        const show = found || holds(r.el);
        r.el.hidden = !show;
        match = match || show;
        count += found ? 1 : 0;
      });
      g.el.hidden = !match && !holds(g.el);
      if (match) {
        g.el.open = true;
      }
      any = any || match;
    });
    if (none) {
      none.hidden = any;
    }
    if (announce) {
      const text = count === 0 ? NO_MATCH : count === 1 ? "1 effect matches." : count + " effects match.";
      timer = setTimeout(() => announce(text), FILTER_SAY);
    }
  }
  input.addEventListener("input", run);
  input.addEventListener("search", run);
  return { run };
}

let made = 0; // for the ids of cards and groups without one

// form makes a page's controls. Every control registers itself; F.update
// shows the settings in all of them, F.leave sends what waits.
export function form(app) {
  const ed = edits(app);
  const broken = brokenCard(app);
  const controls = [];
  const resets = []; // {update(ctx)} of the Reset buttons
  const live = []; // f(ctx), run on every update: help by status, notes
  const waiting = new Map(); // control -> {pairs, timer}: a slider's write
  let pageEl = null;
  let titleEl = null;
  let bar = null;
  let before = null; // the control that had the focus when the file broke
  let changes = 0; // focus loss, writes and errors
  let shownKey = null;
  let left = false;

  function context() {
    return {
      config: (app.settings && app.settings.config) || {},
      lock: locked(app),
      status: app.status,
      schema: app.schema || [],
      ed,
    };
  }

  // current is the setting at path, or its default when the file has none.
  function current(ctx, path) {
    const v = valueAt(ctx.config, path);
    return v === undefined ? defaultAt(ctx.schema, path) : v;
  }

  function settingsRev() {
    return app.settings ? app.settings.rev : 0;
  }

  function refresh() {
    changes++;
    app.refresh();
  }

  // onTheWay: a write that touches path is on its way, or written and not
  // in the settings yet; also one a slider's hold took the place of.
  function onTheWay(path) {
    for (const [at, mark] of ed.pending) {
      const m = mark.held ? mark.after : mark;
      if (m && overlaps(at, path) && !(m.rev > 0 && settingsRev() >= m.rev)) {
        return true;
      }
    }
    return false;
  }

  // queued: a slider's write that touches path waits.
  function queued(path) {
    for (const w of waiting.values()) {
      if (w.pairs.some(([p]) => overlaps(p, path))) {
        return true;
      }
    }
    return false;
  }

  // owner is the control that shows path (its own or above it), first
  // the one that wrote.
  function owner(path, first) {
    const mine = (c) => c && Array.isArray(c.paths) && c.paths.some((p) => path === p || path.startsWith(p + "."));
    if (mine(first)) {
      return first;
    }
    return controls.find(mine) || null;
  }

  function add(c) {
    controls.push(c);
    if (c.el) {
      // an edit that waited for the focus to go lands then
      c.el.addEventListener("focusout", () => {
        changes++;
        setTimeout(() => app.refresh(), 0);
      });
    }
    changes++;
    return c;
  }

  // refused shows why a write was not taken: under the control of each
  // problem's path, else as a toast; toast: every one as a toast (the
  // page is going).
  function refused(control, problems, toast) {
    const list = problems && problems.length > 0 ? problems : [null];
    const failed = new Set();
    for (const p of list) {
      const path = p && typeof p.path === "string" ? p.path : "";
      const c = toast || !path ? null : owner(path, control);
      const key = keyOf(app.schema, path || (c && c.paths[0]) || "");
      if (c && typeof c.fail === "function") {
        if (!failed.has(c)) {
          failed.add(c);
          c.fail(problemText(p, key));
        }
      } else if (toast && p && p.code !== "broken" && labelOf(path)) {
        app.toast(labelOf(path) + ": " + problemText(p, key), true);
      } else {
        app.toast(refusal(p), true);
      }
    }
  }

  // write sends pairs ([path, value], ...) for control in one patch: the
  // one way a page changes settings. Nothing goes while edsense.json is
  // broken. A pair equal to the settings is left out, unless a write of
  // its path is still on its way. A refusal shows under the control of its
  // path, else as a toast; toast: as a toast. It resolves to true when the
  // settings take the values.
  async function write(control, pairs, toast) {
    const ctx = context();
    if (ctx.lock) {
      for (const [path] of pairs) {
        ed.drop(path);
      }
      refresh();
      return false;
    }
    const send = [];
    for (const [path, value] of pairs) {
      if (same(current(ctx, path), value) && !onTheWay(path)) {
        ed.drop(path);
      } else {
        send.push([path, value]);
      }
    }
    if (send.length === 0) {
      for (const [path] of pairs) {
        const c = owner(path, control);
        if (c && c.clear) {
          c.clear();
        }
      }
      refresh();
      return true;
    }
    let ok = false;
    try {
      const reply = await ed.setMany(send);
      ok = Boolean(reply && reply.applied);
      if (ok) {
        for (const [path] of pairs) {
          const c = owner(path, control);
          if (c && c.clear) {
            c.clear();
          }
        }
      } else {
        // the page may have gone while the patch was on its way
        refused(control, reply && reply.problems, toast || left);
      }
    } catch (err) {
      const text = app.errorText(err);
      if (!(toast || left) && control && typeof control.fail === "function") {
        control.fail(text);
      } else {
        app.toast(text, true);
      }
    }
    refresh();
    return ok;
  }

  // giveWay takes the queued writes that touch pairs' paths out of the
  // queue: the new write replaces them. Their other paths are let go.
  function giveWay(pairs) {
    for (const [c, w] of Array.from(waiting)) {
      if (w.pairs.some(([p]) => pairs.some(([q]) => overlaps(p, q)))) {
        clearTimeout(w.timer);
        waiting.delete(c);
        for (const [p] of w.pairs) {
          if (!pairs.some(([q]) => q === p)) {
            ed.drop(p);
          }
        }
      }
    }
  }

  // later queues a slider's write, SLIDER_WAIT after its last change. The
  // paths keep their values meanwhile (ed.hold).
  function later(control, pairs) {
    giveWay(pairs);
    for (const [path, value] of pairs) {
      ed.hold(path, value);
    }
    const w = { pairs, timer: 0 };
    w.timer = setTimeout(() => {
      if (waiting.get(control) === w) {
        waiting.delete(control);
        write(control, pairs);
      }
    }, SLIDER_WAIT);
    waiting.set(control, w);
    changes++;
  }

  // park takes the queued writes at or under paths out of the queue, to
  // be dropped or queued again (unpark).
  function park(paths) {
    const out = [];
    for (const [c, w] of Array.from(waiting)) {
      if (w.pairs.some(([p]) => under(p, paths))) {
        clearTimeout(w.timer);
        waiting.delete(c);
        out.push([c, w]);
      }
    }
    return out;
  }

  function unpark(list) {
    for (const [c, w] of list) {
      if (left) {
        write(c, w.pairs, true);
      } else {
        later(c, w.pairs);
      }
    }
  }

  // flush sends every queued write now. It resolves once EDSense
  // answered them.
  function flush(toast) {
    const sent = [];
    for (const [c, w] of Array.from(waiting)) {
      clearTimeout(w.timer);
      waiting.delete(c);
      sent.push(write(c, w.pairs, toast));
    }
    return Promise.all(sent);
  }

  // cancel stops the queued writes at or under paths and lets their
  // values go.
  function cancel(paths) {
    for (const [, w] of park(paths)) {
      for (const [p] of w.pairs) {
        ed.drop(p);
      }
    }
    for (const p of paths) {
      ed.drop(p);
    }
    changes++;
  }

  // atDefaults: paths are at their defaults, with no write of them queued
  // or on its way.
  function atDefaults(ctx, paths) {
    return atDefault(ctx.schema, ctx.config, paths) && !paths.some((p) => queued(p) || onTheWay(p));
  }

  // reset asks, then puts paths back to their defaults in one patch of
  // nulls. Writes of them that wait are held while it asks: Reset drops
  // them, Cancel lets them go.
  async function reset(title, paths) {
    const ctx = context();
    if (ctx.lock) {
      return false;
    }
    if (atDefaults(ctx, paths)) {
      app.announce("Already at the defaults.", false);
      return false;
    }
    const parked = park(paths);
    const yes = await app.confirm({
      title: "Reset " + title + "?",
      text: "These settings go back to their defaults:",
      items: resetItems(ctx.schema, ctx.config, paths),
      ok: "Reset",
    });
    if (!yes || left || locked(app)) {
      unpark(parked);
      refresh();
      return false;
    }
    for (const [, w] of parked) {
      for (const [p] of w.pairs) {
        ed.drop(p);
      }
    }
    cancel(paths);
    for (const c of controls) {
      if (c.clear && Array.isArray(c.paths) && c.paths.some((p) => under(p, paths))) {
        c.clear();
      }
    }
    let ok = false;
    try {
      const reply = await ed.setMany(paths.map((p) => [p, null]));
      ok = Boolean(reply && reply.applied);
      if (ok) {
        app.announce("Reset to defaults.", false);
      } else {
        refused(null, reply && reply.problems, true);
      }
    } catch (err) {
      app.toast(app.errorText(err), true);
    }
    refresh();
    return ok;
  }

  // resetButton is a quiet small button that resets paths, named by its
  // text and title: "Reset Haptics", "Reset group Ship systems". It keeps
  // the focus while there is nothing to reset (aria-disabled), and is
  // disabled while edsense.json is broken.
  function resetButton(title, paths, text = "Reset") {
    const btn = h("button", { class: "btn quiet small", type: "button", "aria-label": text + " " + title, text, onclick: () => reset(title, paths) });
    resets.push({
      update(ctx) {
        btn.disabled = ctx.lock;
        attr(btn, "aria-disabled", atDefaults(ctx, paths) ? "true" : null);
      },
    });
    return btn;
  }

  // base builds a setting's row: its label, help, the line about the
  // file's value and the error, with the control's parts in .control.
  // single: one control, labelled by a <label for>; else a group.
  function base(path, opts, single) {
    const id = opts.id || idOf(path);
    const ids = { row: id, label: id + "-label", help: id + "-help", why: id + "-why", err: id + "-err", ctl: id + "-ctl" };
    const helpText = opts.help !== undefined ? opts.help : helpOf(path);
    const p = {
      ids,
      name: h("span", { id: ids.label, text: opts.label !== undefined ? opts.label : labelOf(path) }),
      help: h("span", { class: "row-sub", id: ids.help, text: helpText, hidden: !helpText }),
      why: h("span", { class: "row-why", id: ids.why, hidden: true }),
      err: h("p", { class: "field-error", id: ids.err }),
      control: h("div", { class: "control", role: single ? null : "group", "aria-labelledby": single ? null : ids.label }),
    };
    p.head = h(single ? "label" : "span", { class: "row-label", for: single ? ids.ctl : null }, p.name, p.help, p.why);
    p.field = h("div", { class: "field" }, p.control, p.err);
    p.el = h("div", { class: opts.tight ? "row wrap tight" : "row wrap", "data-setting": path, id: ids.row }, p.head, p.field);
    return p;
  }

  // make is a control's common part: its error, its lines, and when it
  // takes the settings' value. targets: the inputs the help and error
  // describe; watch: the inputs whose focus keeps the value as it is.
  function make(path, opts, p, targets, watch) {
    let err = null; // {text, raw, was}
    const c = {
      el: p.el,
      path,
      paths: [path],
      parts: p,
      targets,
      watch: watch || targets,
      force: false, // take the settings' value once, even with the focus
      busy: false, // asking first: keep the value
      update() {},
      focus() {
        const t = targets.find((x) => !x.disabled && !x.hidden) || targets[0];
        if (t) {
          t.focus();
        }
      },
      error() {
        return err;
      },
      // fail shows text under the control and reads it out once, after
      // opts.prefix (a table cell's column). raw: the typed text, which a
      // text field keeps; other controls go back to the settings' value.
      fail(text, raw) {
        const said = (opts.prefix || "") + text;
        err = { text: said, raw, was: c.paths.map((x) => current(context(), x)) };
        if (raw === undefined) {
          c.force = true;
        }
        setText(p.err, said);
        c.describe();
        app.announce(said, true);
        changes++;
      },
      clear() {
        if (!err) {
          return;
        }
        err = null;
        setText(p.err, "");
        c.describe();
        changes++;
      },
      // check clears the error once the setting changed in the file
      check(ctx) {
        if (err && !c.paths.every((x, i) => same(current(ctx, x), err.was[i]))) {
          c.clear();
        }
      },
      why(text) {
        setText(p.why, text || "");
        if (p.why.hidden !== !text) {
          p.why.hidden = !text;
          c.describe();
        }
      },
      help(text) {
        setText(p.help, text || "");
        if (p.help.hidden !== !text) {
          p.help.hidden = !text;
          c.describe();
        }
      },
      describe() {
        const by = [p.help.hidden ? "" : p.ids.help, p.why.hidden ? "" : p.ids.why, err ? p.ids.err : ""].filter(Boolean).join(" ");
        for (const t of c.targets) {
          attr(t, "aria-describedby", by || null);
          attr(t, "aria-invalid", err ? "true" : null);
        }
      },
      // takes: the control may show the settings' value now
      takes() {
        if (c.busy) {
          return false;
        }
        if (c.force) {
          c.force = false;
          return true;
        }
        const focused = typeof document !== "undefined" ? document.activeElement : null;
        return !waiting.has(c) && c.paths.every((x) => ed.shows(null, x)) &&
          !c.watch.some((w) => focused && w.contains(focused));
      },
    };
    return c;
  }

  // liveHelp has the help follow the status and the value shown, for a
  // setting whose help does (or own(ctx), the page's).
  function liveHelp(c, path, own, shownValue) {
    const s = path.includes(".") ? null : setting(path);
    if (!own && !(s && (s.byKind || s.byValue || s.ds4windowsByValue || s.ds4windows || s.udpSilent))) {
      return;
    }
    live.push((ctx) => c.help(own ? own(ctx) : helpNow(path, shownValue(), ctx.status)));
  }

  // toggle is a switch row. opts.on and opts.off: the values it writes
  // (true and false); opts.confirm(on) -> Promise<bool>: asked first;
  // opts.helpFor(ctx): its help as things are.
  function toggle(path, opts = {}) {
    const on = opts.on !== undefined ? opts.on : true;
    const off = opts.off !== undefined ? opts.off : false;
    const p = base(path, opts, true);
    const input = h("input", { type: "checkbox", class: "switch", role: "switch", id: p.ids.ctl, "aria-labelledby": p.ids.label });
    p.control.append(input);
    const c = make(path, opts, p, [input]);
    input.addEventListener("change", async () => {
      const want = input.checked;
      if (opts.confirm) {
        c.busy = true;
        let yes = false;
        try {
          yes = await opts.confirm(want);
        } finally {
          c.busy = false;
        }
        if (!yes) {
          c.force = true; // No puts it back
          refresh();
          return;
        }
      }
      const value = want ? on : off;
      if (await write(c, [[path, value]]) && opts.after) {
        opts.after(value);
      }
    });
    c.update = (ctx) => {
      c.check(ctx);
      input.disabled = ctx.lock;
      if (c.takes()) {
        input.checked = same(current(ctx, path), on);
      }
    };
    liveHelp(c, path, opts.helpFor ? (ctx) => opts.helpFor(ctx) : null, () => (input.checked ? on : off));
    c.describe();
    return add(c);
  }

  // options are a choice's [value, label] pairs: opts', the catalog's, or
  // the Schema's enum.
  function optionsOf(path, opts) {
    const s = setting(path) || {};
    const key = keyOf(app.schema, path) || {};
    return opts.options || s.options || (key.enum || []).map((v) => [v, v]);
  }

  function aliasOf(path, opts) {
    const s = setting(path) || {};
    return opts.alias || s.alias || {};
  }

  // choice is a segmented row: native radios in a radiogroup. opts.options
  // [[value, label]], opts.alias {file value: value shown},
  // opts.helpFor(value, ctx).
  function choice(path, opts = {}) {
    const options = optionsOf(path, opts);
    const alias = aliasOf(path, opts);
    const shown = (v) => (typeof v === "string" && has(alias, v) ? alias[v] : v);
    const p = base(path, opts, false);
    p.control.removeAttribute("role"); // the radiogroup is the group
    p.control.removeAttribute("aria-labelledby");
    const seg = h("div", { class: "seg", role: "radiogroup", "aria-labelledby": p.ids.label });
    const c = make(path, opts, p, [seg]);
    const radios = options.map(([value, label]) => {
      const r = h("input", { type: "radio", name: p.ids.ctl, value });
      r.addEventListener("change", async () => {
        if (r.checked && await write(c, [[path, value]]) && opts.after) {
          opts.after(value);
        }
      });
      seg.append(h("label", {}, r, h("span", { text: label })));
      return r;
    });
    p.control.append(seg);
    c.focus = () => {
      const r = radios.find((x) => x.checked) || radios[0];
      if (r) {
        r.focus();
      }
    };
    c.update = (ctx) => {
      c.check(ctx);
      for (const r of radios) {
        r.disabled = ctx.lock;
      }
      if (c.takes()) {
        const v = shown(current(ctx, path));
        for (const r of radios) {
          r.checked = r.value === v;
        }
      }
    };
    const picked = () => {
      const r = radios.find((x) => x.checked);
      return r ? r.value : shown(current(context(), path));
    };
    liveHelp(c, path, opts.helpFor ? (ctx) => opts.helpFor(picked(), ctx) : null, picked);
    c.describe();
    return add(c);
  }

  // select is a row with a native select. A file value it lacks shows as
  // an extra selected option, opts.unknown(v) ("<v> (unknown)").
  function select(path, opts = {}) {
    const options = optionsOf(path, opts);
    const alias = aliasOf(path, opts);
    const unknown = opts.unknown || ((v) => String(v) + " (unknown)");
    const p = base(path, opts, true);
    const sel = h("select", { class: "select", id: p.ids.ctl, "aria-labelledby": p.ids.label },
      options.map(([value, label]) => h("option", { value, text: label })));
    p.control.append(sel);
    const c = make(path, opts, p, [sel]);
    let extra = null;
    sel.addEventListener("change", async () => {
      if (extra && sel.selectedOptions[0] === extra) {
        return; // the file's own word again
      }
      const value = sel.value;
      if (await write(c, [[path, value]]) && opts.after) {
        opts.after(value);
      }
    });
    c.update = (ctx) => {
      c.check(ctx);
      sel.disabled = ctx.lock;
      if (!c.takes()) {
        return;
      }
      const v = current(ctx, path);
      const w = typeof v === "string" && has(alias, v) ? alias[v] : v;
      if (options.some(([o]) => o === w)) {
        if (extra) {
          extra.remove();
          extra = null;
        }
        sel.value = w;
      } else {
        if (!extra) {
          extra = h("option", { value: "" });
          sel.append(extra);
        }
        setText(extra, unknown(v));
        extra.selected = true;
      }
    };
    c.describe();
    return add(c);
  }

  // slider is a range row. It writes SLIDER_WAIT after its last change.
  // opts.log: a log slider (the Schema's log); opts.raw: on the stored
  // value; opts.field: a number field beside it, which writes on change;
  // opts.kind: "level" (0 is Off) or "sens"; opts.link() -> other paths
  // written with it, whose sliders follow.
  function slider(path, opts = {}) {
    const key = keyOf(app.schema, path) || {};
    const kind = opts.kind || (opts.log || key.log ? "sens" : path.startsWith("haptics_gain.") ? "level" : "");
    const m = sliderScale(key, opts);
    const p = base(path, opts, !opts.field);
    const def = defaultAt(app.schema, path);
    const ticks = typeof def === "number" ? h("datalist", { id: p.ids.ctl + "-ticks" }, h("option", { value: String(m.pos(def)) })) : null;
    const input = h("input", {
      type: "range", class: "range", id: p.ids.ctl, min: String(m.min), max: String(m.max), step: String(m.step),
      list: ticks ? ticks.id : null, "aria-labelledby": p.ids.label,
    });
    const text = opts.field ? null : h("span", { class: "value", "aria-hidden": "true" });
    const field = opts.field ? h("input", {
      class: "text sens", type: "text", inputmode: "decimal", autocomplete: "off", spellcheck: "false",
      id: p.ids.ctl + "-field", "aria-labelledby": p.ids.label,
    }) : null;
    p.control.append(...[input, text, field, ticks].filter(Boolean));
    const c = make(path, opts, p, [input, field].filter(Boolean));
    const fail = c.fail;
    let typed = null; // {raw}: the text of the field's write on its way
    // a refusal of the field's write keeps the typed text; the slider
    // goes back
    c.fail = (t) => {
      if (typed) {
        fail(t, typed.raw);
        c.force = true;
      } else {
        fail(t);
      }
    };
    const fmt = (v) => (kind === "sens" ? sens(v) : plain(v));
    const links = () => (opts.link ? opts.link() || [] : []).filter((x) => x !== path);
    const linked = () => links().map((x) => controls.find((o) => o.path === x && o.preview)).filter(Boolean);

    // show puts v in the value text, aria-valuetext and the field
    function show(v) {
      const t = valueText(key, v, kind);
      if (text) {
        setText(text, t);
      }
      attr(input, "aria-valuetext", t);
      const e = c.error();
      if (field && document.activeElement !== field && !(e && e.raw !== undefined)) {
        field.value = fmt(v);
      }
    }
    c.preview = (v) => {
      input.value = String(m.pos(v));
      show(v);
    };

    input.addEventListener("input", () => {
      const v = m.value(Number(input.value));
      show(v);
      for (const o of linked()) {
        o.preview(v);
      }
    });
    input.addEventListener("change", () => {
      if (locked(app)) {
        return;
      }
      const v = m.value(Number(input.value));
      show(v);
      for (const o of linked()) {
        o.preview(v);
      }
      later(c, [[path, v], ...links().map((x) => [x, v])]);
    });
    if (field) {
      field.addEventListener("change", async () => {
        if (locked(app)) {
          return;
        }
        const raw = field.value;
        const lo = key.min || 0;
        const hi = typeof key.max === "number" ? key.max : Infinity;
        const n = parseNumber(raw);
        if (n === null || n < lo || n > hi) {
          fail("Type a number from " + plain(lo) + " to " + plain(hi) + ".", raw);
          return;
        }
        const v = kind === "sens" ? sensRound(n, key) : valueOf(key, n);
        const pairs = [[path, v], ...links().map((x) => [x, v])];
        giveWay(pairs);
        input.value = String(m.pos(v));
        show(v);
        for (const o of linked()) {
          o.preview(v);
        }
        const mine = { raw };
        typed = mine;
        let ok = false;
        try {
          ok = await write(c, pairs);
        } finally {
          if (typed === mine) {
            typed = null;
          }
        }
        if (ok && opts.after) {
          opts.after(v);
        }
      });
    }
    c.update = (ctx) => {
      c.check(ctx);
      input.disabled = ctx.lock;
      if (field) {
        field.disabled = ctx.lock;
      }
      const v = current(ctx, path);
      c.why(typeof v === "number" ? outside(key, v) : "");
      const e = c.error();
      if (field && e && e.raw !== undefined && document.activeElement !== field && field.value !== e.raw) {
        field.value = e.raw; // a refused number stays with its error
      }
      if (typeof v === "number" && c.takes()) {
        input.value = String(m.pos(v));
        show(v); // the file's own value, also past the slider's end
      }
    };
    c.describe();
    return add(c);
  }

  // text is a text field row, or with opts.compact a table cell: the field
  // and its error (opts.aria: its aria-label). opts.parse(text, ctx) ->
  // {value} or {error}: an error is not sent. opts.show(value) -> text.
  // opts.wide: the field takes the row's width. opts.attrs: the input's
  // own attributes (class, placeholder, inputmode, maxlength). The field
  // keeps a refused text with its error.
  function text(path, opts = {}) {
    const key = keyOf(app.schema, path) || {};
    const parse = opts.parse || ((t) => defaultParse(key, t));
    const showText = opts.show || ((v) => (v === undefined || v === null ? "" : String(v)));
    const input = h("input", { class: "text", type: "text", autocomplete: "off", spellcheck: "false", ...(opts.attrs || {}) });
    if (opts.wide) {
      input.classList.add("wide");
    }
    let p;
    if (opts.compact) {
      const id = opts.id || idOf(path);
      p = {
        ids: { row: id, label: "", help: id + "-help", why: id + "-why", err: id + "-err", ctl: id + "-ctl" },
        help: h("span", { hidden: true }),
        why: h("span", { hidden: true }),
        err: h("p", { class: "field-error", id: id + "-err" }),
      };
      input.id = p.ids.ctl;
      input.setAttribute("aria-label", opts.aria || labelOf(path));
      p.el = h("div", { class: "field cell", "data-setting": path, id }, input, p.err);
    } else {
      p = base(path, opts, true);
      input.id = p.ids.ctl;
      input.setAttribute("aria-labelledby", p.ids.label);
      p.control.append(input);
      if (opts.wide) {
        p.field.classList.add("wide");
      }
    }
    const c = make(path, opts, p, [input]);
    const fail = c.fail;
    let typed = null; // {raw}: the text of the write on its way
    // the typed text stays: the text of the write refused, which the
    // field may have lost meanwhile when the focus had gone
    c.fail = (t) => fail(t, typed ? typed.raw : input.value);
    input.addEventListener("change", async () => {
      if (locked(app)) {
        return;
      }
      const raw = input.value;
      const r = parse(raw, context()) || { error: refusal(null) };
      if (has(r, "error")) {
        fail(r.error, raw);
        return;
      }
      const mine = { raw };
      typed = mine;
      let ok = false;
      try {
        ok = await write(c, [[path, r.value]]);
      } finally {
        if (typed === mine) {
          typed = null;
        }
      }
      if (ok && opts.after) {
        opts.after(r.value);
      }
    });
    c.update = (ctx) => {
      c.check(ctx);
      input.disabled = ctx.lock;
      const v = current(ctx, path);
      if (!opts.compact) {
        c.why(opts.why ? opts.why(v, ctx) : outside(key, v));
      }
      const e = c.error();
      if (e) {
        if (document.activeElement !== input && input.value !== e.raw) {
          input.value = e.raw;
        }
        return;
      }
      if (c.takes()) {
        const t = showText(v);
        if (input.value !== t) {
          input.value = t;
        }
      }
    };
    c.describe();
    return add(c);
  }

  // color is a colour row: a native colour input and a hex field. rgb
  // settings (colors.*) write [r, g, b]; hex settings (hud_colors.*) write
  // "#rrggbb", and null when the field is emptied or Clear is pressed. An
  // unset hex setting is automatic: its picker starts from opts.std (the
  // catalog's standard colour). opts.off: a checkbox Off that writes "off"
  // (the hit flashes').
  function color(path, opts = {}) {
    const key = keyOf(app.schema, path) || {};
    const hex = key.type === "hex";
    const entry = path.split(".").pop();
    const std = opts.std || (hex && has(HUD_COLORS, entry) ? HUD_COLORS[entry][2] : "#000000");
    const offBox = opts.off !== undefined ? Boolean(opts.off) : hex && entry === "flash";
    const name = opts.label !== undefined ? opts.label : labelOf(path);
    // the picker's name: "Hit colour", "HUD main colour"
    const picker = /colou?r$/i.test(name) ? name : name + " colour";
    const p = base(path, opts, false);
    const swatch = h("input", { type: "color", class: "swatch", id: p.ids.ctl, "aria-label": picker });
    const field = h("input", {
      type: "text", class: "text hex", id: p.ids.ctl + "-hex", autocomplete: "off", spellcheck: "false",
      "aria-label": name + " hex", placeholder: hex ? "Automatic" : null,
    });
    const off = offBox ? h("input", { type: "checkbox", class: "box", id: p.ids.ctl + "-off" }) : null;
    const clear = hex ? h("button", { class: "btn quiet small", type: "button", text: "Clear", "aria-label": "Clear " + name, hidden: true }) : null;
    p.control.append(...[swatch, field, off ? h("label", { class: "box-label", for: off.id }, off, h("span", { text: "Off" })) : null, clear].filter(Boolean));
    const c = make(path, opts, p, [swatch, field], [field]);
    const fail = c.fail;
    let typed = null; // {raw}: the hex field's text of the write on its way
    c.fail = (t) => {
      fail(t, typed ? typed.raw : field.value); // the typed text stays, the picker goes back
      c.force = true;
    };

    function send(v) {
      return write(c, [[path, v === null || v === "off" || hex ? v : hexToRgb(v)]]);
    }
    swatch.addEventListener("input", () => {
      field.value = swatch.value; // the colour as it is picked
    });
    swatch.addEventListener("change", () => {
      if (!locked(app)) {
        send(swatch.value.toLowerCase());
      }
    });
    field.addEventListener("change", async () => {
      if (locked(app)) {
        return;
      }
      const raw = field.value;
      const t = raw.trim();
      let n = null; // an emptied hex field: automatic
      if (!(hex && t === "")) {
        n = normHex(t);
        if (!n) {
          fail("Type a colour as #rrggbb.", raw);
          return;
        }
      }
      const mine = { raw };
      typed = mine;
      try {
        await send(n);
      } finally {
        if (typed === mine) {
          typed = null;
        }
      }
    });
    if (off) {
      off.addEventListener("change", () => {
        if (!locked(app)) {
          send(off.checked ? "off" : null);
        }
      });
    }
    if (clear) {
      clear.addEventListener("click", () => {
        if (locked(app)) {
          return;
        }
        swatch.focus(); // the button goes once the row is automatic
        field.value = "";
        send(null);
      });
    }
    c.update = (ctx) => {
      c.check(ctx);
      const v = current(ctx, path);
      c.why(outside(key, v, entry));
      const e = c.error();
      if (e && document.activeElement !== field && field.value !== e.raw) {
        field.value = e.raw;
      }
      if (c.takes()) {
        if (hex) {
          const good = fileHex(v);
          const isoff = Boolean(off) && isOff(v);
          swatch.value = good || std;
          swatch.classList.toggle("unset", !good);
          attr(swatch, "aria-label", picker + (good ? "" : ", automatic"));
          if (off) {
            off.checked = isoff;
          }
          if (!e) {
            field.value = isoff ? "" : good || (typeof v === "string" ? v : "");
          }
        } else {
          swatch.value = rgbToHex(v);
          if (!e) {
            field.value = rgbToHex(v);
          }
        }
      }
      const offNow = Boolean(off && off.checked);
      swatch.disabled = ctx.lock || offNow;
      field.disabled = ctx.lock || offNow;
      attr(field, "placeholder", hex ? (offNow ? "Off" : "Automatic") : null);
      if (off) {
        off.disabled = ctx.lock;
      }
      if (clear) {
        clear.hidden = !(typeof v === "string" && v.trim() !== "" && !isOff(v));
        clear.disabled = ctx.lock;
      }
    };
    c.describe();
    return add(c);
  }

  // card is a settings card: its title, a Reset button for the paths in
  // reset (none: no Reset), help, the rows of body (controls or nodes) and
  // a note under them, note(ctx) -> text ("" hides it).
  function card({ id, title, help, reset: paths, body, note }) {
    const cid = id || "card-" + ++made;
    const head = h("h2", { id: cid + "-title", tabindex: "-1", text: title });
    const helpEl = help ? h("p", { class: "note card-help", id: cid + "-help", text: help }) : null;
    const rows = h("div", { class: "rows" });
    const noteEl = note ? h("p", { class: "note", hidden: true }) : null;
    const el = h("section", { class: "card list", id: cid, "aria-labelledby": head.id, "aria-describedby": helpEl ? helpEl.id : null },
      h("div", { class: "card-head" }, head, paths ? resetButton(title, paths) : null),
      helpEl, rows, noteEl);
    const put = (...items) => {
      for (const b of items.flat()) {
        if (b) {
          rows.append(b.el || b);
        }
      }
    };
    put(body || []);
    function update(ctx = context()) {
      if (noteEl) {
        const t = (typeof note === "function" ? note(ctx) : note) || "";
        setText(noteEl, t);
        noteEl.hidden = !t;
      }
    }
    if (noteEl) {
      live.push(update);
    }
    return { el, title: head, rows, add: put, update };
  }

  // group is a folded group of rows in a card (details), closed at start:
  // its title with the count, a "Reset group" button for reset, and body.
  function group({ id, title, count, reset: paths, body }) {
    const gid = id || "group-" + ++made;
    const rows = h("div", { class: "rows" });
    // named as it reads: "Weapons and flying (15)"
    const summary = h("summary", { "aria-label": title + " (" + count + ")" }, icon("chevron", 14),
      h("span", { text: title }), h("span", { class: "count", text: " (" + count + ")" }));
    const el = h("details", { class: "group", id: gid }, summary,
      h("div", { class: "group-body" },
        paths ? h("div", { class: "group-tools" }, resetButton(title, paths, "Reset group")) : null,
        rows));
    for (const b of [].concat(body || []).flat()) {
      if (b) {
        rows.append(b.el || b);
      }
    }
    return { el, summary, rows };
  }

  // page is a page's frame: its title, lead and note (note(status, ctx)
  // -> text), the broken card, the Apply bar (bar: applyBar's), then the
  // cards (card objects or nodes).
  function page({ title, lead, note, bar: applyBar }, ...cards) {
    bar = applyBar || null;
    const noteEl = h("p", { class: "note page-note", hidden: true });
    titleEl = h("h1", { text: title });
    pageEl = h("div", { class: "page" },
      h("div", {}, titleEl, lead ? h("p", { class: "lead", text: lead }) : null, noteEl),
      broken.el,
      bar ? bar.el : null,
      cards.flat().map((x) => (x && x.el) || x));
    if (note) {
      live.push((ctx) => {
        const t = note(ctx.status, ctx) || "";
        setText(noteEl, t);
        noteEl.hidden = !t;
      });
    }
    return pageEl;
  }

  // update shows the settings and the status in every control. The
  // controls are skipped while nothing they show changed: status events
  // come 4 times a second.
  function update() {
    const ctx = context();
    const focused = typeof document !== "undefined" ? document.activeElement : null;
    // the file is fixed while the broken card, which now goes, has the
    // focus
    const back = !ctx.lock && !broken.el.hidden && Boolean(focused) && broken.el.contains(focused);
    broken.update();
    if (bar) {
      bar.update();
    }
    const st = ctx.status;
    const key = [settingsRev(), ctx.lock, st ? st.kind : "", st ? st.full : "", ed.pending.size, changes].join("|");
    if (key !== shownKey) {
      shownKey = key;
      for (const c of controls) {
        c.update(ctx);
      }
      for (const r of resets) {
        r.update(ctx);
      }
    }
    for (const f of live) {
      f(ctx);
    }
    // the file broke while a control it disables had the focus
    if (ctx.lock && pageEl && focused && focused.disabled === true && pageEl.contains(focused)) {
      before = focused;
      broken.focus();
    }
    // fixed: the focus goes back to that control, else to the title
    if (back) {
      const to = before && before.isConnected && !before.disabled && !before.hidden ? before : null;
      before = null;
      if (to) {
        to.focus({ preventScroll: true });
      }
      // a control in a group closed meanwhile takes no focus
      if (titleEl && (!to || document.activeElement !== to)) {
        titleEl.tabIndex = -1;
        titleEl.focus({ preventScroll: true });
      }
    } else if (!ctx.lock) {
      before = null;
    }
  }

  // leave sends every waiting write now; a refusal shows as a toast.
  function leave() {
    showing.delete(flushNow);
    flush(true);
    left = true;
  }

  const flushNow = () => flush(false);
  showing.add(flushNow);

  return {
    ed,
    broken,
    context,
    value: (path) => current(context(), path),
    page,
    card,
    group,
    toggle,
    choice,
    select,
    slider,
    text,
    color,
    resetButton,
    add(control) {
      return add(control);
    },
    update,
    flush: flushNow,
    cancel,
    leave,
    reset,
    write(control, pairs) {
      return write(control, pairs, false);
    },
  };
}
