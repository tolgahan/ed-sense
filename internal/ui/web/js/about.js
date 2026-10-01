// About: the version, the signature, the licence and the credits. Links
// go through EDSense by name; the page never holds an address.
import { h, icon, setText } from "./dom.js";

function row(label, value, cls) {
  return h("div", { class: "row" }, h("span", { class: "row-label", text: label }), h("span", { class: "row-value" + (cls ? " " + cls : "") }, value));
}

function link(app, id, label) {
  return h("button", { class: "btn", type: "button", onclick: () => app.call("url.open", { id }).catch(() => {}) }, icon("link"), h("span", { text: label }));
}

export function view(app) {
  const version = app.version || "dev";
  const signature = h("span", { text: "Checking" });
  const thumb = h("span", { class: "mono", text: "-" });
  const sigRow = row("Signature", signature);
  const el = h("div", { class: "page" },
    h("div", {},
      h("h1", { text: "About" }),
      h("p", { class: "lead", text: "EDSense " + version + " for Elite Dangerous." })),
    h("section", { class: "card list", "aria-label": "This copy" },
      h("div", { class: "rows" },
        row("Version", version),
        sigRow,
        row("Certificate thumbprint", thumb),
        row("License", "MIT"))),
    h("div", { class: "links" },
      link(app, "source", "Source code"),
      link(app, "releases", "Releases"),
      link(app, "signature", "Checking a download"),
      link(app, "license", "License")),
    h("section", { class: "card credits", "aria-label": "Credits" },
      h("h2", { text: "Credits" }),
      h("p", { class: "lead", text: "The gyro's drift calibration follows ideas from GamepadMotionHelpers and JoyShockMapper (MIT license). This window runs on Wails (MIT license) and Microsoft Edge WebView2." }),
      h("div", { class: "links" },
        link(app, "gmh", "GamepadMotionHelpers"),
        link(app, "jsm", "JoyShockMapper"),
        link(app, "wails", "Wails"),
        link(app, "notices", "Third-party notices"))),
    h("p", { class: "note", text: "EDSense is not affiliated with or endorsed by Frontier Developments, Paliverse (DSX), the DS4Windows project or Sony." }));

  const valueOf = sigRow.querySelector(".row-value");
  app.call("win.about").then((a) => {
    const states = {
      valid: ["Signed, valid", "ok"],
      invalid: ["Signed, not valid", "err"],
      none: ["Not signed", ""],
    };
    const [text, cls] = states[a.signature] || states.none;
    setText(signature, text);
    valueOf.className = "row-value" + (cls ? " " + cls : "");
    setText(thumb, a.thumbprint || "-");
  }).catch(() => setText(signature, "Could not check"));

  return { el, update() {} };
}
