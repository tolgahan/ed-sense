// Numbers and times in en-US whatever the Windows locale: a Turkish
// Windows would write "%80" and "0,05".

const whole = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });

function twoDigits() {
  try {
    return new Intl.NumberFormat("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2, signDisplay: "negative" });
  } catch {
    return new Intl.NumberFormat("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  }
}
const fixed2 = twoDigits();

const clockFormat = new Intl.DateTimeFormat("en-US", { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });

// pct is a percentage: 80%.
export function pct(v) {
  return whole.format(v) + "%";
}

// count is a number of things: "1 controller", "2 controllers".
export function count(n, one, many) {
  return whole.format(n) + " " + (n === 1 ? one : many);
}

// drift is the gyro's drift on its three axes.
export function drift(d) {
  return (d || [0, 0, 0]).map((x) => fixed2.format(x)).join(" ") + " deg/s";
}

// clock is a time of day, 24-hour: 14:05.
export function clock(ms) {
  return clockFormat.format(new Date(ms));
}

// capital starts a sentence.
export function capital(s) {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : "";
}
