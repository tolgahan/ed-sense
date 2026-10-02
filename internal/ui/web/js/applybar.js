// The Apply bar: the settings that wait for Apply now, and the button that
// applies them. The Controller and Advanced pages show it.
import { h, setButton, setText } from "./dom.js";
import { labelOf } from "./catalog.js";

// drives: EDSense drives the controller now, so a switch is felt.
export function drives(s) {
  return Boolean(s && s.active && s.online);
}

// pendingText is the bar's sentence for the settings that wait for Apply
// now, by the names the pages show them under; "" for none. When the
// catalog lacks one, the bar names them as edsense.json does.
export function pendingText(keys) {
  if (!keys || keys.length === 0) {
    return "";
  }
  const labels = keys.map((k) => labelOf(k));
  const named = labels.every(Boolean);
  const list = (named ? labels : keys).join(", ");
  return (named ? "Changed: " : "Changed in edsense.json: ") + list + ". " +
    (keys.length > 1 ? "They are" : "It is") + " used after EDSense reconnects.";
}

// takeEngine shows what an apply or a choice left running, until the
// next status says so.
export function takeEngine(app, eng) {
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

let bars = 0; // for the ids of the bars' texts

// applyBar is the bar, shown while status.pending names a setting. Apply
// now asks first while EDSense drives the controller, and is described by
// the bar's text. When the bar goes from under the focus, the focus goes
// to focusAfter().
export function applyBar(app, focusAfter) {
  let applying = false;
  const text = h("span", { class: "grow", id: "apply-text-" + ++bars });
  const btn = h("button", { class: "btn primary", type: "button", "aria-describedby": text.id, onclick: apply });
  const el = h("div", { class: "restart-bar", hidden: true }, text, btn);

  async function apply() {
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
    app.refresh();
    try {
      takeEngine(app, await app.call("engine.apply"));
    } catch (err) {
      app.toast(app.errorText(err), true);
    } finally {
      applying = false;
      app.refresh();
    }
  }

  function update() {
    const s = app.status;
    const t = pendingText(s && s.pending);
    if (!t && !el.hidden && el.contains(document.activeElement)) {
      const next = focusAfter ? focusAfter() : null;
      if (next) {
        next.focus(); // applied: the bar goes from under the focus
      }
    }
    el.hidden = !t;
    setText(text, t);
    setButton(btn, "", applying ? "Applying..." : "Apply now", applying);
  }

  return { el, update };
}
