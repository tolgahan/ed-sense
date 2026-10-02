// The first run: shown once, for a fresh install, in place of the pages.
// Five steps: what EDSense does, the controller app, how that app is set
// up, gyro aim, and the end. Nothing is written to edsense.json before
// Finish or Set up later.
import { h, setButton, setText } from "./dom.js";
import { brokenCard } from "./cards.js";
import { cardItems, checkRows, poll, setupRows, setupWatch } from "./checklist.js";
import { NAMES, refusal, setChips } from "./controller.js";
import { profileCard } from "./profilecard.js";

// The steps, as the dots name them.
export const STEPS = ["Welcome", "Your controller app", "Set it up", "Gyro aim", "Ready"];

// The apps to pick from, with their sub lines.
const APPS = [
  { id: "ds4windows", sub: "DS4Windows 5, free, with its game mod support" },
  { id: "dsx", sub: "DSX, with its Incoming UDP" },
];

const LATER = ["Native USB", "Xbox controllers"];

// Who turns the motion into mouse movement with DSX: gyro_by.
const AIMERS = [["edsense", "EDSense"], ["dsx", "DSX"]];
const AIMERS_SUB = "EDSense: EDSense turns the controller's motion into mouse movement. DSX: DSX does, as before.";

// What Set up later leaves; pinned: while -backend decides this run.
export const LATER_TEXT = "EDSense uses the controller app that runs. The Controller page changes it.";
export const LATER_PINNED = "EDSense keeps the app -backend set for this run, and from the next start uses the one that runs. The Controller page changes it.";

// detected is the app backend.detect is sure of; "" when it is sure of none.
export function detected(det) {
  const auto = det && det.auto;
  return auto && auto.sure && NAMES[auto.kind] ? auto.kind : "";
}

// neitherRuns: backend.detect found neither app running.
export function neitherRuns(det) {
  return Boolean(det && det.dsx && det.ds4windows && !det.dsx.running && !det.ds4windows.running);
}

// appChips is what the card of app id shows beside its name, as
// [text, tone].
export function appChips(id, det) {
  if (!det || !det[id]) {
    return [];
  }
  const chips = [];
  if (detected(det) === id) {
    chips.push(["Detected", "ok"]);
  }
  chips.push([det[id].running ? "Running" : "Not running", ""]);
  return chips;
}

// anyBad: a check of the setup shows a problem.
export function anyBad(setup) {
  return Boolean(setup && (setup.items || []).some((it) => it.state === "bad"));
}

// finishPatch is what Finish writes besides the controller app: gyro_by
// only with DSX, where EDSense or DSX may aim.
export function finishPatch(pick, aim, by) {
  const patch = { gyro_aim: Boolean(aim) };
  if (pick === "dsx") {
    patch.gyro_by = by === "dsx" ? "dsx" : "edsense";
  }
  return patch;
}

// gyroText says what gyro aim does after Finish.
export function gyroText(pick, aim, by) {
  if (!aim) {
    return "Off";
  }
  if (pick !== "dsx") {
    return "On";
  }
  return by === "dsx" ? "On, DSX aims" : "On, EDSense aims";
}

// demoLabel is the demo button's label: "Stop demo (step 3 of 9)".
export function demoLabel(s) {
  if (!s || !s.demo) {
    return "Play demo";
  }
  return s.demo_steps ? "Stop demo (step " + s.demo_step + " of " + s.demo_steps + ")" : "Stop demo";
}

export function view(app) {
  const cfg = (app.settings && app.settings.config) || {};
  let aim = cfg.gyro_aim !== false; // the player's choices, written by Finish
  let by = cfg.gyro_by === "dsx" ? "dsx" : "edsense";
  let pick = ""; // the app picked: dsx or ds4windows
  let step = -1; // the step shown; -1 until the first update
  let cur = null; // the step's parts: {title, nodes, buttons, update()}
  let det = null; // backend.detect's last answer
  let setup = null; // setup.check's last answer for pick
  let setupErr = "";
  let checking = false; // Check again runs
  let busy = ""; // "later" or "finish" while it writes
  let hintText = ""; // why Continue waits

  const dots = STEPS.map((name, i) => h("li", {},
    h("span", { class: "step-dot", "aria-hidden": "true" }),
    h("span", { class: "sr-only", text: "Step " + (i + 1) + " of " + STEPS.length + ": " + name })));
  const broken = brokenCard(app);
  const body = h("div", { class: "onboard-body" });
  const hint = h("p", { class: "note", id: "fr-hint", hidden: true });
  const error = h("p", { class: "field-error", id: "fr-error" });
  const msg = h("div", { class: "onboard-msg", hidden: true }, hint, error);
  const buttons = h("div", { class: "actions" });
  const el = h("div", { class: "page onboard" },
    h("ol", { class: "steps", "aria-label": "Setup progress" }, dots),
    body,
    h("div", { class: "onboard-foot" }, msg, buttons));

  // finishing: the first run writes the settings itself, so the config
  // event that follows is no news to it
  const self = { el, finishing: false, update, leave };

  // who runs, for the app cards: from the first step, so the second opens
  // with the detected app picked
  const detPoll = poll(async (fresh) => {
    try {
      det = await app.call("backend.detect", { fresh });
    } catch (err) {
      console.error(err); // the cards show no chips
    }
    if (!pick && detected(det)) {
      pick = detected(det); // preselected; the player may pick the other
    }
    update();
  });
  const setupPoll = setupWatch(app, () => pick, (s, err) => {
    if (s) {
      setup = s;
      setupErr = "";
    } else {
      setupErr = app.errorText(err);
    }
    update();
  });

  // button makes a footer button; f runs unless it is aria-disabled.
  function button(label, kind, f, iconName) {
    const b = h("button", { class: kind ? "btn " + kind : "btn", type: "button" });
    b.addEventListener("click", () => {
      if (b.getAttribute("aria-disabled") !== "true") {
        f();
      }
    });
    setButton(b, iconName || "", label, false);
    return b;
  }

  function backButton() {
    return button("Back", "quiet", () => showStep(step - 1, true), "back");
  }

  function head(title, lead) {
    return h("div", {}, title, h("p", { class: "lead", text: lead }));
  }

  function welcome() {
    const title = h("h1", { class: "welcome", id: "fr-title", text: "Welcome to EDSense" });
    const later = button("Set up later", "", setUpLater);
    const next = button("Get started", "primary", () => showStep(1, true));
    return {
      title,
      nodes: [
        head(title, "EDSense gives Elite Dangerous a DualSense feel: adaptive triggers, lights and haptics that follow the game."),
        h("p", { text: "A few steps set it up. Your choices are saved when you finish." }),
      ],
      buttons: [later, next],
      update() {
        setButton(later, "", "Set up later", busy !== "");
        setButton(next, "", "Get started", busy !== "");
      },
    };
  }

  function apps() {
    const title = h("h1", { id: "fr-title", text: "Your controller app" });
    const radios = {};
    const heads = {};
    const group = h("div", { class: "choices stack", role: "radiogroup", "aria-labelledby": "fr-title" },
      APPS.map((a) => {
        radios[a.id] = h("input", { type: "radio", name: "first-app", value: a.id, onchange: () => pickApp(a.id) });
        heads[a.id] = h("span", { class: "choice-head" }, h("span", { text: NAMES[a.id] }));
        return h("label", { class: "choice" }, radios[a.id], heads[a.id], h("span", { class: "choice-sub", text: a.sub }));
      }));
    const none = h("p", { class: "note", hidden: true, text: "Neither app runs right now. Pick the one you use: EDSense connects when it starts." });
    const back = backButton();
    const next = button("Continue", "primary", () => showStep(2, true));
    next.setAttribute("aria-describedby", "fr-hint");
    return {
      title,
      nodes: [
        head(title, "EDSense drives the controller through one of these apps. Pick the one you use."),
        group,
        none,
        h("div", { class: "choices", role: "list", "aria-label": "Coming later" },
          LATER.map((name) => h("div", { class: "choice later", role: "listitem" },
            h("span", { class: "choice-head" }, h("span", { text: name }), h("span", { class: "chip", text: "Coming later" }))))),
      ],
      buttons: [back, next],
      update() {
        for (const a of APPS) {
          radios[a.id].checked = a.id === pick;
          setChips(heads[a.id], appChips(a.id, det));
        }
        none.hidden = !neitherRuns(det);
        hintText = pick ? "" : "Pick an app to continue.";
        setButton(back, "back", "Back", busy !== "");
        setButton(next, "", "Continue", !pick || busy !== "");
      },
    };
  }

  function setUp() {
    const name = NAMES[pick];
    const title = h("h1", { id: "fr-title", text: "Set up " + name });
    const again = h("button", { class: "btn", type: "button", "aria-describedby": "fr-setup-title", onclick: checkAgain });
    // EDSense's profile for Elite in the app: the first run installs it
    // only when asked, and DSX's is not added by itself before Finish
    const card = profileCard(app, pick, "fr-profile");
    // an item EDSense's profile fixes takes the player to its card
    const rows = checkRows(app, setupRows, () => card.focus());
    const note = h("p", { class: "note", hidden: true });
    const setupCard = h("section", { class: "card stack", "aria-labelledby": "fr-setup-title" },
      h("div", { class: "card-head" }, h("h2", { id: "fr-setup-title", text: name + " setup" }), again),
      rows.el, note);
    card.show(true);
    const back = backButton();
    const next = button("Continue", "primary", () => showStep(3, true));
    return {
      title,
      nodes: [
        head(title, "What EDSense finds in " + name + " now. Fix the problems listed, or go on: the Controller page has the same list. Some checks wait until EDSense uses " + name + "."),
        setupCard,
        card.el,
      ],
      buttons: [back, next],
      update() {
        rows.show(setup ? setup.items : null);
        card.update(pick === "ds4windows" ? cardItems(setup) : []);
        note.hidden = !setupErr;
        setText(note, setupErr);
        setButton(again, "refresh", checking ? "Checking..." : "Check again", checking);
        setButton(back, "back", "Back", busy !== "");
        setButton(next, "", anyBad(setup) ? "Continue anyway" : "Continue", busy !== "");
      },
    };
  }

  function gyro() {
    const title = h("h1", { id: "fr-title", text: "Gyro aim" });
    const sw = h("input", {
      type: "checkbox", class: "switch", role: "switch", id: "fr-aim", "aria-describedby": "fr-aim-sub",
      onchange: () => {
        aim = sw.checked;
        update();
      },
    });
    const rows = [h("div", { class: "row" },
      h("label", { class: "row-label", for: "fr-aim" },
        h("span", { text: "Gyro aim" }),
        h("span", { class: "row-sub", id: "fr-aim-sub", text: "Move the controller to aim, as with a mouse." })),
      sw)];
    let seg = null;
    if (pick === "dsx") {
      seg = h("div", { class: "seg", role: "radiogroup", "aria-labelledby": "fr-by-label", "aria-describedby": "fr-by-sub" },
        AIMERS.map(([id, label]) => h("label", {},
          h("input", {
            type: "radio", name: "gyro_by", value: id,
            onchange: () => {
              by = id;
              update();
            },
          }),
          h("span", { text: label }))));
      rows.push(h("div", { class: "row wrap" },
        h("span", { class: "row-label" },
          h("span", { id: "fr-by-label", text: "Who aims" }),
          h("span", { class: "row-sub", id: "fr-by-sub", text: AIMERS_SUB })),
        seg));
    }
    const nodes = [
      head(title, "EDSense can aim in Elite with the controller's gyro. The Gyro aim page changes this later."),
      h("section", { class: "card list", "aria-labelledby": "fr-title" }, h("div", { class: "rows" }, rows)),
    ];
    let gyroRows = null;
    let gyroCard = null;
    if (pick === "ds4windows") {
      gyroRows = checkRows(app, (it) => it.id === "gyro" || it.id === "motion");
      gyroCard = h("section", { class: "card stack", "aria-labelledby": "fr-gyro-title" },
        h("h2", { id: "fr-gyro-title", text: "With DS4Windows" }),
        h("p", { class: "muted", text: "EDSense aims while the DS4Windows profile leaves the gyro alone (Gyro > Output Mode Passthru), " +
          "and reads the motion from DS4Windows' UDP server (Settings > UDP Server > Enable Server)." }),
        gyroRows.el);
      nodes.push(gyroCard);
    }
    const back = backButton();
    const next = button("Continue", "primary", () => showStep(4, true));
    return {
      title,
      nodes,
      buttons: [back, next],
      update() {
        sw.checked = aim;
        if (seg) {
          for (const input of seg.querySelectorAll("input")) {
            input.checked = input.value === by;
            input.disabled = !aim;
          }
        }
        if (gyroCard) {
          gyroCard.hidden = !aim;
          gyroRows.show(setup ? setup.items : null);
        }
        setButton(back, "back", "Back", busy !== "");
        setButton(next, "", "Continue", busy !== "");
      },
    };
  }

  function ready() {
    const title = h("h1", { id: "fr-title", text: "Ready" });
    const row = (label, value) => h("div", { class: "row" },
      h("span", { class: "row-label", text: label }), h("span", { class: "row-value", text: value }));
    const back = backButton();
    const demo = button("Play demo", "", playDemo, "play");
    const fin = button("Finish", "primary", finish);
    return {
      title,
      nodes: [
        head(title, "Finish saves these choices."),
        h("section", { class: "card list", "aria-label": "Your choices" },
          h("div", { class: "rows" },
            row("Controller app", NAMES[pick]),
            row("Gyro aim", gyroText(pick, aim, by)))),
        h("p", { text: "Run Elite in borderless or windowed mode, so EDSense can read the HUD." }),
        h("p", { text: "Closing this window keeps EDSense running next to the clock." }),
      ],
      buttons: [back, demo, fin],
      update() {
        const s = app.status;
        setButton(back, "back", "Back", busy !== "");
        setButton(demo, s && s.demo ? "stop" : "play", demoLabel(s), busy !== "");
        setButton(fin, "", busy === "finish" ? "Finishing..." : "Finish", busy !== "");
      },
    };
  }

  const BUILD = [welcome, apps, setUp, gyro, ready];

  // showStep shows step i; focus: by the player's hand, so the focus goes
  // to the step's title.
  function showStep(i, focus) {
    step = Math.max(0, Math.min(i, BUILD.length - 1));
    setError("");
    cur = BUILD[step]();
    // the error card comes after the step's title, which takes the focus
    body.replaceChildren(cur.nodes[0], broken.el, ...cur.nodes.slice(1));
    buttons.replaceChildren(...cur.buttons);
    dots.forEach((li, n) => {
      li.classList.toggle("done", n < step);
      if (n === step) {
        li.setAttribute("aria-current", "step");
      } else {
        li.removeAttribute("aria-current");
      }
    });
    if (step <= 1) {
      detPoll.start();
    } else {
      detPoll.stop();
    }
    if (step === 2 || step === 3 && pick === "ds4windows") {
      setupPoll.start();
    } else {
      setupPoll.stop();
    }
    update();
    if (focus) {
      cur.title.tabIndex = -1;
      cur.title.focus({ preventScroll: true });
      if (el.parentElement) {
        el.parentElement.scrollTop = 0;
      }
    }
  }

  function pickApp(id) {
    if (id === pick) {
      return;
    }
    pick = id;
    setup = null; // the checks were about the other app
    setupErr = "";
    update();
  }

  // setError shows why a write failed, and has a screen reader say it.
  function setError(text) {
    setText(error, text);
    if (text) {
      app.announce(text, true);
    }
  }

  async function checkAgain() {
    if (checking) {
      return;
    }
    checking = true;
    update();
    await setupPoll.now(true);
    checking = false;
    update();
    if (setupErr) {
      app.announce(setupErr, true);
    }
  }

  function playDemo() {
    const s = app.status;
    app.call(s && s.demo ? "demo.stop" : "demo.play").catch((err) => app.toast(app.errorText(err), true));
  }

  // failed ends a write that did not go through: the first run stays.
  function failed(text) {
    self.finishing = false;
    busy = "";
    setError(text);
    update();
  }

  async function setUpLater() {
    if (self.finishing) {
      return;
    }
    self.finishing = true;
    busy = "later";
    setError("");
    update();
    let eng = null;
    try {
      eng = await app.choose("auto", true); // a -backend choice stays for this run
    } catch (err) {
      failed(app.errorText(err));
      return;
    }
    app.endFirstRun();
    app.toast(eng && eng.pinned ? LATER_PINNED : LATER_TEXT);
  }

  // finish writes the gyro choices, then switches to the app picked,
  // which saves it: the first run is over.
  async function finish() {
    if (self.finishing || !pick) {
      return;
    }
    self.finishing = true;
    busy = "finish";
    setError("");
    update();
    try {
      const reply = await app.patch(finishPatch(pick, aim, by));
      if (!reply || !reply.applied) {
        failed(refusal(reply && reply.problems ? reply.problems[0] : null));
        return;
      }
      await app.choose(pick);
    } catch (err) {
      failed(app.errorText(err));
      return;
    }
    app.endFirstRun();
  }

  function update() {
    if (step < 0) {
      showStep(0, false);
      return;
    }
    broken.update();
    hintText = "";
    cur.update();
    setText(hint, hintText);
    hint.hidden = !hintText;
    msg.hidden = !hintText && !error.textContent;
  }

  function leave() {
    detPoll.stop();
    setupPoll.stop();
  }

  return self;
}
