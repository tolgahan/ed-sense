// Lights: the lightbar, the player LEDs and the mic LED, and the
// lightbar's colours, a card each.
import { COLOR_GROUPS, PAGE_LEADS } from "./catalog.js";
import { form } from "./form.js";

// The note under What EDSense sets while the lightbar is left to the
// profile.
export const LIGHTBAR_OFF = "The lightbar follows your profile, so the brightness and colours are not used.";

// The settings of the first card, in the order of its rows.
export const SETS = ["control_lightbar", "lightbar_brightness", "control_player_leds", "control_mic_led"];

// cardId is a colour card's id: "lights-hull-and-health".
export function cardId(title) {
  return "lights-" + String(title).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

export function view(app, title) {
  const F = form(app);

  const sets = F.card({
    id: "lights-sets",
    title: "What EDSense sets",
    reset: SETS,
    body: [
      F.toggle("control_lightbar"),
      F.slider("lightbar_brightness", { raw: true }),
      F.toggle("control_player_leds"),
      F.toggle("control_mic_led"),
    ],
    note: () => (F.value("control_lightbar") === false ? LIGHTBAR_OFF : ""),
  });

  const colours = COLOR_GROUPS.map((g) => {
    const paths = Object.keys(g.keys).map((k) => "colors." + k);
    return F.card({
      id: cardId(g.title),
      title: g.title,
      reset: paths,
      body: paths.map((p) => F.color(p)),
    });
  });

  const el = F.page({ title, lead: PAGE_LEADS["lights"] }, sets, colours);
  return { el, update: F.update, leave: F.leave };
}
