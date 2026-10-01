// Cards more than one page shows.
import { h, icon, setText } from "./dom.js";

// locked: edsense.json has an error, so the page changes no setting until
// it is fixed. EDSense keeps the settings it had.
export function locked(app) {
  return Boolean(app.settings && app.settings.broken);
}

// brokenText says where edsense.json has its error.
export function brokenText(b) {
  const msg = String(b.msg || "").replace(/\.$/, "");
  const where = b.line > 0 ? "edsense.json has an error at line " + b.line + ", column " + b.col + ": " + msg : msg;
  return where + ". EDSense keeps the settings it had. Fix the file and save it.";
}

let cards = 0; // for the ids of the cards' titles

// brokenCard shows that edsense.json has an error, with the file one
// click away. update() shows or hides it by the settings; focus() puts
// the focus on its button.
export function brokenCard(app) {
  const id = "broken-title-" + ++cards;
  const text = h("p", { class: "alert-text" });
  const open = h("button", {
    class: "btn", type: "button",
    onclick: () => app.call("file.open", { which: "settings" }).catch((err) => app.toast(app.errorText(err), true)),
  }, icon("file"), h("span", { text: "Open edsense.json" }));
  const el = h("section", { class: "card alert", "aria-labelledby": id, hidden: true },
    h("span", { class: "alert-icon", "aria-hidden": "true" }, icon("alert", 20)),
    h("div", { class: "alert-body" },
      h("h2", { id, text: "Error in the settings file" }),
      text,
      h("div", { class: "actions" }, open)));
  return {
    el,
    update() {
      const b = app.settings && app.settings.broken;
      el.hidden = !b;
      if (b) {
        setText(text, brokenText(b));
      }
    },
    focus() {
      if (!el.hidden) {
        open.focus();
      }
    },
  };
}
