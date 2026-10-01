// Every node is built here, with text set as text only: nothing from
// EDSense or the game is ever read as markup.

// h makes an element. props: class, text, on<event> handlers, and plain
// attributes; children: nodes or strings.
export function h(tag, props, ...children) {
  const el = document.createElement(tag);
  setProps(el, props);
  for (const c of children.flat()) {
    if (c !== null && c !== undefined && c !== false) {
      el.append(c);
    }
  }
  return el;
}

function setProps(el, props) {
  if (!props) {
    return;
  }
  for (const [k, v] of Object.entries(props)) {
    if (v === undefined || v === null || v === false) {
      continue;
    }
    if (k === "class") {
      el.setAttribute("class", v);
    } else if (k === "text") {
      el.textContent = String(v);
    } else if (k.startsWith("on") && typeof v === "function") {
      el.addEventListener(k.slice(2), v);
    } else if (k.startsWith("on") || k === "style") {
      throw new Error("no inline " + k);
    } else {
      el.setAttribute(k, v === true ? "" : String(v));
    }
  }
}

const SVG = "http://www.w3.org/2000/svg";

// Icons on the 24 px grid, drawn with the current text colour.
const ICONS = {
  pause: [["path", { d: "M9 5.5v13M15 5.5v13" }]],
  play: [["path", { d: "M8 5.5 18.5 12 8 18.5z", "stroke-linejoin": "round" }]],
  stop: [["rect", { x: "6.5", y: "6.5", width: "11", height: "11", rx: "1.5" }]],
  target: [["circle", { cx: "12", cy: "12", r: "3" }], ["path", { d: "M12 3v3M12 18v3M3 12h3M18 12h3" }]],
  file: [["path", { d: "M6.5 3.5h7l4 4v13h-11z", "stroke-linejoin": "round" }], ["path", { d: "M13.5 3.5v4h4" }]],
  link: [["path", { d: "M14 4.5h5.5V10M19.5 4.5 11 13", "stroke-linejoin": "round" }], ["path", { d: "M17 13.5v5a1 1 0 0 1-1 1H5.5a1 1 0 0 1-1-1V8a1 1 0 0 1 1-1h5" }]],
  close: [["path", { d: "M6.5 6.5l11 11M17.5 6.5l-11 11" }]],
  check: [["path", { d: "M5 12.5l4.5 4.5L19 7", "stroke-linejoin": "round" }]],
  alert: [["path", { d: "M12 4 21 19.5H3z", "stroke-linejoin": "round" }], ["path", { d: "M12 10v4M12 16.8v.1" }]],
  refresh: [["path", { d: "M19.5 12a7.5 7.5 0 1 1-2.2-5.3" }], ["path", { d: "M18.5 3.5v3.5H15", "stroke-linejoin": "round" }]],
  chevron: [["path", { d: "M9.5 6l6 6-6 6", "stroke-linejoin": "round" }]],
  back: [["path", { d: "M19 12H5.5M11 6l-6 6 6 6", "stroke-linejoin": "round" }]],
};

// icon draws one of ICONS.
export function icon(name, size = 16, stroke = 1.8) {
  const svg = document.createElementNS(SVG, "svg");
  for (const [k, v] of Object.entries({
    width: size, height: size, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor",
    "stroke-width": stroke, "stroke-linecap": "round", "aria-hidden": "true", focusable: "false",
  })) {
    svg.setAttribute(k, String(v));
  }
  for (const [tag, attrs] of ICONS[name] || []) {
    const part = document.createElementNS(SVG, tag);
    for (const [k, v] of Object.entries(attrs)) {
      part.setAttribute(k, v);
    }
    svg.append(part);
  }
  return svg;
}

// setText changes a node's text only when it differs, so screen readers
// hear live regions only on a real change.
export function setText(el, text) {
  const t = String(text);
  if (el.textContent !== t) {
    el.textContent = t;
  }
}
