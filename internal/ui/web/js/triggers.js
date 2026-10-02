// Triggers: the resistance of R2 and L2 in each situation.
import { h } from "./dom.js";
import { PAGE_LEADS, TRIGGER_GROUPS } from "./catalog.js";
import { form } from "./form.js";
import { triggerEditor } from "./trigger.js";

const HOW = "Start and end are points along the trigger's travel: 0 is at rest, 1 is barely pressed, 9 is pressed nearly all the way. A vibration is felt only past its start.";
const OFF = "The triggers follow your profile, so nothing below is used.";

// slug is a card's id from its title: "card-in-the-ship".
function slug(title) {
  return "card-" + title.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

// situation is a situation's block: its label and help, then an editor
// for R2 and one for L2 side by side, or one for both triggers.
function situation(F, app, row) {
  const sid = "sit-" + row.keys[0].replace(/_[rl]$/, "");
  const label = h("span", { class: "situation-label", id: sid + "-label", text: row.label });
  const help = h("span", { class: "row-sub", id: sid + "-help", text: row.help });
  const names = row.keys.length === 1 ? ["R2 and L2"] : ["R2", "L2"];
  const editors = row.keys.map((k, i) => F.add(triggerEditor(F, app, k, { name: names[i], labelledBy: label.id, describedBy: help.id })));
  return h("div", { class: "row situation", id: sid },
    h("div", { class: "situation-head" }, label, help),
    h("div", { class: "pair" }, editors.map((e) => e.el)));
}

export function view(app, title) {
  const F = form(app);
  const main = F.card({
    id: "card-triggers",
    title: "Adaptive triggers",
    reset: ["control_triggers"],
    body: [F.toggle("control_triggers"), h("p", { class: "note trigger-how", text: HOW })],
    note: () => (F.value("control_triggers") === false ? OFF : ""),
  });
  const cards = TRIGGER_GROUPS.map((g) => F.card({
    id: slug(g.title),
    title: g.title,
    reset: g.rows.flatMap((r) => r.keys).map((k) => "triggers." + k),
    body: g.rows.map((r) => situation(F, app, r)),
  }));
  const el = F.page({ title, lead: PAGE_LEADS["triggers"] }, main, ...cards);
  return { el, update: F.update, leave: F.leave };
}
