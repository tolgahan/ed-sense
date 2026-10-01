// Controller: which app EDSense drives the controller through, how it
// reaches that app, and how the app is set up.
import { h, setButton, setText } from "./dom.js";
import { capital } from "./format.js";
import { brokenCard, locked } from "./cards.js";
import { edits, keyOf, valueAt } from "./edits.js";
import { cardItems, checkRows, poll, setupRows, setupWatch } from "./checklist.js";
import { JOB_STATES, profileCard } from "./profilecard.js";

// The controller apps by the names the settings use.
export const NAMES = { dsx: "DSX", ds4windows: "DS4Windows" };

// The choices, with the tray's tips as their sub lines.
const APPS = [
  { id: "auto", name: "Auto", sub: "The one that runs. With both, the one that answers." },
  { id: "dsx", name: "DSX", sub: "DSX, with its Incoming UDP" },
  { id: "ds4windows", name: "DS4Windows", sub: "DS4Windows 5, with its game mod support" },
];

const LATER = ["Native USB", "Xbox controllers"];

// The port each app is reached on.
const PORTS = {
  ds4windows: { path: "ds4windows_port", label: "DS4Windows port", sub: "0 uses DS4Windows' own setting (127.0.0.1:6969 when it has none)." },
  dsx: { path: "dsx_port", label: "DSX port", sub: "0 reads DSX's port file (6969 when there is none)." },
};

// The settings this page changes, by the names it shows them under.
const LABELS = { ds4windows_port: "DS4Windows port", dsx_port: "DSX port" };

const HAPTICS = [["auto", "Auto"], ["controller", "Controller"], ["virtual", "Virtual"]];

// drives: EDSense drives the controller now, so a switch is felt.
export function drives(s) {
  return Boolean(s && s.active && s.online);
}

// connectionText says where EDSense sends, who answers there, and who
// answers elsewhere: "Sends to 127.0.0.1:6969. DS4Windows 5.0.12.0 answers."
export function connectionText(s, det) {
  const addr = s && s.addr ? s.addr : "";
  const at = new Map(); // address -> who answers there
  for (const id of ["dsx", "ds4windows"]) {
    const seen = det && det[id];
    if (seen && seen.addr && !at.has(seen.addr)) {
      at.set(seen.addr, seen.answers || "");
    }
  }
  const who = (id) => {
    const d4 = det && det.ds4windows;
    return id === "ds4windows" && d4 && d4.version ? "DS4Windows " + d4.version : NAMES[id] || id;
  };
  const parts = [];
  if (addr) {
    let line = "Sends to " + addr + ".";
    if (at.has(addr)) {
      const a = at.get(addr);
      line += a ? " " + who(a) + " answers." : " Nothing answers there.";
    }
    parts.push(line);
  }
  for (const [where, a] of at) {
    if (where !== addr && a) {
      parts.push(who(a) + " answers on " + where + ".");
    }
  }
  return parts.join(" ");
}

// pendingText is the restart bar's sentence for the settings that wait
// for Apply now; "" for none.
export function pendingText(keys) {
  if (!keys || keys.length === 0) {
    return "";
  }
  const named = keys.every((k) => LABELS[k]);
  const list = (named ? keys.map((k) => LABELS[k]) : keys).join(", ");
  return (named ? "Changed: " : "Changed in edsense.json: ") + list + ". " +
    (keys.length > 1 ? "They are" : "It is") + " used after EDSense reconnects.";
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

// portProblem is a port field's error for problem p; key is the port's
// schema row.
export function portProblem(p, key) {
  if (p && (p.code === "range" || p.code === "type")) {
    const lo = key && key.min ? key.min : 0;
    const hi = key && key.max ? key.max : 65535;
    return "Type a port from " + lo + (lo === 0 ? " (automatic)" : "") + " to " + hi + ".";
  }
  return refusal(p);
}

// chipsFor is what the card of choice id shows beside its name, as
// [text, tone]. wanted: a choice the player made here; switching: it is on
// its way. A switch started elsewhere has no card: its target is not known.
export function chipsFor(id, s, det, wanted, switching) {
  const chips = [];
  if (s && s.choice === id) {
    chips.push(["In use", "accent"]);
  }
  if (det && id !== "auto") {
    if (det.auto && det.auto.sure && det.auto.kind === id) {
      chips.push(["Detected", "ok"]);
    }
    if (det[id] && !det[id].running) {
      chips.push(["Not running", ""]);
    }
  }
  if (switching && wanted === id) {
    chips.push(["Switching...", "warn"]);
  }
  return chips;
}

// fallback: Auto is in use and sure of no app, so EDSense waits on DSX.
export function fallback(s, det) {
  return Boolean(s && s.choice === "auto" && s.kind === "dsx" && det && det.auto && !det.auto.sure);
}

// autoLine is the Auto card's line while Auto is in use: the app it uses,
// or why it is sure of none.
export function autoLine(s, det) {
  if (!s || s.choice !== "auto" || !s.backend) {
    return "";
  }
  if (fallback(s, det) && det.auto.why) {
    return capital(det.auto.why) + ", so EDSense uses " + s.backend + " for now.";
  }
  return "Uses " + s.backend;
}

// setupApps are the apps whose setup the page shows: the one in use, or,
// while Auto falls back to DSX, the ones that run.
export function setupApps(s, det) {
  const kind = s && NAMES[s.kind] ? s.kind : "";
  if (!fallback(s, det)) {
    return kind ? [kind] : [];
  }
  return Object.keys(NAMES).filter((id) => det[id] && det[id].running);
}

// profileApps are the apps whose profile card the page shows: those whose
// setup it shows, the one chosen, and those whose card (in profiles, by
// app) has a write that was asked for, until its end is seen, so its
// Cancel and its end stay in view.
export function profileApps(s, shown, profiles) {
  const apps = (shown || []).slice();
  if (s && NAMES[s.choice] && !apps.includes(s.choice)) {
    apps.push(s.choice);
  }
  for (const id of Object.keys(NAMES)) {
    const st = profiles && Object.prototype.hasOwnProperty.call(profiles, id) ? profiles[id] : null;
    if (st && JOB_STATES.includes(st.state) && !apps.includes(id)) {
      apps.push(id);
    }
  }
  return apps;
}

// first is the problem about path, else the first one.
function first(reply, path) {
  const list = (reply && reply.problems) || [];
  return list.find((p) => p.path === path) || list[0] || null;
}

// setChips puts chips after the name in a choice's head, rebuilt only on
// a change.
export function setChips(head, chips) {
  const key = chips.map((c) => c.join(":")).join("|");
  if (head.dataset.chips === key) {
    return;
  }
  head.dataset.chips = key;
  while (head.children.length > 1) {
    head.lastChild.remove();
  }
  head.append(...chips.map(([text, tone]) => h("span", { class: tone ? "chip " + tone : "chip", text })));
}

export function view(app, title) {
  const ed = edits(app);
  const broken = brokenCard(app);
  let started = false; // the polls run
  let kind = ""; // the app in use: the port's
  let shown = []; // the apps whose setup shows
  let det = null; // backend.detect's last answer
  let detErr = "";
  let wanted = ""; // a choice being confirmed or made
  let switching = false; // wanted is on its way
  let applying = false;
  let checkingDet = false;
  let portErr = null; // a refused port: {path, raw, text, was}

  // Controller app
  const heads = {};
  const radios = {};
  const uses = h("span", { class: "choice-sub", hidden: true });
  const choices = h("div", { class: "choices", role: "radiogroup", "aria-labelledby": "app-title", "aria-describedby": "app-keys" },
    APPS.map((a) => {
      radios[a.id] = h("input", { type: "radio", name: "backend", value: a.id, onchange: () => pick(a.id) });
      heads[a.id] = h("span", { class: "choice-head" }, h("span", { text: a.name }));
      return h("label", { class: "choice" }, radios[a.id], heads[a.id],
        h("span", { class: "choice-sub", text: a.sub }), a.id === "auto" ? uses : null);
    }));
  // The arrow keys move the focus between the apps and choose none: a
  // choice switches at once, so it takes Space, Enter or a click.
  choices.addEventListener("keydown", (e) => {
    if (e.altKey || e.ctrlKey || e.metaKey) {
      return;
    }
    const list = APPS.map((a) => radios[a.id]).filter((r) => !r.disabled);
    const at = list.indexOf(e.target);
    const step = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }[e.key];
    if (at < 0) {
      return;
    }
    if (step) {
      e.preventDefault();
      list[(at + step + list.length) % list.length].focus();
    } else if (e.key === "Enter" && !e.target.checked) {
      e.preventDefault();
      e.target.click();
    }
  });
  const elsewhere = h("p", { class: "note", hidden: true, text: "Switching the controller app..." });
  const pinned = h("p", { class: "note", hidden: true, text: "Set by -backend for this run. A choice here replaces it." });
  const appCard = h("section", { class: "card stack", "aria-labelledby": "app-title" },
    h("h2", { id: "app-title", text: "Controller app" }),
    h("span", { class: "sr-only", id: "app-keys", text: "The arrow keys move between the apps. Space or Enter switches to the one with the focus." }),
    choices,
    elsewhere,
    pinned,
    h("div", { class: "choices", role: "list", "aria-label": "Coming later" },
      LATER.map((name) => h("div", { class: "choice later", role: "listitem" },
        h("span", { class: "choice-head" }, h("span", { text: name }), h("span", { class: "chip", text: "Coming later" }))))));

  // Connection
  const detLine = h("p", {});
  const detNote = h("p", { class: "note", hidden: true });
  const detAgain = h("button", { class: "btn", type: "button", "aria-describedby": "conn-title", onclick: checkDetect });
  const portLabel = h("span", {});
  const portSub = h("span", { class: "row-sub", id: "port-sub" });
  const portInput = ed.track(h("input", {
    class: "text", type: "text", inputmode: "numeric", id: "port-input", maxlength: "8",
    autocomplete: "off", spellcheck: "false", "aria-describedby": "port-sub", onchange: setPort,
  }));
  const portError = h("p", { class: "field-error", id: "port-error" });
  const portRow = h("div", { class: "row wrap", hidden: true },
    h("label", { class: "row-label", for: "port-input" }, portLabel, portSub),
    h("div", { class: "field" }, portInput, portError));
  const barText = h("span", { class: "grow" });
  const applyBtn = h("button", { class: "btn primary", type: "button", onclick: applyNow });
  const bar = h("div", { class: "restart-bar", hidden: true }, barText, applyBtn);
  const connCard = h("section", { class: "card stack", "aria-labelledby": "conn-title" },
    h("div", { class: "card-head" }, h("h2", { id: "conn-title", text: "Connection" }), detAgain),
    detLine, detNote, portRow, bar);

  // EDSense's profile for Elite in each app, shown for the apps
  // profileApps names
  const cards = {
    ds4windows: profileCard(app, "ds4windows", "ds4w-profile"),
    dsx: profileCard(app, "dsx", "dsx-profile"),
  };

  // Setup: a card per app, shown for the apps setupApps names
  const setups = {};
  for (const id of Object.keys(NAMES)) {
    setups[id] = setupCard(id);
  }

  // Haptics with DS4Windows
  const seg = ed.track(h("div", { class: "seg", role: "radiogroup", "aria-labelledby": "haptics-label", "aria-describedby": "haptics-sub" },
    HAPTICS.map(([id, label]) => h("label", {},
      h("input", { type: "radio", name: "ds4windows_haptics", value: id, onchange: () => setHaptics(id) }),
      h("span", { text: label })))));
  const hapticsCard = h("section", { class: "card list", "aria-labelledby": "haptics-label", hidden: true },
    h("div", { class: "rows" },
      h("div", { class: "row wrap" },
        h("span", { class: "row-label" },
          h("span", { id: "haptics-label", text: "Haptics with DS4Windows" }),
          h("span", { class: "row-sub", id: "haptics-sub", text: "Where native haptics play. Auto: the controller's own audio device when it is wired, else the virtual DualSense's." })),
        seg)));

  const el = h("div", { class: "page" },
    h("div", {},
      h("h1", { text: title }),
      h("p", { class: "lead", text: "Which app EDSense drives the controller through, and how it is set up." })),
    broken.el, appCard, connCard, Object.values(setups).map((s) => s.card), cards.ds4windows.el, cards.dsx.el, hapticsCard);

  // the polls: who runs and answers, and the setup of the apps shown
  const detPoll = poll(async (fresh) => {
    try {
      det = await app.call("backend.detect", { fresh });
      detErr = "";
    } catch (err) {
      detErr = app.errorText(err);
    }
    update();
  });

  // setupCard is the Setup card of app id, with its own poll of
  // setup.check, which asks only while the card shows.
  function setupCard(id) {
    const s = { id, data: null, err: "", checking: false };
    const titleId = id + "-setup-title";
    s.again = h("button", { class: "btn", type: "button", "aria-describedby": titleId, onclick: () => checkSetup(s) });
    // an item EDSense's profile fixes takes the player to the profile card
    s.rows = checkRows(app, setupRows, () => cards[id].focus());
    s.note = h("p", { class: "note", hidden: true });
    s.card = h("section", { class: "card stack", "aria-labelledby": titleId, hidden: true },
      h("div", { class: "card-head" }, h("h2", { id: titleId, text: NAMES[id] + " setup" }), s.again),
      s.rows.el, s.note);
    s.poll = setupWatch(app, () => (shown.includes(id) ? id : ""), (got, err) => {
      if (got) {
        s.data = got;
        s.err = "";
      } else {
        s.err = app.errorText(err);
      }
      update();
    });
    return s;
  }

  // config: the settings, else nothing
  const config = () => (app.settings ? app.settings.config : null);

  // takeEngine shows what an apply or a choice left running, until the
  // next status says so.
  function takeEngine(eng) {
    const s = app.status;
    if (!s || !eng) {
      return;
    }
    Object.assign(s, { choice: eng.choice, pinned: eng.pinned, kind: eng.kind, why: eng.why, switching: eng.switching });
    if (Array.isArray(eng.pending)) {
      s.pending = eng.pending;
    }
    if (eng.addr) {
      s.addr = eng.addr;
    }
    if (eng.name) {
      s.backend = eng.name;
    }
  }

  // pick switches to choice id at once, as the tray does; asked first
  // while EDSense drives the controller.
  async function pick(id) {
    if (wanted || locked(app)) {
      update(); // one at a time: the radios show the one on its way
      return;
    }
    wanted = id;
    update();
    if (drives(app.status)) {
      const yes = await app.confirm({
        title: "Switch the controller app?",
        text: "EDSense hands the controller back to your profile for about a second while it switches.",
        ok: "Switch",
      });
      if (!yes) {
        wanted = "";
        update();
        return;
      }
    }
    switching = true;
    update();
    try {
      const eng = await app.choose(id);
      takeEngine(eng);
      if (eng && eng.name && !eng.not_saved) {
        app.announce("Now using " + eng.name, false);
      }
    } catch (err) {
      app.toast(app.errorText(err), true);
    } finally {
      wanted = "";
      switching = false;
      update();
    }
  }

  async function applyNow() {
    if (applying) {
      return;
    }
    if (drives(app.status)) {
      const yes = await app.confirm({
        title: "Apply now?",
        text: "EDSense hands the controller back to your profile for about a second while it reconnects.",
        ok: "Apply now",
      });
      if (!yes) {
        return;
      }
    }
    applying = true;
    update();
    try {
      takeEngine(await app.call("engine.apply"));
    } catch (err) {
      app.toast(app.errorText(err), true);
    } finally {
      applying = false;
      update();
    }
  }

  async function checkDetect() {
    if (checkingDet) {
      return;
    }
    checkingDet = true;
    update();
    await detPoll.now(true);
    checkingDet = false;
    update();
    if (detErr) {
      app.announce(detErr, true);
    }
  }

  async function checkSetup(s) {
    if (s.checking) {
      return;
    }
    s.checking = true;
    update();
    await s.poll.now(true);
    s.checking = false;
    update();
    if (s.err) {
      app.announce(s.err, true);
    }
  }

  // setPort sends the port typed in; a refused one stays in the field
  // with what is wrong with it.
  async function setPort() {
    const port = PORTS[kind];
    if (!port || locked(app)) {
      return;
    }
    const raw = portInput.value.trim();
    const value = /^\d+$/.test(raw) ? Number(raw) : raw;
    const was = valueAt(config(), port.path);
    const fail = (text) => {
      portErr = { path: port.path, raw, text, was };
      app.announce(text, true);
    };
    if (value === was) {
      portErr = null;
      update();
      return;
    }
    try {
      const reply = await ed.set(port.path, value);
      if (reply && reply.applied) {
        portErr = null;
      } else {
        fail(portProblem(first(reply, port.path), keyOf(app.schema, port.path)));
      }
    } catch (err) {
      fail(app.errorText(err));
    }
    update();
  }

  async function setHaptics(id) {
    const path = "ds4windows_haptics";
    if (locked(app)) {
      showHaptics(true);
      return;
    }
    try {
      const reply = await ed.set(path, id);
      if (!reply || !reply.applied) {
        app.toast(refusal(first(reply, path)), true);
        showHaptics(true);
      }
    } catch (err) {
      app.toast(app.errorText(err), true);
      showHaptics(true);
    }
  }

  // showHaptics checks the radio of the setting; force: also while the
  // player is at it, after a change that did not go through.
  function showHaptics(force) {
    const path = "ds4windows_haptics";
    if (!force && !ed.shows(seg, path)) {
      return;
    }
    const v = valueAt(config(), path);
    for (const input of seg.querySelectorAll("input")) {
      input.checked = input.value === v;
    }
  }

  function updateApps(s, lock) {
    const checked = wanted || (s ? s.choice : "");
    for (const a of APPS) {
      radios[a.id].checked = a.id === checked;
      radios[a.id].disabled = lock;
      setChips(heads[a.id], chipsFor(a.id, s, det, wanted, switching));
    }
    const line = autoLine(s, det);
    uses.hidden = !line;
    setText(uses, line);
    elsewhere.hidden = !(s && s.switching && !switching);
    pinned.hidden = !(s && s.pinned);
  }

  function updateConnection(s, lock) {
    const line = connectionText(s, det);
    setText(detLine, line || "Looking for the controller apps...");
    detNote.hidden = !detErr;
    setText(detNote, detErr);
    setButton(detAgain, "refresh", checkingDet ? "Checking..." : "Check again", checkingDet);

    const port = PORTS[kind];
    portRow.hidden = !port;
    if (port) {
      const v = valueAt(config(), port.path);
      if (portErr && (portErr.path !== port.path || v !== portErr.was)) {
        portErr = null; // another app, or the setting changed elsewhere
      }
      setText(portLabel, port.label);
      setText(portSub, port.sub);
      const focused = portInput === document.activeElement;
      if (portErr) {
        if (!focused && portInput.value !== portErr.raw) {
          portInput.value = portErr.raw;
        }
      } else if (ed.shows(portInput, port.path)) {
        const text = v === undefined || v === null ? "" : String(v);
        if (portInput.value !== text) {
          portInput.value = text;
        }
      }
      portInput.disabled = lock;
      setText(portError, portErr ? portErr.text : "");
      if (portErr) {
        portInput.setAttribute("aria-invalid", "true");
        portInput.setAttribute("aria-describedby", "port-sub port-error");
      } else {
        portInput.removeAttribute("aria-invalid");
        portInput.setAttribute("aria-describedby", "port-sub");
      }
    }

    const text = pendingText(s && s.pending);
    if (!text && !bar.hidden && bar.contains(document.activeElement)) {
      detAgain.focus(); // applied: the bar goes from under the focus
    }
    bar.hidden = !text;
    setText(barText, text);
    setButton(applyBtn, "", applying ? "Applying..." : "Apply now", applying);
  }

  function updateSetups() {
    for (const s of Object.values(setups)) {
      const on = shown.includes(s.id);
      if (!on && !s.card.hidden && s.card.contains(document.activeElement)) {
        detAgain.focus(); // the card goes from under the focus
      }
      s.card.hidden = !on;
      if (on) {
        s.rows.show(s.data ? s.data.items : null);
        s.note.hidden = !s.err;
        setText(s.note, s.err);
        setButton(s.again, "refresh", s.checking ? "Checking..." : "Check again", s.checking);
      }
    }
  }

  function updateProfiles(s, lock) {
    // while Auto falls back to DSX, only the apps that run have a card
    const apps = profileApps(s, shown, app.profiles);
    for (const [id, c] of Object.entries(cards)) {
      const on = apps.includes(id);
      if (!on && !c.el.hidden && c.el.contains(document.activeElement)) {
        detAgain.focus(); // the card goes from under the focus
      }
      c.show(on);
      if (on) {
        c.update(id === "ds4windows" ? cardItems(setups.ds4windows.data) : []);
      }
    }

    hapticsCard.hidden = kind !== "ds4windows";
    for (const input of seg.querySelectorAll("input")) {
      input.disabled = lock;
    }
    showHaptics(false);
  }

  // showSetups shows the setup of the apps setupApps names now. A card
  // that comes or goes starts over, as all do when the app in use
  // changed (again), and a card that starts over asks at once.
  function showSetups(s, again) {
    const next = setupApps(s, det);
    for (const x of Object.values(setups)) {
      const was = shown.includes(x.id);
      const is = next.includes(x.id);
      if (again || was !== is) {
        x.data = null;
        x.err = "";
        if (started && is) {
          x.poll.now(false);
        }
      }
    }
    shown = next;
  }

  function update() {
    const s = app.status;
    const lock = locked(app);
    const focused = document.activeElement;
    broken.update();
    const k = s && NAMES[s.kind] ? s.kind : "";
    const again = k !== kind; // the checklists were about the app before
    if (again) {
      kind = k;
    }
    showSetups(s, again);
    if (!started) {
      started = true;
      detPoll.start();
      for (const x of Object.values(setups)) {
        x.poll.start();
      }
    }
    updateApps(s, lock);
    updateConnection(s, lock);
    updateSetups();
    updateProfiles(s, lock);
    // the file broke while a setting control it disables had the focus
    if (lock && focused instanceof HTMLElement && el.contains(focused) && focused.disabled) {
      broken.focus();
    }
  }

  function leave() {
    detPoll.stop();
    for (const x of Object.values(setups)) {
      x.poll.stop();
    }
  }

  return { el, update, leave };
}
