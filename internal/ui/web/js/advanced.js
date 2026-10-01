// Advanced: the window's theme. The rest of the page comes in a later
// step.
import { h } from "./dom.js";
import { laterCard } from "./later.js";

const THEMES = [["system", "System"], ["light", "Light"], ["dark", "Dark"]];

export function view(app, title) {
  const seg = h("div", { class: "seg", role: "radiogroup", "aria-labelledby": "theme-label", "aria-describedby": "theme-sub" },
    THEMES.map(([id, label]) => h("label", {},
      h("input", { type: "radio", name: "theme", value: id, onchange: () => app.setTheme(id) }),
      h("span", { text: label }))));
  const el = h("div", { class: "page" },
    h("h1", { text: title }),
    h("section", { class: "card list", "aria-labelledby": "appearance" },
      h("h2", { id: "appearance", text: "Appearance" }),
      h("div", { class: "rows" },
        h("div", { class: "row wrap" },
          h("span", { class: "row-label" },
            h("span", { id: "theme-label", text: "Theme" }),
            h("span", { class: "row-sub", id: "theme-sub", text: "System follows the light or dark mode of Windows." })),
          seg))),
    laterCard(app, "The rest of this page comes in a later step. Until then its settings are in edsense.json."));

  function update() {
    for (const input of seg.querySelectorAll("input")) {
      input.checked = input.value === app.theme;
    }
  }
  return { el, update };
}
