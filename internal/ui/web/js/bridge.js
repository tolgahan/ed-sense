// The page's only way out: one Go method in the window process, which
// checks each call against the allowlist and passes it on to EDSense.
import { Call, Events } from "/wails/runtime.js";

// Alias of the Go method Bridge.Call (callAlias in window_windows.go).
const CALL = 1;

// call asks the window process for method m with params p.
export function call(m, p) {
  return Call.ByID(CALL, p === undefined ? { m } : { m, p });
}

// on calls f with each "name" event from EDSense: status, notice.
export function on(name, f) {
  return Events.On("edsense:" + name, (ev) => f(ev ? ev.data : undefined));
}
