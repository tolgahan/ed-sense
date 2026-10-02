// HUD reader: shields, heat and fire groups from the cockpit HUD, the
// HUD's colours, and the debug captures.
import { h, icon } from "./dom.js";
import { HUD_COLORS, PAGE_LEADS, SETTINGS } from "./catalog.js";
import { form, idOf } from "./form.js";
import { pct } from "./format.js";
import { hudNote } from "./values.js";

// The question before the HUD captures go on.
export const CAPTURES_QUESTION = {
  title: "Save HUD captures?",
  text: "EDSense saves small screen captures of the HUD to the hud_debug folder next to edsense.json while it reads the HUD. They show what was on your screen. Turn this off when you are done.",
  ok: "Save captures",
};

// hudLine is what the HUD reader does now, as on Home; "" while the
// status does not say (no status, or status.full false).
export function hudLine(s) {
  if (!s || !s.full || !s.hud) {
    return "";
  }
  const hud = s.hud;
  if (!hud.can) {
    return "Not available on this PC";
  }
  if (!hud.on) {
    return "Off";
  }
  const parts = [];
  if (hud.shield >= 0) {
    parts.push("shield " + pct(hud.shield));
  }
  if (hud.heat >= 0) {
    parts.push("heat " + pct(hud.heat));
  }
  return parts.length > 0 ? "Reading: " + parts.join(", ") : "Waiting: reads in the cockpit, with Elite in front";
}

export function view(app, title) {
  const F = form(app);

  const reader = F.card({
    id: "hud-reader",
    title: "HUD reader",
    reset: ["hud_reader"],
    body: [F.toggle("hud_reader")],
    note: (ctx) => hudLine(ctx.status),
  });

  const colours = F.card({
    id: "hud-colours",
    title: "HUD colours",
    help: SETTINGS["hud_colors"].help,
    reset: ["hud_colors"],
    body: Object.keys(HUD_COLORS).map((k) => F.color("hud_colors." + k)),
  });

  // turning the captures on asks first: they hold what was on the screen
  const captures = F.toggle("hud_debug", {
    confirm: async (on) => !on || app.confirm(CAPTURES_QUESTION),
  });
  const open = h("button", {
    class: "btn", type: "button", "aria-describedby": idOf("hud_debug") + "-help",
    onclick: () => app.call("folder.open", { which: "hud_debug" }).catch((err) => app.toast(app.errorText(err), true)),
  }, icon("folder"), h("span", { text: "Open folder" }));
  const debug = F.card({
    id: "hud-debug",
    title: "Debug captures",
    reset: ["hud_debug"],
    body: [captures, h("div", { class: "actions hud-actions" }, open)],
  });

  const el = F.page({ title, lead: PAGE_LEADS["hud"], note: hudNote }, reader, colours, debug);
  return { el, update: F.update, leave: F.leave };
}
