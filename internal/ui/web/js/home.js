// Home: what EDSense does now, the quick actions, and what happened.
import { h, icon, setText } from "./dom.js";
import { capital, clock, count, drift, pct } from "./format.js";
import { brokenCard } from "./cards.js";

// setButton gives a button an icon and a label, rebuilt only on a change.
function setButton(btn, iconName, label) {
  const key = iconName + "|" + label;
  if (btn.dataset.key === key) {
    return;
  }
  btn.dataset.key = key;
  btn.replaceChildren(icon(iconName), h("span", { text: label }));
}

function tile(label) {
  const dot = h("span", { class: "dot", "aria-hidden": "true" });
  const value = h("div", { class: "tile-value", text: "..." });
  const sub = h("div", { class: "tile-sub" });
  const el = h("div", { class: "tile", role: "group", "aria-label": label },
    h("div", { class: "tile-label" }, dot, label), value, sub);
  return {
    el,
    set(tone, v, s) {
      if (tone) {
        dot.dataset.tone = tone;
      } else {
        delete dot.dataset.tone;
      }
      setText(value, v);
      setText(sub, s || "");
    },
  };
}

const HAPTICS = { native: "native haptics", rumble: "rumble", off: "haptics off", waiting: "haptics waiting" };

function gyroWords(s) {
  if (!s.gyro.aim) {
    return "gyro aim off";
  }
  return "gyro aim by " + (s.gyro.by === "edsense" ? "EDSense" : s.backend);
}

function subLine(s) {
  if (!s.full) {
    return s.backend;
  }
  return [s.backend, s.controllers > 0 ? count(s.controllers, "controller", "controllers") : "no controller",
    HAPTICS[s.haptics], gyroWords(s)].filter(Boolean).join(" - ");
}

export function view(app) {
  const dot = h("span", { class: "dot large", "aria-hidden": "true" });
  const title = h("h1", { "aria-live": "polite", text: "Starting" });
  const sub = h("div", { class: "muted" });

  const pause = h("button", { class: "btn primary", type: "button", onclick: () => app.status && app.setPaused(!app.status.paused) });
  const demo = h("button", {
    class: "btn", type: "button",
    onclick: () => app.call(app.status && app.status.demo ? "demo.stop" : "demo.play").catch(() => {}),
  });
  // aria-disabled in place of disabled: the button keeps the focus while
  // it calibrates, and its hint says why it is off
  const calibrate = h("button", {
    class: "btn", type: "button", "aria-describedby": "cal-hint",
    onclick: () => calibrate.getAttribute("aria-disabled") !== "true" && setCal("confirm"),
  });
  const hint = h("p", { class: "hint", id: "cal-hint", hidden: true });

  // calibrating: asked first, in place of the message box the tray shows
  let cal = "idle"; // idle, confirm or busy
  let calTimer = 0;
  let sawCalibrating = false;
  const startCal = h("button", {
    class: "btn primary", type: "button", text: "Start",
    onclick: () => {
      sawCalibrating = false;
      setCal("busy");
      calTimer = setTimeout(() => setCal("idle"), 8000);
      app.call("gyro.calibrate").catch(() => setCal("idle"));
    },
  });
  const cancelCal = h("button", { class: "btn quiet", type: "button", text: "Cancel", onclick: () => setCal("idle", true) });
  const confirm = h("div", { class: "confirm", role: "group", "aria-label": "Calibrate gyro", hidden: true },
    h("div", { text: "Put the controller down on a flat surface and let go, then press Start. Keep it still for 2 seconds." }),
    h("div", { class: "actions" }, startCal, cancelCal));
  confirm.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      setCal("idle", true);
    }
  });
  function setCal(next, refocus) {
    if (next !== "busy") {
      clearTimeout(calTimer);
    }
    cal = next;
    update();
    if (next === "confirm") {
      startCal.focus();
    } else if (next === "busy" || refocus) {
      calibrate.focus(); // it now says "Calibrating... keep still"
    }
  }

  const tiles = {
    connection: tile("Connection"),
    controller: tile("Controller"),
    haptics: tile("Haptics"),
    gyro: tile("Gyro aim"),
    elite: tile("Elite Dangerous"),
    hud: tile("HUD reader"),
  };

  const rows = h("div", { class: "rows" });
  let shownNotices = "";
  const broken = brokenCard(app);

  // the error card comes after the status, whose title takes the focus,
  // so reading on from there reaches it
  const el = h("div", { class: "page" },
    h("section", { class: "card hero", "aria-label": "Status" },
      h("div", { class: "hero-head" }, dot, title),
      sub,
      h("div", { class: "actions" }, pause, demo, calibrate),
      hint,
      confirm),
    broken.el,
    h("div", { class: "tiles" }, Object.values(tiles).map((t) => t.el)),
    h("section", { class: "card list", "aria-label": "Activity" },
      h("h2", { text: "Activity" }),
      rows));

  function updateTiles(s) {
    if (!s.full) {
      return;
    }
    tiles.connection.set(s.online ? "ok" : "err", s.backend + (s.online ? " answering" : " not answering"),
      s.addr ? "on " + s.addr : "");

    tiles.controller.set(s.controllers > 0 ? "ok" : "", s.controllers > 0 ? count(s.controllers, "controller", "controllers") : "No controller",
      s.online ? "Listed by " + s.backend : s.backend + " is not answering");

    const haptics = {
      native: ["ok", "Native", "Through the virtual DualSense's audio"],
      rumble: ["ok", "Rumble", "The controller's motors"],
      off: ["", "Off", "\"control_haptics\" is off in edsense.json"],
      waiting: ["", "Waiting", s.elite ? "Looking for the virtual DualSense's audio" : "Starts while Elite runs"],
    }[s.haptics] || ["", "Waiting", ""];
    tiles.haptics.set(...haptics);

    const g = s.gyro;
    const calibrated = g.calibrated ? "Drift " + drift(g.drift) : "Not calibrated yet";
    if (!g.aim) {
      tiles.gyro.set("", "Off", "\"gyro_aim\" is off");
    } else if (g.by !== "edsense") {
      tiles.gyro.set("ok", s.backend + " aims", "EDSense gyro is off in the tray menu");
    } else if (!g.has) {
      tiles.gyro.set("", "No gyro", "This connection passes no motion");
    } else if (g.aiming) {
      tiles.gyro.set("ok", "EDSense aims", calibrated);
    } else {
      tiles.gyro.set("", "Waiting", s.elite ? "Aims while Elite is in front and you fly" : "Aims while Elite runs");
    }

    if (!s.elite) {
      tiles.elite.set("", "Not running", "EDSense switches on when you play");
    } else if (s.active) {
      tiles.elite.set("accent", capital(s.context), s.fire_group > 0 ? "Fire group " + s.fire_group : "");
    } else {
      tiles.elite.set("", capital(s.context), s.paused ? "Effects paused" : "Effects start in the ship or on foot");
    }

    const hud = s.hud;
    if (!hud.can) {
      tiles.hud.set("", "Not available", "The screen cannot be read here");
    } else if (!hud.on) {
      tiles.hud.set("", "Off", "\"hud_reader\" is off in edsense.json");
    } else if (hud.shield >= 0 || hud.heat >= 0) {
      const parts = [];
      if (hud.shield >= 0) {
        parts.push("Shield " + pct(hud.shield));
      }
      if (hud.heat >= 0) {
        parts.push((parts.length ? "heat " : "Heat ") + pct(hud.heat));
      }
      tiles.hud.set("ok", parts.join(", "), "Read from the cockpit HUD");
    } else {
      tiles.hud.set("", "Waiting", "Reads in the cockpit, with Elite in front");
    }
  }

  function updateActivity() {
    const list = app.notices;
    const key = list.length + ":" + (list.length ? list[list.length - 1].id : 0);
    if (key === shownNotices) {
      return;
    }
    shownNotices = key;
    if (!list.length) {
      rows.replaceChildren(h("div", { class: "empty", text: "Nothing yet. What EDSense does shows up here." }));
      return;
    }
    rows.replaceChildren(...list.slice().reverse().map((n) =>
      h("div", { class: "row short" },
        h("span", { class: "time", text: clock(n.t) }),
        h("span", { class: "notice-text" + (n.level === "message" ? " message" : ""), text: n.text }))));
  }

  function update() {
    const s = app.status;
    broken.update();
    updateActivity();
    if (!s) {
      return;
    }
    dot.dataset.level = s.level;
    setText(title, s.text);
    setText(sub, subLine(s));
    setButton(pause, s.paused ? "play" : "pause", s.paused ? "Resume effects" : "Pause effects");
    let demoLabel = "Play demo";
    if (s.demo) {
      demoLabel = s.demo_steps ? "Stop demo (step " + s.demo_step + " of " + s.demo_steps + ")" : "Stop demo";
    }
    setButton(demo, s.demo ? "stop" : "play", demoLabel);

    let why = "";
    if (s.full && !s.gyro.has) {
      why = "This controller connection passes no gyro.";
    } else if (!s.elite) {
      why = "Start Elite first to calibrate the gyro.";
    }
    if (cal === "busy") {
      if (s.gyro.calibrating) {
        sawCalibrating = true;
      } else if (sawCalibrating) {
        clearTimeout(calTimer);
        cal = "idle";
      }
    }
    if (why && cal === "confirm") {
      cal = "idle";
    }
    setButton(calibrate, "target", cal === "busy" ? "Calibrating... keep still" : "Calibrate gyro");
    if (why || cal === "busy" || s.demo) {
      calibrate.setAttribute("aria-disabled", "true");
    } else {
      calibrate.removeAttribute("aria-disabled");
    }
    const focusIn = confirm.contains(document.activeElement);
    calibrate.hidden = cal === "confirm";
    confirm.hidden = cal !== "confirm";
    if (focusIn && confirm.hidden) {
      calibrate.focus(); // the confirm closed under the focus
    }
    hint.hidden = !why;
    setText(hint, why);
    updateTiles(s);
  }

  return { el, update };
}
