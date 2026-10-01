// Setup checklists: what EDSense finds in a controller app, one row per
// check, asked again every 2 seconds while the page can be seen.
import { h, icon, setText } from "./dom.js";

// The checks of the DS4Windows profile in use; the profile's card shows
// them, the rest are the app's setup.
export const PROFILE_IDS = ["profile", "gyro", "touchpad", "trigger_lab"];

// How often a page asks again.
export const EVERY = 2000;

// The mark's tone and shape per item state; wait, unknown and later are
// an idle dot. The shapes tell the states apart without their colours.
const TONES = { ok: "ok", warn: "warn", bad: "err" };
const MARKS = { ok: "check", warn: "alert", bad: "close" };

// What a screen reader hears before the text, as the dot shows it.
const SAID = { ok: "OK: ", warn: "Warning: ", bad: "Problem: " };

// The link buttons' labels, by url.open id.
const LINKS = { ds4windows_doc: "DS4Windows guide", ds4windows_releases: "Get DS4Windows 5" };

// poll runs task every ms while the page can be seen, one run at a time,
// from start() until stop(). now(fresh) runs it at once; a run on its way
// is waited for first. task(fresh) returns a promise.
export function poll(task, ms = EVERY) {
  let on = false;
  let timer = 0;
  let running = null;

  const visible = () => document.visibilityState !== "hidden";

  function next() {
    clearTimeout(timer);
    timer = 0;
    if (on && visible()) {
      timer = setTimeout(() => run(false), ms);
    }
  }

  function run(fresh) {
    if (!on) {
      return Promise.resolve();
    }
    if (running) {
      return running.then(() => run(fresh));
    }
    clearTimeout(timer);
    timer = 0;
    running = Promise.resolve()
      .then(() => task(fresh))
      .catch((err) => console.error(err))
      .finally(() => {
        running = null;
        next();
      });
    return running;
  }

  function seen() {
    if (visible()) {
      run(false);
    } else {
      clearTimeout(timer);
      timer = 0;
    }
  }

  return {
    start() {
      if (on) {
        return;
      }
      on = true;
      document.addEventListener("visibilitychange", seen);
      if (visible()) {
        run(false);
      }
    },
    stop() {
      on = false;
      clearTimeout(timer);
      timer = 0;
      document.removeEventListener("visibilitychange", seen);
    },
    now(fresh) {
      return run(Boolean(fresh));
    },
  };
}

// setupWatch asks setup.check for the app which() names (dsx or
// ds4windows; nothing while it names none) and hands got(setup, err) each
// answer: setup is {app, t, items}, or null with err when the call failed.
// An answer for an app which() no longer names is dropped.
export function setupWatch(app, which, got) {
  return poll(async (fresh) => {
    const name = which();
    if (!name) {
      return;
    }
    try {
      const setup = await app.call("setup.check", { app: name, fresh });
      if (setup && setup.app === which()) {
        got(setup, null);
      }
    } catch (err) {
      if (name === which()) {
        got(null, err);
      }
    }
  });
}

// away moves the focus out of node, which is about to go, to to; a
// screen reader then reads on from there.
function away(node, to) {
  if (node.contains(document.activeElement)) {
    to.tabIndex = -1;
    to.focus({ preventScroll: true });
  }
}

// checkRows shows a checklist's items that keep(item) takes. Each row is
// kept by its id and changed in place, so an open How stays open and the
// focus stays where it is; a How or a row that goes leaves the focus on
// the row or the list.
export function checkRows(app, keep = () => true) {
  const el = h("div", { class: "checks", role: "list" });
  const rows = new Map(); // id -> the row's parts

  function row(id) {
    const mark = h("span", { class: "mark", "aria-hidden": "true" });
    const said = h("span", { class: "sr-only" });
    const text = h("span", {});
    const body = h("div", { class: "check-body" }, h("span", {}, said, text));
    const r = { el: h("div", { class: "check", role: "listitem", "data-id": id }, mark, body), mark, said, text, body, how: null };
    rows.set(id, r);
    return r;
  }

  // setMark shows a row's state as a coloured shape.
  function setMark(r, state) {
    if (r.mark.dataset.state === state) {
      return;
    }
    r.mark.dataset.state = state;
    const tone = TONES[state];
    if (tone) {
      r.mark.dataset.tone = tone;
    } else {
      delete r.mark.dataset.tone;
    }
    r.mark.replaceChildren(MARKS[state] ? icon(MARKS[state], 16, 2.2) : h("span", { class: "dot" }));
  }

  // setHow gives a row its folded How text and link button, or takes
  // them away. Its summary names the item for a screen reader: every row
  // has a How.
  function setHow(r, how, link, what) {
    if (!how && !link) {
      if (r.how) {
        away(r.how.el, r.el);
        r.how.el.remove();
        r.how = null;
      }
      return;
    }
    if (!r.how) {
      const text = h("span", {});
      const body = h("div", { class: "how-body" }, text);
      const about = h("span", { class: "sr-only" });
      const summary = h("summary", {}, icon("chevron", 14), h("span", { text: "How" }), about);
      const el = h("details", { class: "how" }, summary, body);
      r.how = { el, body, text, about, summary, link: "", button: null };
      r.body.append(el);
    }
    setText(r.how.about, what ? ": " + what : "");
    setText(r.how.text, how || "");
    r.how.text.hidden = !how;
    if (r.how.link !== (link || "")) {
      r.how.link = link || "";
      if (r.how.button) {
        away(r.how.button, r.how.summary);
        r.how.button.remove();
        r.how.button = null;
      }
      if (link) {
        r.how.button = h("button", {
          class: "btn small", type: "button",
          onclick: () => app.call("url.open", { id: link }).catch((err) => app.toast(app.errorText(err), true)),
        }, icon("link", 14), h("span", { text: LINKS[link] || "Open the guide" }));
        r.how.body.append(r.how.button);
      }
    }
  }

  return {
    el,
    // show puts items in the rows; null: not checked yet.
    show(items) {
      const list = items ? items.filter(keep) : [{ id: "", state: "wait", text: "Checking..." }];
      const ids = new Set(list.map((it) => it.id));
      for (const [id, r] of rows) {
        if (!ids.has(id)) {
          away(r.el, el);
          r.el.remove();
          rows.delete(id);
        }
      }
      list.forEach((it, i) => {
        const r = rows.get(it.id) || row(it.id);
        setMark(r, it.state);
        setText(r.said, SAID[it.state] || "");
        setText(r.text, it.text || "");
        setHow(r, it.how, it.link, it.text);
        if (el.children[i] !== r.el) {
          el.insertBefore(r.el, el.children[i] || null);
        }
      });
      el.hidden = list.length === 0;
    },
  };
}
