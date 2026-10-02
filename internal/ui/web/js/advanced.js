// Advanced: the folders, the update interval, the rumble fallback, the
// window's theme, the settings file and Reset all settings.
import { h, icon } from "./dom.js";
import { locked } from "./cards.js";
import { EFFECT_GROUPS, EFFECTS, PAGE_LEADS, SETTINGS } from "./catalog.js";
import { applyBar } from "./applybar.js";
import { filterField, filterGroups, form } from "./form.js";
import { atDefault, defaultAt, keyOf, outside, parseNumber, refusal, valueAt } from "./values.js";

const THEMES = [["system", "System"], ["light", "Light"], ["dark", "Dark"]];

// The rumble table's columns, and the two strength fields of each row.
const COLUMNS = ["Effect", "Left", "Right", "Length"];
const SIDES = [["left", "Left"], ["right", "Right"]];

const PERCENT_ERROR = "Type a whole number from 0 to 100.";
const MS_ERROR = "Type a whole number of milliseconds.";

// editable is every setting a patch may change, by its name in the file:
// all but backend and config_version.
export function editable(schema) {
  return (schema || []).filter((k) => k.editable).map((k) => k.path.replace(/\.\*$/, ""));
}

// rumbleGroups are the effect groups with only the keys rumble has, in
// their order; a group without one is left out.
export function rumbleGroups(schema) {
  const row = (schema || []).find((k) => k.path === "rumble.*");
  const keys = new Set(row && Array.isArray(row.keys) ? row.keys : []);
  return EFFECT_GROUPS.map((g) => ({ title: g.title, keys: g.keys.filter((k) => keys.has(k)) }))
    .filter((g) => g.keys.length > 0);
}

// continuous: an effect that plays while its state lasts, so its length
// is not used. Its Schema default has a length of 0.
export function continuous(schema, key) {
  const d = defaultAt(schema, "rumble." + key);
  return Boolean(d) && typeof d === "object" && d.ms === 0;
}

// parsePercent reads a left or right cell: a whole number from 0 to 100,
// written as a share of 1 with 2 decimals.
export function parsePercent(text) {
  const n = parseNumber(text, true);
  if (n === null || n < 0 || n > 100) {
    return { error: PERCENT_ERROR };
  }
  return { value: Number((n / 100).toFixed(2)) };
}

// parseMs reads a length cell: a whole number of 0 or more. Go says how
// long it may be.
export function parseMs(text) {
  const n = parseNumber(text, true);
  return n === null || n < 0 ? { error: MS_ERROR } : { value: n };
}

export function view(app, title) {
  const F = form(app);
  const mine = []; // this page's setting controls, for Reset all
  let left = false;

  // Folders
  const folder = (path) => F.text(path, {
    wide: true,
    attrs: { placeholder: "Elite's standard folder" },
    parse: (t) => ({ value: String(t).trim() }),
  });
  const journal = folder("journal_dir");
  const bindings = folder("bindings_dir");
  const folders = F.card({
    id: "adv-folders", title: SETTINGS["journal_dir"].card, reset: ["journal_dir", "bindings_dir"],
    body: [journal, bindings],
  });

  // Timing
  const poll = F.slider("poll_ms");
  const timing = F.card({ id: "adv-timing", title: SETTINGS["poll_ms"].card, reset: ["poll_ms"], body: [poll] });
  mine.push(journal, bindings, poll);

  // Rumble fallback
  const rumble = rumbleCard(app, F, mine);

  // Appearance: the window's own state, so it has no Reset
  const radios = THEMES.map(([id]) => h("input", { type: "radio", name: "theme", value: id, onchange: () => app.setTheme(id) }));
  const seg = h("div", { class: "seg", role: "radiogroup", "aria-labelledby": "theme-label", "aria-describedby": "theme-sub" },
    THEMES.map(([, label], i) => h("label", {}, radios[i], h("span", { text: label }))));
  const appearance = F.card({
    id: "adv-appearance", title: "Appearance",
    body: [h("div", { class: "row wrap" },
      h("span", { class: "row-label" },
        h("span", { id: "theme-label", text: "Theme" }),
        h("span", { class: "row-sub", id: "theme-sub", text: "System follows the light or dark mode of Windows." })),
      seg)],
  });

  // Settings file
  const open = (which) => () => app.call("file.open", { which }).catch((err) => app.toast(app.errorText(err), true));
  const file = F.card({
    id: "adv-file", title: "Settings file",
    body: [h("div", { class: "row" }, h("div", { class: "actions" },
      h("button", { class: "btn", type: "button", onclick: open("settings") }, icon("file"), h("span", { text: "Open edsense.json" })),
      h("button", { class: "btn quiet", type: "button", onclick: open("log") }, h("span", { text: "Open log" }))))],
  });

  // Reset all settings
  let resetting = false;
  const allBtn = h("button", {
    class: "btn danger", type: "button", "aria-describedby": "adv-reset-text", text: "Reset all settings...",
    onclick: () => resetAll(),
  });
  const all = F.card({
    id: "adv-reset", title: "Reset all settings",
    body: [h("div", { class: "row wrap" },
      h("span", { class: "row-label", id: "adv-reset-text", text: "Every setting on these pages goes back to its default. The controller app stays." }),
      allBtn)],
  });

  // atDefaults: every editable setting is at its default, with no write
  // waiting or on its way
  function atDefaults(ctx) {
    return ctx.ed.pending.size === 0 && atDefault(ctx.schema, ctx.config, editable(ctx.schema));
  }
  F.add({
    el: null,
    paths: [],
    update(ctx) {
      allBtn.disabled = ctx.lock;
      if (atDefaults(ctx)) {
        allBtn.setAttribute("aria-disabled", "true");
      } else {
        allBtn.removeAttribute("aria-disabled");
      }
    },
    focus() {
      allBtn.focus();
    },
  });

  // resetAll asks first, then puts every editable setting back to its
  // default in one patch of nulls. backend and config_version stay.
  async function resetAll() {
    const ctx = F.context();
    if (ctx.lock || resetting) {
      return;
    }
    if (atDefaults(ctx)) {
      app.announce("Already at the defaults.", false);
      return;
    }
    resetting = true;
    try {
      const yes = await app.confirm({
        title: "Reset all settings?",
        text: "Every setting goes back to its default, on every page.",
        notes: ["The controller app stays as it is. The ports, folders and update interval wait for Apply now."],
        ok: "Reset all",
        danger: true,
      });
      if (!yes || left || locked(app)) {
        return;
      }
      const names = editable(app.schema);
      F.cancel(names);
      for (const c of mine) {
        c.clear();
      }
      const reply = await F.ed.setMany(names.map((n) => [n, null]));
      if (reply && reply.applied) {
        app.announce("All settings are back to their defaults.", false);
      } else {
        const problems = reply && reply.problems && reply.problems.length > 0 ? reply.problems : [null];
        for (const p of problems) {
          app.toast(refusal(p), true);
        }
      }
    } catch (err) {
      app.toast(app.errorText(err), true);
    } finally {
      resetting = false;
      app.refresh();
    }
  }

  const el = F.page({ title, lead: PAGE_LEADS["advanced"], bar: applyBar(app, () => folders.title) },
    folders, timing, rumble, appearance, file, all);

  function update() {
    F.update();
    for (const r of radios) {
      r.checked = r.value === app.theme;
    }
  }

  function leave() {
    left = true;
    F.leave();
  }

  return { el, update, leave };
}

// rumbleCard is the Rumble fallback card: the filter, then a folded table
// per effect group. Each cell writes its own field of the entry. mine
// gets the cells, for Reset all.
function rumbleCard(app, F, mine) {
  const schema = app.schema || [];
  const key = keyOf(schema, "rumble.*");
  const box = filterField("adv-rumble-filter", "Filter effects");
  const lines = []; // [cell, field, column]: the cells whose line says what the file has
  const groups = rumbleGroups(schema).map((g, i) => {
    const rows = g.keys.map((k) => rumbleRow(app, F, k, mine, lines));
    const table = h("table", { class: "grid rumble" },
      h("caption", { class: "sr-only", text: g.title }),
      h("colgroup", {}, h("col", {}), h("col", { class: "num" }), h("col", { class: "num" }), h("col", { class: "len" })),
      h("thead", {}, h("tr", {}, COLUMNS.map((t) => h("th", { scope: "col", text: t })))),
      h("tbody", {}, rows.map((r) => r.el)));
    const group = F.group({
      id: "adv-rumble-" + (i + 1), title: g.title, count: g.keys.length,
      reset: g.keys.map((k) => "rumble." + k),
      body: [h("div", { class: "rumble-scroll" }, table)],
    });
    return { el: group.el, rows };
  });
  filterGroups(box.input, groups, box.none, (text) => app.announce(text, false));

  // a strength in the file past what a cell takes shows under the
  // effect's name, and the cell keeps the file's number
  F.add({
    el: null,
    paths: [],
    update(ctx) {
      for (const [cell, field, name] of lines) {
        const entryPath = cell.path.slice(0, cell.path.lastIndexOf("."));
        const entry = valueAt(ctx.config, entryPath);
        const text = outside(key, entry === undefined ? defaultAt(ctx.schema, entryPath) : entry, field);
        cell.why(text ? name + ": " + text : "");
      }
    },
    focus() {},
  });

  return F.card({
    id: "adv-rumble", title: SETTINGS["rumble"].card, help: SETTINGS["rumble"].help, reset: ["rumble"],
    body: [box.el, ...groups.map((g) => g.el), box.none],
  });
}

// rumbleRow is one effect's table row: its name, then the left and right
// strengths in % and the length in ms ("Continuous" for an effect that
// lasts while its state does). The lines about the file's values and the
// cells' errors show under the name, where the column is wide enough;
// each says its column.
function rumbleRow(app, F, k, mine, lines) {
  const [label, help] = EFFECTS[k] || [k, ""];
  const path = "rumble." + k;
  const cell = (field, column, aria, parse, show) => F.text(path + "." + field, {
    compact: true,
    aria,
    prefix: column + ": ",
    attrs: { class: "text cell", inputmode: "numeric" },
    parse,
    show,
  });
  const strengths = SIDES.map(([side, name]) => {
    const c = cell(side, name, label + " " + side + ", percent", parsePercent,
      (v) => (typeof v === "number" ? String(Math.round(v * 100)) : ""));
    lines.push([c, side, name]);
    return c;
  });
  const length = continuous(app.schema, k) ? null : cell("ms", COLUMNS[3], label + " length, milliseconds", parseMs,
    (v) => (typeof v === "number" ? String(v) : ""));
  const cells = strengths.concat(length ? [length] : []);
  mine.push(...cells);

  const head = h("th", { scope: "row" }, h("span", { class: "rumble-name", text: label }));
  for (const c of strengths) {
    const why = c.parts.why;
    why.id = c.parts.ids.why;
    why.className = "row-why";
    head.append(why);
  }
  for (const c of cells) {
    head.append(c.parts.err);
  }
  const el = h("tr", { "data-setting": path },
    head,
    strengths.map((c) => h("td", {}, c.el)),
    h("td", {}, length ? length.el : h("span", { class: "muted rumble-cont", text: "Continuous" })));
  return { el, text: label + " " + help + " " + k };
}
