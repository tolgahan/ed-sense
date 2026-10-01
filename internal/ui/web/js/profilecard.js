// Profile cards: EDSense's profile for Elite in DSX or DS4Windows, as
// EDSense's install service finds it, and the buttons that write it. The
// work runs after the question closes, and the card shows how it goes.
import { h, icon, setButton, setText } from "./dom.js";
import { checkRows } from "./checklist.js";

// The cards' titles, by app.
export const TITLES = { dsx: "DSX profile", ds4windows: "DS4Windows profile" };

// What a card says before EDSense has looked.
const LOOKING = { dsx: "Checking DSX's profiles...", ds4windows: "Checking DS4Windows' settings..." };

// The folders of copies folder.open shows, by app.
export const BACKUPS = { dsx: "dsx_backups", ds4windows: "ds4windows_backups" };

// The link buttons' labels, by url.open id.
const LINKS = { ds4windows_releases: "Get DS4Windows 5", ds4windows_doc: "DS4Windows guide" };

// A step's chip, by its state: the word and the tone.
export const STEP_CHIPS = { done: ["Done", "ok"], todo: ["To do", ""], skip: ["Not needed", ""] };

// The buttons a card can have, in their order, with their look.
const BUTTONS = [
  ["install", "btn primary", ""],
  ["reset", "btn danger", ""],
  ["cancel", "btn", ""],
  ["link", "btn", "link"],
  ["again", "btn", "refresh"],
  ["backups", "btn", "folder"],
];

// The states in which a card may look again by hand: nothing can be set
// up until something outside EDSense changes.
const LOOK_AGAIN = ["blocked", "no_folder"];

// The states of a write that was asked for, until its end is seen.
export const JOB_STATES = ["waiting", "writing", "done", "failed", "interrupted"];

// newer: st is the card of a known app, newer than have, the card the
// page has for that app.
export function newer(have, st) {
  return Boolean(st && Object.prototype.hasOwnProperty.call(TITLES, st.app) && typeof st.rev === "number" &&
    (!have || st.rev > have.rev));
}

// actions are the buttons card st offers, in order, as {id, label}. A
// write that failed is tried again with its own question.
export function actions(st) {
  if (!st) {
    return [];
  }
  const out = [];
  const failed = st.state === "failed";
  const install = Boolean(st.can_install && st.install);
  if (install) {
    out.push({ id: "install", label: failed ? "Try again..." : "Install..." });
  }
  if (st.can_reset && st.reset) {
    out.push({ id: "reset", label: failed && !install ? "Try again..." : "Reset..." });
  }
  if (st.can_cancel) {
    out.push({ id: "cancel", label: "Cancel" });
  }
  if (st.link) {
    out.push({ id: "link", label: LINKS[st.link] || "Open the guide" });
  }
  if (LOOK_AGAIN.includes(st.state) || (out.length === 0 && (failed || st.state === "interrupted"))) {
    out.push({ id: "again", label: "Check again" });
  }
  if (st.backups) {
    out.push({ id: "backups", label: "Open backups" });
  }
  return out;
}

// stepsShown: the card lists its steps, those of a DS4Windows profile
// that can be set up. While a write waits, they show what it adds; a
// reset adds nothing (its file is in Files).
export function stepsShown(st) {
  if (!st || !Array.isArray(st.steps) || st.steps.length === 0 || st.state === "blocked" || st.state === "unknown") {
    return false;
  }
  return st.state !== "waiting" || st.steps.some((s) => s.state === "todo");
}

// filesText says where what the card offers writes: the folder, then a
// file per line; "" when it offers to write nothing.
export function filesText(st) {
  if (!st || !Array.isArray(st.files) || st.files.length === 0) {
    return "";
  }
  return (st.dir ? "In " + st.dir + ":\n" : "") + st.files.join("\n");
}

// failText says why a profile call did not go through. busy: EDSense is
// writing that profile now.
export function failText(err, errorText) {
  const cause = err && err.cause && typeof err.cause === "object" ? err.cause : {};
  if (cause.code === "busy") {
    return "EDSense is writing the profile now. Try again in a moment.";
  }
  return errorText(err);
}

// installParams are profile.install's parameters for action id ("install"
// or "reset") of which's card, after the player answered its question q:
// its key, so nothing is written when the card asks another question by
// then.
export function installParams(which, id, q) {
  const params = { app: which, reset: id === "reset" };
  if (q && typeof q.key === "string" && q.key !== "") {
    params.key = q.key;
  }
  return params;
}

// cancelText is what a cancel answered with reply says: "Cancelled.", or
// the card's text when the write was not cancelled (it runs, or it ended).
export function cancelText(reply) {
  return reply && JOB_STATES.includes(reply.state) && reply.text ? reply.text : "Cancelled.";
}

// question is the dialog for a card's question q.
export function question(q) {
  return {
    title: q.title, text: q.text, items: q.items || [], notes: q.notes || [],
    ok: q.ok, danger: Boolean(q.danger),
  };
}

// profileCard is the card of which's profile (dsx or ds4windows), from
// the card app.profiles has for it; prefix makes its ids unique on the
// page. show(on) shows or hides it, and asks for the newest card when it
// comes into view. update(items) shows the card, with items (DS4Windows'
// HidHide row: a state and a How) under its buttons. focus() takes the
// player to it, on its first button.
export function profileCard(app, which, prefix) {
  const ids = { title: prefix + "-title", text: prefix + "-text" };
  const title = h("h2", { id: ids.title, text: TITLES[which] });
  const text = h("p", { id: ids.text });
  const steps = h("div", { class: "checks apart", role: "list", "aria-label": "What EDSense sets up", hidden: true });
  const facts = checkRows(app); // the player's own profile for Elite
  const filesBody = h("span", {});
  const files = h("details", { class: "how", hidden: true },
    h("summary", {}, icon("chevron", 14), h("span", { text: "Files" }), h("span", { class: "sr-only", text: ": " + TITLES[which] })),
    h("div", { class: "how-body" }, filesBody));
  const error = h("p", { class: "field-error", id: prefix + "-error" });
  const buttons = {};
  const bar = h("div", { class: "actions", hidden: true }, BUTTONS.map(([id, cls]) => {
    buttons[id] = h("button", { class: cls, type: "button", hidden: true, "aria-describedby": ids.text, onclick: () => act(id) });
    return buttons[id];
  }));
  const more = checkRows(app);
  more.el.classList.add("apart");
  const el = h("section", { class: "card stack", "aria-labelledby": ids.title, hidden: true },
    title, text, facts.el, steps, files, error, bar, more.el);
  const stepRows = new Map(); // step id -> its row's parts
  let busy = ""; // the action whose call is on its way
  let errText = "";
  let errState = ""; // the card's state when the error came
  let items = [];

  const card = () => (app.profiles && app.profiles[which]) || null;

  // take keeps a card EDSense answered with, when it is the newest.
  function take(st) {
    if (st && app.takeProfile(st)) {
      app.refresh();
    }
  }

  // look asks for the newest card.
  function look() {
    return app.call("profile.state", { app: which }).then(take, (err) => {
      if (!card()) {
        setError(app.errorText(err));
        update();
      } else {
        console.error(err);
      }
    });
  }

  function setError(t) {
    errText = t;
    const st = card();
    errState = st ? st.state : "";
    if (t) {
      app.announce(t, true);
    }
  }

  async function act(id) {
    if (busy) {
      return;
    }
    const st = card();
    if (id === "link") {
      app.call("url.open", { id: st && st.link }).catch((err) => app.toast(app.errorText(err), true));
      return;
    }
    if (id === "backups") {
      app.call("folder.open", { which: BACKUPS[which] }).catch((err) => app.toast(app.errorText(err), true));
      return;
    }
    let q = null;
    if (id === "install" || id === "reset") {
      q = st && (id === "install" ? st.install : st.reset);
      if (!q || !(await app.confirm(question(q)))) {
        return;
      }
    }
    busy = id;
    setError("");
    update();
    try {
      let reply;
      if (id === "again") {
        reply = await app.call("profile.state", { app: which });
      } else if (id === "cancel") {
        reply = await app.call("profile.cancel", { app: which });
      } else {
        reply = await app.call("profile.install", installParams(which, id, q));
      }
      take(reply);
      if (id === "cancel") {
        app.announce(cancelText(reply), false);
      } else if (id !== "again" && reply && reply.text) {
        app.announce(reply.text, false); // what happens now
      }
    } catch (err) {
      setError(failText(err, app.errorText));
      await look(); // the card as it is now
    } finally {
      busy = "";
      update();
    }
  }

  // showSteps lists the steps, each row kept by its id.
  function showSteps(list) {
    for (const [id, r] of stepRows) {
      if (!list.some((s) => s.id === id)) {
        r.el.remove();
        stepRows.delete(id);
      }
    }
    list.forEach((s, i) => {
      let r = stepRows.get(s.id);
      if (!r) {
        r = { mark: h("span", { class: "mark", "aria-hidden": "true" }), text: h("span", {}), chip: h("span", { class: "chip" }) };
        r.el = h("div", { class: "check", role: "listitem" }, r.mark, h("div", { class: "check-body" }, r.text), r.chip);
        stepRows.set(s.id, r);
      }
      if (r.mark.dataset.state !== s.state) {
        r.mark.dataset.state = s.state;
        if (s.state === "done") {
          r.mark.dataset.tone = "ok";
        } else {
          delete r.mark.dataset.tone;
        }
        r.mark.replaceChildren(s.state === "done" ? icon("check", 16, 2.2) : h("span", { class: "dot" }));
      }
      const [word, tone] = STEP_CHIPS[s.state] || [s.state, ""];
      setText(r.text, s.text || "");
      setText(r.chip, word);
      r.chip.className = tone ? "chip " + tone : "chip";
      if (steps.children[i] !== r.el) {
        steps.insertBefore(r.el, steps.children[i] || null);
      }
    });
    steps.hidden = list.length === 0;
  }

  // focusFirst puts the focus on the card's first button, else its title.
  function focusFirst() {
    const b = BUTTONS.map(([id]) => buttons[id]).find((x) => !x.hidden);
    if (b) {
      b.focus({ preventScroll: true });
    } else {
      title.tabIndex = -1;
      title.focus({ preventScroll: true });
    }
  }

  function update(rows) {
    if (rows !== undefined) {
      items = rows || [];
    }
    const st = card();
    if (errText && st && st.state !== errState) {
      errText = ""; // the card has moved on since
    }
    setText(text, st ? st.text : LOOKING[which]);
    showSteps(stepsShown(st) ? st.steps : []);
    facts.show(st && st.state === "other" && Array.isArray(st.items) ? st.items : []);
    const where = filesText(st);
    // Files may go from under the focus (a write that ended)
    const filesLost = !where && !files.hidden && files.contains(document.activeElement);
    files.hidden = !where;
    setText(filesBody, where);
    setText(error, errText);

    const offered = actions(st);
    if (!st && errText) {
      offered.push({ id: "again", label: "Check again" });
    }
    let lost = false; // a button with the focus goes
    for (const [id, , iconName] of BUTTONS) {
      const b = buttons[id];
      const a = offered.find((x) => x.id === id);
      if (!a) {
        lost = lost || (!b.hidden && b === document.activeElement);
        b.hidden = true;
        continue;
      }
      b.hidden = false;
      setButton(b, iconName, id === "again" && busy === "again" ? "Checking..." : a.label, busy !== "");
    }
    bar.hidden = offered.length === 0;
    if ((lost || filesLost) && !el.hidden) {
      focusFirst();
    }
    more.show(items);
  }

  return {
    el,
    update,
    show(on) {
      if (on === !el.hidden) {
        return;
      }
      el.hidden = !on;
      if (on) {
        look();
      }
    },
    focus() {
      if (el.hidden) {
        return;
      }
      el.scrollIntoView({ block: "start" });
      focusFirst();
    },
  };
}
