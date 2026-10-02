// Feel: how the controller's haptics answer the game. Haptics, the turn
// and jump feel, the weapon feel per fire group, the multi-cannon spin-up
// and the level of each effect.
import { h, setText } from "./dom.js";
import { locked } from "./cards.js";
import { EFFECT_GROUPS, PAGE_LEADS, SETTINGS, SPIN_SIZES, WEAPONS, labelOf } from "./catalog.js";
import { filterField, filterGroups, form, helpOf, idOf } from "./form.js";
import { groupOrder, keyOf, nextGroup, sameValue } from "./values.js";

// The note under the Haptics card's rows while haptics are off.
const HAPTICS_OFF = "Haptics are off, so nothing on this page is felt.";

// A fire group's two selects: the field in the file and its label.
const FIELDS = [["primary", "R2"], ["secondary", "L2"]];

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

function focusedEl() {
  return typeof document !== "undefined" ? document.activeElement : null;
}

// fireGroups is the Fire groups card's body: a row per group in the
// file, by number, and Add fire group. Rows are kept by number, built
// once and updated in place, so the focus stays. title() is the card's
// title, which takes the focus when a focused row goes. status(s) shows
// the Now chip.
function fireGroups(app, F, title) {
  const key = keyOf(app.schema, "fire_groups.1") || {};
  const words = (key.enum || Object.keys(WEAPONS)).map((w) => [w, WEAPONS[w] || w]);
  const known = (w) => words.some(([o]) => o === w);
  const list = h("div", { class: "rows fg-list" });
  const rows = new Map(); // group number -> row
  let focusNew = ""; // the group Add wrote: its R2 select takes the focus once it shows
  let focusAdd = false; // Remove with Add hidden: Add takes the focus once it shows

  const addErr = h("p", { class: "field-error", id: "feel-fg-add-err" });
  const addBtn = h("button", { class: "btn small", type: "button", id: "feel-fg-add", text: "Add fire group" });
  // the control Add writes for: its refusal shows under the button
  const adder = {
    el: null,
    paths: [],
    fail(text) {
      setText(addErr, text);
      attr(addBtn, "aria-describedby", addErr.id);
      app.announce(text, true);
    },
    clear() {
      setText(addErr, "");
      attr(addBtn, "aria-describedby", null);
    },
  };

  // groups is the fire groups in the file, with the ones on their way
  // from Add, so a second click picks the next number.
  function groups() {
    const fg = F.value("fire_groups");
    const all = { ...(fg && typeof fg === "object" ? fg : {}) };
    for (const [at, mark] of F.ed.pending) {
      const m = /^fire_groups\.([0-9]+)$/.exec(at);
      if (m && mark.value !== null) {
        all[m[1]] = mark.value;
      }
    }
    return all;
  }

  addBtn.addEventListener("click", async () => {
    if (locked(app)) {
      return;
    }
    const n = nextGroup(groups(), key);
    if (!n) {
      return;
    }
    const path = "fire_groups." + n;
    adder.paths = [path];
    adder.clear();
    focusNew = n;
    if (!await F.write(adder, [[path, { primary: "auto", secondary: "auto" }]]) && focusNew === n) {
      focusNew = "";
    }
  });

  function makeRow(n) {
    const path = "fire_groups." + n;
    const id = idOf(path);
    const helpText = helpOf(path);
    const name = h("span", { id: id + "-label", text: labelOf(path) });
    // the Now chip, and what a screen reader hears in its place: the
    // selects are described by it while it shows
    const chip = h("span", { class: "chip accent", text: "Now", "aria-hidden": "true", hidden: true });
    const now = h("span", { class: "sr-only", id: id + "-now", text: "In use now.", hidden: true });
    const help = h("span", { class: "row-sub", id: id + "-help", text: helpText, hidden: !helpText });
    const err = h("p", { class: "field-error", id: id + "-err" });
    const picks = FIELDS.map(([field, short]) => {
      const sid = idOf(path + "." + field);
      const tag = h("span", { id: sid + "-tag", text: short });
      const sel = h("select", { class: "select", id: sid, "aria-labelledby": name.id + " " + tag.id },
        words.map(([w, label]) => h("option", { value: w, text: label })));
      return { field, sel, extra: null, label: h("label", { class: "fg-pick", for: sid }, tag, sel) };
    });
    const remove = n === "1" || n === "2" ? null : h("button", {
      class: "btn quiet small", type: "button", text: "Remove", "aria-label": "Remove fire group " + n,
    });
    // Remove sits after the name, as Reset does after a card's title, so
    // the selects of every row line up
    const el = h("div", { class: "row wrap fire-group", "data-setting": path, id },
      h("span", { class: "row-label" }, h("span", { class: "fg-name" }, name, chip, now, remove), help),
      h("div", { class: "field" },
        h("div", { class: "control", role: "group", "aria-labelledby": name.id }, picks.map((p) => p.label)),
        err));

    let error = null; // {text, was}: a refused write and the group it was refused for
    let force = false; // take the settings' value once, even with the focus

    // unknown: p shows a word that is not a weapon type
    const unknown = (p) => Boolean(p.extra) && p.sel.selectedOptions[0] === p.extra;

    // show puts the file's word w in p: "" is Auto, a word that is not a
    // weapon type an extra option "<word> (unknown)"
    function show(p, w) {
      const v = w === undefined || w === null || w === "" ? "auto" : w;
      if (typeof v === "string" && known(v)) {
        if (p.extra) {
          p.extra.remove();
          p.extra = null;
        }
        p.sel.value = v;
        return;
      }
      if (!p.extra) {
        p.extra = h("option", { value: "" });
        p.sel.append(p.extra);
      }
      setText(p.extra, String(v) + " (unknown)");
      p.extra.selected = true;
    }

    function describe() {
      const by = [now.hidden ? "" : now.id, helpText ? help.id : "", error ? err.id : ""].filter(Boolean).join(" ");
      for (const p of picks) {
        attr(p.sel, "aria-describedby", by || null);
        attr(p.sel, "aria-invalid", error ? "true" : null);
      }
    }

    const c = {
      el,
      path,
      paths: [path],
      focus() {
        picks[0].sel.focus();
      },
      // fail shows text under the selects, reads it out once, and has
      // them go back to the settings' value
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
      update(ctx) {
        const g = F.value(path);
        if (error && !sameValue(g, error.was)) {
          c.clear(); // the group changed in the file
        }
        for (const p of picks) {
          p.sel.disabled = ctx.lock;
        }
        if (remove) {
          remove.disabled = ctx.lock;
        }
        if (!g || typeof g !== "object") {
          return; // not in the file: the row is not shown
        }
        const focused = focusedEl();
        for (const p of picks) {
          if (force || (p.sel !== focused && F.ed.shows(null, path + "." + p.field))) {
            show(p, g[p.field]);
          }
        }
        force = false;
      },
    };

    // A select writes the whole group, with both values as shown. When the
    // other select shows a word that is not a weapon type, only this one
    // is written, so the file keeps that word: EDSense takes no such word
    // in a change.
    for (const p of picks) {
      p.sel.addEventListener("change", () => {
        if (unknown(p)) {
          return; // the file's own word again
        }
        const other = picks.find((o) => o !== p);
        const pairs = unknown(other) ? [[path + "." + p.field, p.sel.value]]
          : [[path, { primary: picks[0].sel.value, secondary: picks[1].sel.value }]];
        F.write(c, pairs);
      });
    }
    if (remove) {
      remove.addEventListener("click", async () => {
        if (locked(app)) {
          return;
        }
        if (addBtn.hidden) {
          focusAdd = true; // Add shows once the group is gone
        } else {
          addBtn.focus();
        }
        if (!await F.write(c, [[path, null]])) {
          focusAdd = false;
        }
      });
    }
    describe();
    // inUse shows or hides the Now chip
    function inUse(on) {
      if (chip.hidden === on) {
        chip.hidden = !on;
        now.hidden = !on;
        describe();
      }
    }
    return { n, el, chip, inUse, control: c };
  }

  const block = {
    el: null,
    paths: [],
    update(ctx) {
      const all = F.value("fire_groups");
      const order = groupOrder(all && typeof all === "object" ? all : {}, key);
      let lost = false;
      for (const r of rows.values()) {
        if (!order.includes(r.n) && r.el.parentNode === list) {
          lost = lost || r.el.contains(focusedEl());
          r.el.remove();
        }
      }
      // the rows there stay in place, so the one with the focus keeps it
      order.forEach((n, i) => {
        let r = rows.get(n);
        if (!r) {
          r = makeRow(n);
          rows.set(n, r);
          F.add(r.control);
        }
        const at = list.children[i];
        if (at !== r.el) {
          list.insertBefore(r.el, at || null);
          r.control.update(ctx);
        }
      });
      addBtn.hidden = nextGroup(all, key) === "";
      addBtn.disabled = ctx.lock;
      if (focusNew && order.includes(focusNew)) {
        const f = focusedEl();
        if (!f || f === document.body || f === addBtn) {
          rows.get(focusNew).control.focus();
        }
        focusNew = "";
      }
      if (lost) {
        if (focusAdd && !addBtn.hidden) {
          addBtn.focus();
        } else {
          title().focus();
        }
      }
      if (lost || !addBtn.hidden) {
        focusAdd = false;
      }
    },
  };
  F.add(block);

  return {
    el: h("div", { class: "fire-groups" }, list, h("div", { class: "fg-add" }, addBtn, addErr)),
    // status shows the Now chip on the fire group the game uses
    status(s) {
      const now = s && s.fire_group > 0 ? String(s.fire_group) : "";
      for (const r of rows.values()) {
        r.inUse(r.n === now);
      }
    },
  };
}

// effectLevels is the Effect levels card: a filter over six folded groups
// of sliders, one per effect.
function effectLevels(app, F) {
  const s = SETTINGS["haptics_gain"];
  const box = filterField("feel-filter");
  const groups = EFFECT_GROUPS.map((g, i) => {
    const paths = g.keys.map((k) => "haptics_gain." + k);
    const sliders = paths.map((p) => F.slider(p, { tight: true }));
    const grp = F.group({ id: "feel-levels-" + (i + 1), title: g.title, count: g.keys.length, reset: paths, body: sliders });
    return {
      el: grp.el,
      rows: sliders.map((c, j) => ({ el: c.el, text: [labelOf(paths[j]), helpOf(paths[j]), g.keys[j]].join(" ") })),
    };
  });
  filterGroups(box.input, groups, box.none, (text) => app.announce(text, false));
  return F.card({
    id: "feel-levels", title: s.card, help: s.help, reset: ["haptics_gain"],
    body: [box.el, ...groups.map((g) => g.el), box.none],
  });
}

export function view(app, title) {
  const F = form(app);
  const haptics = F.card({
    id: "feel-haptics",
    title: SETTINGS["control_haptics"].card,
    reset: ["control_haptics", "haptics_strength", "haptics_mode"],
    body: [F.toggle("control_haptics"), F.slider("haptics_strength"), F.choice("haptics_mode")],
    note: () => (F.value("control_haptics") === false ? HAPTICS_OFF : ""),
  });
  const turns = F.card({
    id: "feel-turns",
    title: SETTINGS["turn_feel"].card,
    reset: ["turn_feel", "jump_feel"],
    body: [F.choice("turn_feel"), F.choice("jump_feel")],
  });
  let fireTitle = null;
  const groups = fireGroups(app, F, () => fireTitle);
  const fire = F.card({
    id: "feel-fire-groups",
    title: SETTINGS["fire_groups"].card,
    help: SETTINGS["fire_groups"].help,
    reset: ["fire_groups"],
    body: [groups.el],
  });
  fireTitle = fire.title;
  const spin = F.card({
    id: "feel-spin-up",
    title: SETTINGS["spin_up_ms"].card,
    help: SETTINGS["spin_up_ms"].help,
    reset: ["spin_up_ms"],
    body: Object.keys(SPIN_SIZES).map((size) => F.slider("spin_up_ms." + size)),
  });
  const el = F.page({ title, lead: PAGE_LEADS["feel"] }, haptics, turns, fire, spin, effectLevels(app, F));
  return {
    el,
    update() {
      F.update();
      groups.status(app.status);
    },
    leave: F.leave,
  };
}
