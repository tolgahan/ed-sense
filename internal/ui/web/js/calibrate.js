// Calibrate gyro: the button, the hint that says why it is off, and the
// question before it starts, in place of the message box the tray shows.
// Home and the Gyro aim page each make their own.
import { h, setButton, setText } from "./dom.js";

// calibrator makes the flow, with ids from prefix ("<prefix>-hint"). el
// holds the button, the hint and the question; Home places those three
// (button, hint, confirm) in its own layout. update(status) follows the
// status.
export function calibrator(app, prefix) {
  let cal = "idle"; // idle, confirm or busy
  let timer = 0;
  let saw = false; // the status said it calibrates

  // aria-disabled in place of disabled: the button keeps the focus while
  // it calibrates, and its hint says why it is off
  const button = h("button", {
    class: "btn", type: "button", "aria-describedby": prefix + "-hint",
    onclick: () => button.getAttribute("aria-disabled") !== "true" && set("confirm"),
  });
  const hint = h("p", { class: "hint", id: prefix + "-hint", hidden: true });
  const start = h("button", {
    class: "btn primary", type: "button", text: "Start",
    onclick: () => {
      saw = false;
      set("busy");
      timer = setTimeout(() => set("idle"), 8000);
      app.call("gyro.calibrate").catch(() => set("idle"));
    },
  });
  const cancel = h("button", { class: "btn quiet", type: "button", text: "Cancel", onclick: () => set("idle", true) });
  const confirm = h("div", { class: "confirm", role: "group", "aria-label": "Calibrate gyro", hidden: true },
    h("div", { text: "Put the controller down on a flat surface and let go, then press Start. Keep it still for 2 seconds." }),
    h("div", { class: "actions" }, start, cancel));
  confirm.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      set("idle", true);
    }
  });
  const el = h("div", { class: "calibrate" }, h("div", { class: "actions" }, button), hint, confirm);

  function set(next, refocus) {
    if (next !== "busy") {
      clearTimeout(timer);
    }
    cal = next;
    update(app.status);
    if (next === "confirm") {
      start.focus();
    } else if (next === "busy" || refocus) {
      button.focus(); // it now says "Calibrating... keep still"
    }
  }

  function update(s) {
    if (!s) {
      return;
    }
    let why = "";
    if (s.full && !(s.gyro && s.gyro.has)) {
      why = "This controller connection passes no gyro.";
    } else if (!s.elite) {
      why = "Start Elite first to calibrate the gyro.";
    }
    if (cal === "busy") {
      if (s.gyro && s.gyro.calibrating) {
        saw = true;
      } else if (saw) {
        clearTimeout(timer);
        cal = "idle";
      }
    }
    if (why && cal === "confirm") {
      cal = "idle";
    }
    setButton(button, "target", cal === "busy" ? "Calibrating... keep still" : "Calibrate gyro", Boolean(why || cal === "busy" || s.demo));
    const focusIn = confirm.contains(document.activeElement);
    button.hidden = cal === "confirm";
    confirm.hidden = cal !== "confirm";
    if (focusIn && confirm.hidden) {
      button.focus(); // the question closed under the focus
    }
    hint.hidden = !why;
    setText(hint, why);
  }

  return { el, button, hint, confirm, update };
}
