// The EDSense window: the sidebar, the pages and the live status.
import { call, on } from "./bridge.js";
import { h, icon, setText } from "./dom.js";
import { capital } from "./format.js";
import { brokenText } from "./cards.js";
import { newer } from "./profilecard.js";
import * as dialog from "./dialog.js";
import { flushAll } from "./form.js";
import * as home from "./home.js";
import * as controller from "./controller.js";
import * as feel from "./feel.js";
import * as triggers from "./triggers.js";
import * as lights from "./lights.js";
import * as gyro from "./gyro.js";
import * as hud from "./hud.js";
import * as about from "./about.js";
import * as advanced from "./advanced.js";
import * as firstrun from "./firstrun.js";

// Nothing may take the window elsewhere: no links, no dropped files.
for (const type of ["dragover", "drop"]) {
  document.addEventListener(type, (e) => e.preventDefault());
}
for (const type of ["click", "auxclick"]) {
  document.addEventListener(type, (e) => {
    if (e.target instanceof Element && e.target.closest("a")) {
      e.preventDefault();
    }
  }, true);
}

// How long Close waits for a slider's write EDSense has not answered yet.
const CLOSE_WAIT = 1000;

// The title bar's buttons: the window has no frame of its own. Close
// sends a slider's waiting write first, so it is not lost.
const capMax = document.getElementById("cap-max");
document.getElementById("cap-min").addEventListener("click", () => call("win.min").catch(() => {}));
capMax.addEventListener("click", () => call("win.max").catch(() => {}));
document.getElementById("cap-close").addEventListener("click", async () => {
  await Promise.race([flushAll(), new Promise((done) => setTimeout(done, CLOSE_WAIT))]);
  call("win.close").catch(() => {});
});
// Alt+F4 or a hidden window: the waiting writes go now
window.addEventListener("pagehide", () => {
  flushAll();
});
on("win", (w) => {
  const max = Boolean(w && w.max);
  document.documentElement.classList.toggle("maximised", max);
  const label = max ? "Restore" : "Maximize";
  capMax.setAttribute("aria-label", label);
  capMax.title = label;
});
// the buttons dim while another window is in front, as Windows' do
function active() {
  document.documentElement.classList.toggle("inactive", !document.hasFocus());
}
window.addEventListener("focus", active);
window.addEventListener("blur", active);
active();

const TITLES = {
  home: "Home",
  controller: "Controller",
  feel: "Feel",
  triggers: "Triggers",
  lights: "Lights",
  gyro: "Gyro aim",
  hud: "HUD reader",
  advanced: "Advanced",
  about: "About",
};
const PAGES = { home, controller, feel, triggers, lights, gyro, hud, advanced, about };
const THEMES = ["system", "light", "dark"];
const MAX_NOTICES = 50;

// app is what the pages see.
const app = {
  version: "",
  status: null, // the latest status from EDSense
  notices: [], // oldest first
  settings: null, // {rev, config, broken, first_run}: edsense.json as EDSense last read it
  schema: [], // every setting's type, range and page
  profiles: {}, // each app's profile card, by app: what EDSense's install service found
  theme: "system",
  call,
  go,
  refresh,
  confirm: dialog.confirm,
  errorText,
  // toast shows a message of the page's own for a while
  toast(text, error) {
    toast({ text }, error);
  },
  // announce has a screen reader read text once; urgent: at once
  announce(text, urgent) {
    announce(urgent ? alarm : say, text);
  },
  // patch changes settings with a merge patch: {"ds4windows_port": 6969}.
  // It resolves to {applied, rev, problems}; a patch with problems
  // changes nothing. The config event brings the new settings.
  patch(obj) {
    return call("settings.patch", { patch: obj });
  },
  // choose switches the controller app at once: auto, dsx or ds4windows.
  // keepPin only saves it while -backend decides this run. It resolves to
  // the engine's state, and rejects with what errorText reads when nothing
  // changed.
  async choose(choice, keepPin) {
    const eng = await call("backend.choose", keepPin ? { choice, keep_pin: true } : { choice });
    if (eng && eng.not_saved) {
      toast({ text: notSavedText(eng.not_saved) }, true);
    }
    return eng;
  },
  endFirstRun,
  // takeProfile keeps an app's profile card when it is newer than the one
  // the page has; the profile event and the profile calls bring them. It
  // reports whether st was new.
  takeProfile(st) {
    if (!newer(st ? app.profiles[st.app] : null, st)) {
      return false;
    }
    app.profiles[st.app] = st;
    return true;
  },
  setTheme(theme) {
    const before = app.theme;
    showTheme(theme);
    refresh();
    call("win.theme", { theme }).catch((err) => {
      if (app.theme === theme) {
        showTheme(before);
        refresh();
      }
      problem(err);
    });
  },
  setPaused(paused) {
    const st = app.status;
    if (st) {
      st.paused = paused; // until the next status says so
      refresh();
    }
    call("pause.set", { paused }).catch((err) => {
      if (st && app.status === st && st.paused === paused) {
        st.paused = !paused; // EDSense did not take it
        refresh();
      }
      problem(err);
    });
  },
};

const main = document.getElementById("main");
const navs = Array.from(document.querySelectorAll(".nav[data-page]"));
const footDot = document.getElementById("foot-dot");
const footText = document.getElementById("foot-text");
const footPause = document.getElementById("foot-pause");
const toasts = document.getElementById("toasts");
const say = document.getElementById("say");
const alarm = document.getElementById("alarm");
let current = null; // the view shown: {el, update(), leave?()}
let currentPage = ""; // "" while the first run shows
let onboarding = null; // the first run's view while it shows
let begun = false; // start() has shown the first view

for (const b of navs) {
  b.addEventListener("click", () => go(b.dataset.page, true));
}
footPause.addEventListener("click", () => {
  if (app.status) {
    app.setPaused(!app.status.paused);
  }
});

// The rail below 860 px shows icons only: the names become tooltips.
const narrow = window.matchMedia("(max-width: 859px)");
function railTitles() {
  for (const b of navs) {
    if (narrow.matches) {
      b.title = TITLES[b.dataset.page];
    } else {
      b.removeAttribute("title");
    }
  }
  setFootPause();
}
narrow.addEventListener("change", railTitles);
railTitles();

// setFootPause labels the sidebar's Pause button; in the rail it is an
// icon with the label as its tooltip.
function setFootPause() {
  const paused = Boolean(app.status && app.status.paused);
  const label = paused ? "Resume" : "Pause";
  if (footPause.dataset.key !== label) {
    footPause.dataset.key = label;
    footPause.replaceChildren(icon(paused ? "play" : "pause", 16), h("span", { class: "label", text: label }));
  }
  if (narrow.matches) {
    footPause.title = label + " effects";
  } else {
    footPause.removeAttribute("title");
  }
}

// Windows' Text size scales every rem size. Should WebView2 ever apply it
// itself, its default font is larger, and the page leaves it at that.
const baseFont = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
function textScale(percent) {
  const f = Number(percent) / 100;
  const scale = baseFont > 16.5 || !(f >= 1 && f <= 2.25) ? 1 : f;
  document.documentElement.style.setProperty("--text-scale", String(scale));
}

// showTheme colours the page; with System, Windows' mode does.
function showTheme(theme) {
  app.theme = THEMES.includes(theme) ? theme : "system";
  if (app.theme === "system") {
    delete document.documentElement.dataset.theme;
  } else {
    document.documentElement.dataset.theme = app.theme;
  }
}

// leave ends the view shown: its polls stop, and a dialog about it closes.
function leave() {
  dialog.closeAll();
  if (current && current.leave) {
    current.leave();
  }
  current = null;
}

// go shows a page.
function go(page, byHand) {
  if (!Object.prototype.hasOwnProperty.call(TITLES, page)) {
    page = "home";
  }
  if (page === currentPage) {
    return;
  }
  currentPage = page;
  leave();
  for (const b of navs) {
    if (b.dataset.page === page) {
      b.setAttribute("aria-current", "page");
    } else {
      b.removeAttribute("aria-current");
    }
  }
  const mod = PAGES[page] || home;
  current = mod.view(app, TITLES[page]);
  main.replaceChildren(current.el);
  main.scrollTop = 0;
  current.update();
  call("win.page", { page }).catch(() => {});
  if (byHand) {
    const title = current.el.querySelector("h1");
    if (title) {
      title.tabIndex = -1;
      title.focus({ preventScroll: true });
    }
  }
}

// While the first run shows, the toasts sit above its footer: they would
// cover its buttons, and hovering one keeps it.
const footWatch = new ResizeObserver(liftToasts);
function liftToasts() {
  const foot = onboarding ? onboarding.el.querySelector(".onboard-foot") : null;
  if (!foot) {
    document.documentElement.style.removeProperty("--toast-lift");
    return;
  }
  const below = parseFloat(getComputedStyle(onboarding.el).paddingBottom) || 0;
  document.documentElement.style.setProperty("--toast-lift", Math.ceil(foot.offsetHeight + below) + "px");
}

// showFirstRun shows the first run in place of the pages: no sidebar, and
// the window remembers the page it had. byHand: it replaces a page, so
// its title takes the focus.
function showFirstRun(byHand) {
  leave();
  currentPage = "";
  document.documentElement.classList.add("onboarding");
  onboarding = firstrun.view(app);
  current = onboarding;
  main.replaceChildren(current.el);
  main.scrollTop = 0;
  current.update();
  const foot = current.el.querySelector(".onboard-foot");
  if (foot) {
    footWatch.observe(foot);
  }
  liftToasts();
  const title = byHand ? current.el.querySelector("h1") : null;
  if (title) {
    title.tabIndex = -1;
    title.focus({ preventScroll: true });
  }
}

// endFirstRun takes the window from the first run to Home.
function endFirstRun() {
  if (!onboarding) {
    return;
  }
  document.documentElement.classList.remove("onboarding");
  go("home", true);
  onboarding = null;
  footWatch.disconnect();
  liftToasts();
}

function refresh() {
  const s = app.status;
  if (s) {
    footDot.dataset.level = s.level;
    setText(footText, s.text);
    footDot.parentElement.title = narrow.matches ? s.text : "";
    footPause.disabled = false;
    setFootPause();
  }
  if (current) {
    current.update();
  }
}

// addNotice keeps a notice once; it reports whether it was new.
function addNotice(n) {
  if (!n || app.notices.some((m) => m.id === n.id)) {
    return false;
  }
  app.notices.push(n);
  app.notices.sort((a, b) => a.id - b.id);
  if (app.notices.length > MAX_NOTICES) {
    app.notices.splice(0, app.notices.length - MAX_NOTICES);
  }
  return true;
}

// announce has a screen reader read text once: politely for EDSense's
// messages, at once for errors. Each text is a node of its own in a live
// region that stays in the page, so only the new one is read.
function announce(region, text) {
  const line = h("div", { text });
  region.append(line);
  setTimeout(() => line.remove(), 10000);
}

// whenSeen runs f once the player is at the window: at once when it has
// the focus, else a moment after it gets it, so a screen reader reads the
// window first. A message waits for that, as a box would.
function whenSeen(f) {
  if (document.hasFocus()) {
    f();
  } else {
    window.addEventListener("focus", () => setTimeout(f, 500), { once: true });
  }
}

const MAX_TOASTS = 3;
const waiting = []; // messages for when a toast has gone

// toast shows a message from EDSense for a while once the player is at
// the window, at most three at once. A fourth pushes out one that was
// seen, or waits: EDSense showed no box for it.
function toast(n, error) {
  if (toasts.children.length >= MAX_TOASTS) {
    const old = Array.from(toasts.children).find((t) => t.dataset.seen === "yes");
    if (!old) {
      waiting.push([n, error]);
      return;
    }
    old.remove();
  }
  const item = h("div", { class: error ? "toast err" : "toast" });
  const gone = () => {
    item.remove();
    while (waiting.length > 0 && toasts.children.length < MAX_TOASTS) {
      toast(...waiting.shift());
    }
  };
  const close = h("button", { class: "btn quiet small", type: "button", "aria-label": "Dismiss", onclick: gone }, icon("close", 14));
  item.append(h("div", { class: "notice-text", text: n.text }), close);
  toasts.append(item);
  let timer = 0;
  let seen = false;
  const later = (ms) => {
    clearTimeout(timer);
    timer = setTimeout(gone, ms);
  };
  whenSeen(() => {
    if (item.isConnected) {
      seen = true;
      item.dataset.seen = "yes";
      announce(error ? alarm : say, n.text);
      later(6000);
    }
  });
  const hold = () => clearTimeout(timer);
  const resume = () => {
    if (seen) {
      later(3000);
    }
  };
  item.addEventListener("mouseenter", hold);
  item.addEventListener("focusin", hold);
  item.addEventListener("mouseleave", resume);
  item.addEventListener("focusout", resume);
}

// notSavedText is the toast for a choice that works but could not be
// saved in edsense.json, with why.
function notSavedText(why) {
  return "Check that edsense.json is not read-only, then choose again. The controller app changed until EDSense quits, " +
    "but it could not be saved: " + String(why || "").replace(/\.$/, "") + ".";
}

// errorText says why a call failed, for a toast or a line on the page.
function errorText(err) {
  const cause = err && err.cause && typeof err.cause === "object" ? err.cause : {};
  const msg = typeof cause.msg === "string" ? cause.msg.replace(/\.$/, "") : "";
  switch (cause.code) {
    case "busy":
      return "EDSense is calibrating the gyro or switching. Try again in a moment.";
    case "broken":
      return (msg ? capital(msg) + ". " : "edsense.json has an error. ") + "Fix the file and save it, then try again.";
    case "failed":
      if (msg === "stopped") {
        return "EDSense is quitting.";
      }
      return msg ? "EDSense could not do it: " + msg + "." : "EDSense could not do it. The log may say more.";
  }
  return "EDSense did not answer. Try again, or see the log.";
}

function problem(err) {
  console.error(err);
  toast({ text: errorText(err) }, true);
}

function restartView() {
  return h("div", { class: "page" },
    h("h1", { text: "Restart EDSense" }),
    h("section", { class: "card" },
      h("p", { class: "lead", text: "EDSense was updated while it ran. Quit it from its tray icon, then start it again to finish the update." })));
}

// Only while the page can be seen does EDSense send its status.
document.addEventListener("visibilitychange", () => {
  call("status.watch", { on: document.visibilityState === "visible" }).catch(() => {});
  if (document.visibilityState === "hidden") {
    flushAll();
  }
});

// EDSense's messages from while this window started, shown once the page
// is: they wait for it in place of a box.
let starting = true;
const held = [];

function showHeld() {
  starting = false;
  held.sort((a, b) => a.id - b.id);
  for (const n of held.splice(0)) {
    toast(n);
  }
}

// newSettings takes s when it is newer than the settings the page has; the
// config event and settings.get may arrive in either order. It reports
// whether s was new.
function newSettings(s) {
  if (!s || typeof s.rev !== "number" || (app.settings && s.rev <= app.settings.rev)) {
    return false;
  }
  const before = app.settings;
  app.settings = s;
  // edsense.json broke while the window showed: the pages show a card, and
  // a screen reader hears it once per error
  if (begun && s.broken && (!before || !before.broken || brokenText(before.broken) !== brokenText(s.broken))) {
    announce(alarm, brokenText(s.broken));
  }
  // the controller app was set elsewhere while the first run showed; a
  // broken file is not that, and the first run's own Finish says so itself
  if (onboarding && !onboarding.finishing && !s.first_run && !s.broken) {
    endFirstRun();
    toast({ text: "The controller app was set from the tray." });
  } else if (begun && !onboarding && before && before.broken && !s.broken && s.first_run) {
    // a fresh file, broken when the window opened, is fixed: the first run
    // waited for it
    showFirstRun(true);
  }
  return true;
}

async function start() {
  try {
    const init = await call("win.init");
    textScale(init.text_scale);
    showTheme(init.theme);
    on("text", textScale);
    app.version = init.version || "";
    if (!init.proto_ok) {
      main.replaceChildren(restartView());
      return;
    }
    on("status", (st) => {
      if (st) {
        app.status = st;
        refresh();
      }
    });
    on("notice", (n) => {
      if (addNotice(n)) {
        if (n.level === "message") {
          if (starting) {
            held.push(n);
          } else {
            toast(n);
          }
        }
        refresh();
      }
    });
    on("config", (s) => {
      if (newSettings(s)) {
        refresh();
      }
    });
    on("profile", (st) => {
      if (app.takeProfile(st)) {
        refresh();
      }
    });
    // answered once EDSense has the whole status, so the first view shows it
    await call("status.watch", { on: true });
    const [st, list, settings, schema] = await Promise.all([
      call("status.get"), call("notices.list"), call("settings.get"), call("settings.schema")]);
    app.status = st;
    app.schema = schema || [];
    newSettings(settings);
    const after = typeof init.after === "number" ? init.after : Infinity;
    for (const n of list || []) {
      if (addNotice(n) && n.level === "message" && n.id > after) {
        held.push(n);
      }
    }
    // a fresh install starts with the first run, and never shows Home first
    if (app.settings && app.settings.first_run) {
      showFirstRun(false);
    } else {
      go(init.page || "home", false);
    }
    begun = true;
    refresh();
    showHeld();
  } catch (err) {
    main.replaceChildren(h("div", { class: "page" },
      h("h1", { text: "EDSense did not answer" }),
      h("p", { class: "lead", text: "Close this window and open it again from the tray icon. The log may say more." })));
    console.error(err);
  } finally {
    showHeld();
    const newest = app.notices.length > 0 ? app.notices[app.notices.length - 1].id : 0;
    call("win.ready", { after: newest }).catch(() => {});
  }
}

start();
