// Gyro aim: aim by turning the controller. Who aims, how far the mouse
// moves, calibration, and the panels where the gyro stops.
import { h, setText } from "./dom.js";
import { drift } from "./format.js";
import { locked } from "./cards.js";
import { calibrator } from "./calibrate.js";
import { checkRows, setupWatch } from "./checklist.js";
import { PAGE_LEADS, PANELS, labelOf } from "./catalog.js";
import { form, helpOf, idOf } from "./form.js";
import { focusList, gyroNote, keyOf, outside, sameValue } from "./values.js";

const X = "gyro_sensitivity_x";
const Y = "gyro_sensitivity_y";
const FOCUS = "gyro_off_gui_focus";

// panelList is the checklist of gyro_off_gui_focus: one checkbox per
// panel in a grid. A change writes the whole list at once, sorted and each
// panel once, so entries the file has outside the panels go with it.
function panelList(app, F) {
  const id = idOf(FOCUS);
  const ids = { label: id + "-label", help: id + "-help", why: id + "-why", err: id + "-err" };
  const helpText = helpOf(FOCUS);
  const help = h("span", { class: "row-sub", id: ids.help, text: helpText, hidden: !helpText });
  const why = h("span", { class: "row-why", id: ids.why, hidden: true });
  const err = h("p", { class: "field-error", id: ids.err });
  const boxes = PANELS.map(([n, label]) => {
    const box = h("input", { type: "checkbox", class: "box", id: id + "-" + n });
    return { n, box, el: h("label", { class: "box-label", for: box.id }, box, h("span", { text: label })) };
  });
  const grid = h("div", { class: "checklist", role: "group", "aria-labelledby": ids.label }, boxes.map((b) => b.el));
  const el = h("div", { class: "row wrap", "data-setting": FOCUS, id },
    h("span", { class: "row-label" }, h("span", { id: ids.label, text: labelOf(FOCUS) }), help, why),
    h("div", { class: "field wide" }, grid, err));
  let error = null; // {text, was}: a refused write, until the next one is taken
  let force = false; // take the settings' value once, even with the focus

  function describe() {
    const by = [help.hidden ? "" : ids.help, why.hidden ? "" : ids.why, error ? ids.err : ""].filter(Boolean).join(" ");
    if (by) {
      grid.setAttribute("aria-describedby", by);
    } else {
      grid.removeAttribute("aria-describedby");
    }
  }

  const c = {
    el,
    path: FOCUS,
    paths: [FOCUS],
    focus() {
      const b = boxes.find((x) => !x.box.disabled) || boxes[0];
      b.box.focus();
    },
    // fail shows text under the list, reads it out once, and has the
    // boxes go back to the settings' value
    fail(text) {
      error = { text, was: F.value(FOCUS) };
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
      const v = F.value(FOCUS);
      if (error && !sameValue(v, error.was)) {
        c.clear(); // the setting changed in the file
      }
      for (const b of boxes) {
        b.box.disabled = ctx.lock;
      }
      const text = outside(keyOf(ctx.schema, FOCUS), v);
      setText(why, text);
      if (why.hidden !== !text) {
        why.hidden = !text;
        describe();
      }
      const focused = document.activeElement;
      const takes = force || (ctx.ed.shows(null, FOCUS) && !(focused && grid.contains(focused)));
      force = false;
      if (takes) {
        const list = Array.isArray(v) ? v : [];
        for (const b of boxes) {
          b.box.checked = list.includes(b.n);
        }
      }
    },
  };

  for (const b of boxes) {
    b.box.addEventListener("change", () => {
      if (locked(app)) {
        return;
      }
      const shown = boxes.filter((x) => x.box.checked).map((x) => x.n);
      F.write(c, [[FOCUS, focusList(shown, b.n, b.box.checked)]]);
    });
  }
  describe();
  return F.add(c);
}

export function view(app, title) {
  const F = form(app);

  // 1. Gyro aim, with DS4Windows' gyro and motion checks under the switch
  const checksTitle = h("h3", { id: "gyro-checks-title", text: "With DS4Windows" });
  const checks = checkRows(app, (it) => it.id === "gyro" || it.id === "motion");
  checks.el.setAttribute("aria-labelledby", checksTitle.id);
  const checksErr = h("p", { class: "note", hidden: true });
  const block = h("div", { class: "gyro-checks", hidden: true }, checksTitle, checks.el, checksErr);
  const aim = F.card({
    id: "gyro-card-aim", title: "Gyro aim", reset: ["gyro_aim", "gyro_by"],
    body: [F.toggle("gyro_aim"), F.toggle("gyro_by", { on: "edsense", off: "dsx" }), block],
  });

  // 2. Aim. Link is the page's own state: while it is on, either value
  // writes both.
  const linkBox = h("input", { type: "checkbox", class: "box", id: "gyro-link" });
  const linkRow = h("div", { class: "row tight gyro-link" },
    h("label", { class: "box-label", for: linkBox.id }, linkBox, h("span", { text: "Link sideways and up and down" })));
  const other = (path) => () => (linkBox.checked ? [path === X ? Y : X] : []);
  const sens = F.card({
    id: "gyro-card-sens", title: "Aim", reset: [X, Y, "gyro_roll_mix", "gyro_low_speed"],
    body: [
      linkRow,
      F.slider(X, { field: true, link: other(X) }),
      F.slider(Y, { field: true, link: other(Y) }),
      F.slider("gyro_roll_mix"),
      F.choice("gyro_low_speed"),
    ],
  });

  // 3. Calibration: the drift as the gyro knows it, and Calibrate gyro
  const cal = calibrator(app, "gyro-cal");
  const driftLine = h("p", { class: "gyro-drift", id: "gyro-cal-drift", hidden: true });
  cal.button.setAttribute("aria-describedby", driftLine.id + " " + cal.hint.id);
  const calibration = F.card({
    id: "gyro-card-cal", title: "Calibration", reset: ["gyro_auto_calibrate"],
    body: [F.toggle("gyro_auto_calibrate"), h("div", { class: "gyro-cal" }, driftLine, cal.el)],
  });

  // 4. Gyro off in menus
  const menus = F.card({
    id: "gyro-card-menus", title: "Gyro off in menus", reset: ["gyro_off_in_menus", FOCUS],
    body: [F.toggle("gyro_off_in_menus"), panelList(app, F)],
    note: () => (F.value("gyro_off_in_menus") === false ? "Gyro off in menus is off, so this list is not used." : ""),
  });

  const el = F.page({ title, lead: PAGE_LEADS["gyro"], note: gyroNote }, aim, sens, calibration, menus);

  let kind = null; // status.kind the checks are about
  let setup = null; // setup.check's last answer for DS4Windows
  let setupErr = "";
  let started = false;
  let gone = false;
  let linkSet = false;
  // the checks are asked for only while this page shows, with DS4Windows
  const watch = setupWatch(app, () => (app.status && app.status.kind === "ds4windows" ? "ds4windows" : ""), (got, err) => {
    if (got) {
      setup = got;
      setupErr = "";
    } else {
      setupErr = app.errorText(err);
    }
    if (!gone) {
      update();
    }
  });

  function showChecks() {
    const on = kind === "ds4windows";
    if (on) {
      checks.show(setup ? setup.items : null);
      setText(checksErr, setupErr);
      checksErr.hidden = !setupErr;
    }
    // an answer without either check shows nothing
    const hide = !on || (setup !== null && checks.el.hidden && !setupErr);
    if (hide && !block.hidden && block.contains(document.activeElement)) {
      aim.title.focus({ preventScroll: true }); // the block goes from under the focus
    }
    block.hidden = hide;
  }

  function showDrift() {
    const s = app.status;
    const g = s && s.full && s.gyro ? s.gyro : null;
    setText(driftLine, g ? (g.calibrated ? "Drift " + drift(g.drift) : "Not calibrated yet") : "");
    driftLine.hidden = !g;
  }

  function update() {
    // before F.update, which hands the focus of a control the lock
    // disables to the broken card
    linkBox.disabled = locked(app);
    F.update();
    if (!linkSet && app.settings) {
      linkSet = true;
      linkBox.checked = sameValue(F.value(X), F.value(Y));
    }
    const k = app.status ? app.status.kind || "" : "";
    if (k !== kind) {
      kind = k; // the checks were about the app before
      setup = null;
      setupErr = "";
      if (started && k === "ds4windows") {
        watch.now(false);
      }
    }
    if (!started) {
      started = true;
      watch.start();
    }
    showChecks();
    showDrift();
    cal.update(app.status);
  }

  function leave() {
    gone = true;
    watch.stop();
    F.leave();
  }

  return { el, update, leave };
}
