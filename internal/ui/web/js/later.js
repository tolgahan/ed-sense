// A page that comes in a later step: its settings are in edsense.json.
import { h, icon } from "./dom.js";

// laterCard says what comes in a later step, with the files to use until
// then.
export function laterCard(app, text) {
  const open = (which) => () => app.call("file.open", { which }).catch(() => {});
  return h("section", { class: "card later" },
    h("span", { class: "muted", text }),
    h("div", { class: "actions" },
      h("button", { class: "btn", type: "button", onclick: open("settings") }, icon("file"), h("span", { text: "Open edsense.json" })),
      h("button", { class: "btn quiet", type: "button", onclick: open("log") }, h("span", { text: "Open log" }))));
}

export function view(app, title) {
  const el = h("div", { class: "page" },
    h("h1", { text: title }),
    laterCard(app, "This page comes in a later step. Until then its settings are in edsense.json."));
  return { el, update() {} };
}
