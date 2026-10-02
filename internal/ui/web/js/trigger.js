// One trigger's editor on the Triggers page: its mode, a select for each
// of the mode's values, and a line that says what it does. Every change
// writes the whole trigger at once: {"mode": M, "params": [...]}.
import { h, setText } from "./dom.js";
import { MODES } from "./catalog.js";
import { fitParams, keyOf, modeRow, outside, sameValue, setParam, startParams, triggerSummary } from "./values.js";
import { idOf } from "./form.js";

function isObject(v) {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

function has(obj, k) {
  return Object.prototype.hasOwnProperty.call(obj, k);
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

// modeNames are the trigger modes in the Schema's order, or the
// catalog's when the Schema has none.
export function modeNames(schema) {
  const row = (schema || []).find((k) => k.path === "triggers.*");
  const modes = row && Array.isArray(row.modes) ? row.modes.map((m) => m.name) : [];
  return modes.length > 0 ? modes : Object.keys(MODES);
}

// modeName is the mode the app uses for name: the Schema's mode of that
// name in any case, as the app reads it; "" when there is none (the app
// then uses the trigger as Off).
export function modeName(schema, name) {
  if (typeof name !== "string" || name === "") {
    return "";
  }
  if (modeRow(schema, name)) {
    return name;
  }
  const up = name.toUpperCase();
  return modeRow(schema, up) ? up : "";
}

// shownTrigger is trigger v of key as the editor shows it: {mode, params,
// unknown}. A mode the app does not know has no values. Values that do not
// fit the mode are fitted, with missing ones from the mode's start values.
export function shownTrigger(schema, key, v) {
  const t = isObject(v) ? v : {};
  const name = modeName(schema, t.mode);
  if (!name) {
    return { mode: typeof t.mode === "string" ? t.mode : "", params: [], unknown: true };
  }
  const mode = modeRow(schema, name);
  const list = Array.isArray(t.params) ? t.params : [];
  const start = startParams(schema, key, name);
  const full = mode.params.map((_, i) => (typeof list[i] === "number" && Number.isFinite(list[i]) ? list[i] : start[i]));
  return { mode: name, params: fitParams(mode, full), unknown: false };
}

// paramRange is the [min, max] that value i of mode offers after the
// values before it: the value at "above" starts past the one before it.
export function paramRange(mode, params, i) {
  const [lo, hi] = mode.params[i];
  if (mode.above > 0 && i === mode.above) {
    return [Math.min(Math.max(lo, params[i - 1] + 1), hi), hi];
  }
  return [lo, hi];
}

// whyText is the line under the editor when the file has values its mode
// does not take, or "".
export function whyText(schema, v) {
  if (!isObject(v)) {
    return "";
  }
  const name = modeName(schema, v.mode);
  return name ? outside(keyOf(schema, "triggers.*"), { mode: name, params: v.params }) : "";
}

// unknownText is the extra option of a mode the app does not know.
export function unknownText(mode) {
  return mode === "" ? "No mode (used as Off)" : mode + " (unknown, used as Off)";
}

// valueLabel is the label of value i of mode name: "Start", "1".
function valueLabel(name, i) {
  const m = MODES[name];
  return m && m.params[i] ? m.params[i] : "Value " + (i + 1);
}

// triggerEditor is the editor of triggers.<key>, a control for F.add.
// opts.name: the trigger it sets ("R2", "L2", "R2 and L2");
// opts.labelledBy: the id of the situation's label, read before the name;
// opts.describedBy: the id of the situation's help. Each select is named
// with the situation and the trigger: "Hardpoints out R2 Mode".
export function triggerEditor(F, app, key, opts = {}) {
  const path = "triggers." + key;
  const id = idOf(path);
  const ids = {
    name: id + "-name", mode: id + "-mode", modeLabel: id + "-mode-label", pos: id + "-pos",
    why: id + "-why", err: id + "-err", sum: id + "-sum",
  };
  const schema = () => app.schema || [];
  const named = (...more) => [opts.labelledBy, ids.name, ...more].filter(Boolean).join(" ");

  const modeSel = h("select", { class: "select", id: ids.mode, "aria-labelledby": named(ids.modeLabel) },
    modeNames(schema()).map((n) => h("option", { value: n, text: MODES[n] ? MODES[n].label : n })));
  const area = h("div", { class: "trigger-values" });
  const why = h("span", { class: "row-why", id: ids.why, hidden: true });
  const err = h("p", { class: "field-error", id: ids.err });
  const sum = h("p", { class: "note trigger-sum", id: ids.sum });
  const el = h("div", {
    class: "trigger", role: "group", id, "data-setting": path,
    "aria-labelledby": named(),
    "aria-describedby": opts.describedBy || null,
  },
  h("div", { class: "trigger-head" }, h("span", { class: "trigger-name", id: ids.name, text: opts.name || "" })),
  h("div", { class: "param trigger-mode" }, h("label", { for: ids.mode, id: ids.modeLabel, text: "Mode" }), modeSel),
  area, why, err, sum);

  let shown = null; // {mode, params, unknown}: what the selects show
  let values = []; // the value selects of the mode built
  let ranges = []; // each value select's [min, max] as built
  let built = null; // the mode the value selects are for ("": none)
  let extra = null; // the option of a mode the app does not know
  let lock = false;
  let force = false; // take the settings' value once, even with the focus
  let error = null; // {text, was}
  // kept: each mode's values as last shown, so a mode picked again, as
  // the arrow keys do on the way past it, gets them back
  const kept = {};
  // pushed: by mode, the value at "above" the player had before a value
  // before it pushed it along; it comes back as that value moves back
  let pushed = {};

  function selects() {
    return [modeSel, ...values];
  }

  // describe points each select at the lines under the editor: the mode's
  // at the summary too.
  function describe() {
    const lines = [why.hidden ? "" : ids.why, error ? ids.err : ""].filter(Boolean);
    attr(modeSel, "aria-describedby", [ids.sum, ...lines].join(" "));
    for (const s of values) {
      attr(s, "aria-describedby", lines.length > 0 ? lines.join(" ") : null);
    }
    for (const s of selects()) {
      attr(s, "aria-invalid", error ? "true" : null);
    }
  }

  // build makes the value selects of mode (null: none). When one of the
  // old ones had the focus, the mode select takes it.
  function build(mode) {
    const focused = typeof document !== "undefined" ? document.activeElement : null;
    const had = Boolean(focused) && focused !== area && area.contains(focused);
    const name = mode ? mode.name : "";
    const group = MODES[name] && MODES[name].group;
    const from = group ? group.from : (mode ? mode.params.length : 0);
    values = (mode ? mode.params : []).map((_, i) => {
      const vid = id + "-v" + i;
      // named "Hardpoints out R2 Start", "... Strength at each position 3"
      const sel = h("select", { class: "select", id: vid, "aria-labelledby": named(...(i >= from ? [ids.pos] : []), vid + "-label") });
      sel.addEventListener("change", () => pick(i, sel));
      return sel;
    });
    ranges = values.map(() => null);
    const item = (i) => h("div", { class: "param" }, h("label", { for: values[i].id, id: values[i].id + "-label", text: valueLabel(name, i) }), values[i]);
    const parts = [];
    if (from > 0) {
      parts.push(h("div", { class: "params" }, values.slice(0, from).map((_, i) => item(i))));
    }
    if (group) {
      parts.push(h("div", { class: "positions", role: "group", "aria-labelledby": ids.pos },
        h("span", { class: "positions-label", id: ids.pos, text: group.label }),
        h("div", { class: "params" }, values.slice(from).map((_, j) => item(from + j)))));
    }
    area.replaceChildren(...parts);
    built = name;
    if (had) {
      modeSel.focus();
    }
  }

  // render shows shown in the selects and the summary.
  function render() {
    if (shown.unknown) {
      if (!extra) {
        extra = h("option", { value: "" });
        modeSel.append(extra);
      }
      setText(extra, unknownText(shown.mode));
      modeSel.value = "";
      extra.selected = true;
    } else {
      if (extra) {
        extra.remove();
        extra = null;
      }
      modeSel.value = shown.mode;
    }
    const mode = shown.unknown ? null : modeRow(schema(), shown.mode);
    if (built !== (mode ? mode.name : "")) {
      build(mode);
    }
    values.forEach((sel, i) => {
      const [lo, hi] = paramRange(mode, shown.params, i);
      const was = ranges[i];
      if (!was || was[0] !== lo || was[1] !== hi) {
        const options = [];
        for (let n = lo; n <= hi; n++) {
          options.push(h("option", { value: String(n), text: String(n) }));
        }
        sel.replaceChildren(...options);
        ranges[i] = [lo, hi];
      }
      sel.value = String(shown.params[i]);
    });
    for (const s of selects()) {
      s.disabled = lock;
    }
    setText(sum, triggerSummary(shown.unknown ? null : shown));
    describe();
    if (mode) {
      kept[mode.name] = shown.params.slice();
    }
  }

  // send shows next and writes it whole. While edsense.json is broken the
  // selects go back to what they showed.
  async function send(next) {
    if (F.context().lock) {
      render();
      return;
    }
    shown = next;
    render();
    const ok = await F.write(c, [[path, { mode: next.mode, params: next.params.slice() }]]);
    if (!ok && F.ed.shows(null, path)) {
      // refused, also as a toast: back to the settings' value, unless a
      // newer write of this trigger is on its way
      shown = shownTrigger(schema(), key, F.value(path));
      render();
    }
  }

  modeSel.addEventListener("change", () => {
    if (extra && modeSel.selectedOptions[0] === extra) {
      return; // the file's own word again
    }
    const mode = modeRow(schema(), modeSel.value);
    if (mode) {
      const start = has(kept, mode.name) ? kept[mode.name] : startParams(schema(), key, mode.name);
      send({ mode: mode.name, params: fitParams(mode, start), unknown: false });
    }
  });

  function pick(i, sel) {
    const mode = shown && !shown.unknown ? modeRow(schema(), shown.mode) : null;
    if (!mode) {
      return;
    }
    const next = setParam(mode, shown.params, i, Number(sel.value));
    const a = mode.above;
    if (a > 0 && i === a) {
      delete pushed[mode.name]; // the player set it
    } else if (a > 0 && i === a - 1) {
      // the value at "above" follows the one before it back down to the
      // value the player had, as far as that one lets it
      const own = has(pushed, mode.name) ? pushed[mode.name] : shown.params[a];
      const [lo, hi] = paramRange(mode, next, a);
      next[a] = Math.min(Math.max(own, lo), hi);
      if (next[a] === own) {
        delete pushed[mode.name];
      } else {
        pushed[mode.name] = own;
      }
    }
    send({ mode: mode.name, params: next, unknown: false });
  }

  // takes: the editor may show the settings' value now: no write of it is
  // on its way, and the focus is elsewhere.
  function takes(ctx) {
    if (force) {
      force = false;
      return true;
    }
    const focused = typeof document !== "undefined" ? document.activeElement : null;
    return ctx.ed.shows(null, path) && !(focused && el.contains(focused));
  }

  const c = {
    el,
    path,
    paths: [path],
    update(ctx) {
      const file = F.value(path);
      if (error && !sameValue(file, error.was)) {
        c.clear();
      }
      lock = ctx.lock;
      const line = whyText(ctx.schema, file);
      setText(why, line);
      why.hidden = !line;
      if (shown === null || takes(ctx)) {
        const next = shownTrigger(ctx.schema, key, file);
        if (shown !== null && !sameValue(next, shown)) {
          pushed = {}; // changed elsewhere, or refused
        }
        shown = next;
      }
      render();
    },
    focus() {
      modeSel.focus();
    },
    // fail shows text under the editor and reads it out once; the
    // selects go back to the settings' value.
    fail(text) {
      error = { text, was: F.value(path) };
      force = true;
      setText(err, text);
      describe();
      app.announce(text, true);
    },
    clear() {
      if (!error) {
        return;
      }
      error = null;
      setText(err, "");
      describe();
    },
  };
  return c;
}
