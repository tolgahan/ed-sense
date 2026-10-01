// Changing settings from a page. A setting is named by its path in
// edsense.json: "ds4windows_port", "triggers.hit.mode". Each change is a
// patch of its own; until the settings show it, the control keeps what the
// player set, and an edit made in Notepad meanwhile waits for it.

// valueAt is the setting at path in config; undefined when there is none.
export function valueAt(config, path) {
  let v = config;
  for (const k of path.split(".")) {
    if (v === null || typeof v !== "object" || !Object.prototype.hasOwnProperty.call(v, k)) {
      return undefined;
    }
    v = v[k];
  }
  return v;
}

// patchFor is the merge patch that sets path to value.
export function patchFor(path, value) {
  return path.split(".").reduceRight((inner, k) => ({ [k]: inner }), value);
}

// keyOf is the settings schema's row for path: its own, or its map's
// ("haptics_gain.*" for "haptics_gain.hit"); null when there is none.
export function keyOf(schema, path) {
  const rows = schema || [];
  const own = rows.find((k) => k.path === path);
  if (own) {
    return own;
  }
  const dot = path.indexOf(".");
  return dot < 0 ? null : rows.find((k) => k.path === path.slice(0, dot) + ".*") || null;
}

// How long a written change may wait for the settings to show it.
const SHOW_WAIT = 3000;

// edits keeps one view's changes on their way to EDSense. A control takes
// its value from the settings only while shows() says so.
export function edits(app) {
  const pending = new Map(); // path -> {value, rev} on the way

  function settingsRev() {
    return app.settings ? app.settings.rev : 0;
  }

  return {
    pending,

    // shows: the control el for path may take the settings' value now. It
    // does not while it has the focus, or while a change of path is on its
    // way, or written but not in the settings yet.
    shows(el, path) {
      const mark = pending.get(path);
      if (mark && mark.rev > 0 && settingsRev() >= mark.rev) {
        pending.delete(path);
      }
      return !pending.has(path) && !(el && el.contains(document.activeElement));
    },

    // track has the view shown again once el loses the focus, so an edit
    // that waited for it lands then.
    track(el) {
      el.addEventListener("focusout", () => setTimeout(() => app.refresh(), 0));
      return el;
    },

    // set changes path to value. It resolves to settings.patch's answer,
    // {applied, rev, problems}, and rejects when EDSense did not answer.
    async set(path, value) {
      const mark = { value, rev: 0 };
      pending.set(path, mark);
      let reply = null;
      try {
        reply = await app.patch(patchFor(path, value));
        return reply;
      } finally {
        if (pending.get(path) === mark) {
          if (reply && reply.applied && reply.rev > settingsRev()) {
            // written: the config event with it is on its way
            mark.rev = reply.rev;
            setTimeout(() => {
              if (pending.get(path) === mark) {
                pending.delete(path);
                app.refresh();
              }
            }, SHOW_WAIT);
          } else {
            pending.delete(path);
          }
        }
        app.refresh();
      }
    },
  };
}
