// Changing settings from a page. A setting is named by its path in
// edsense.json: "ds4windows_port", "triggers.hit.mode". Each change is a
// patch of its own (setMany: several settings in one); until the settings
// show it, the control keeps what the player set, and an edit made in
// Notepad meanwhile waits for it.
export { keyOf, valueAt } from "./values.js";

// put sets obj[k] as an own property, whatever k is ("__proto__" too).
function put(obj, k, v) {
  Object.defineProperty(obj, k, { value: v, enumerable: true, writable: true, configurable: true });
}

function isObject(v) {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

// patchFor is the merge patch that sets path to value.
export function patchFor(path, value) {
  return path.split(".").reduceRight((inner, k) => ({ [k]: inner }), value);
}

// mergePatch is one patch that does what a and then b do: objects merge,
// anything else in b, null too, replaces a's. a and b are not changed.
export function mergePatch(a, b) {
  if (!isObject(a) || !isObject(b)) {
    return b;
  }
  const out = {};
  for (const k of Object.keys(a)) {
    put(out, k, a[k]);
  }
  for (const k of Object.keys(b)) {
    put(out, k, Object.prototype.hasOwnProperty.call(out, k) ? mergePatch(out[k], b[k]) : b[k]);
  }
  return out;
}

// overlaps: one path is another, or under it.
function overlaps(a, b) {
  return a === b || a.startsWith(b + ".") || b.startsWith(a + ".");
}

// How long a written change may wait for the settings to show it.
const SHOW_WAIT = 3000;

// edits keeps one view's changes on their way to EDSense. A control takes
// its value from the settings only while shows() says so.
export function edits(app) {
  // path -> {value, rev} of a write on its way, or {value, rev: 0, held:
  // true, after?} while a write waits to be sent; after: the mark of a
  // write of path still on its way when the hold came
  const pending = new Map();

  function settingsRev() {
    return app.settings ? app.settings.rev : 0;
  }

  // shown: the settings show the write of mark.
  function shown(mark) {
    return mark.rev > 0 && settingsRev() >= mark.rev;
  }

  // waits: a change of path, or of a setting above it, is on its way or
  // held. A mark the settings show now is forgotten.
  function waits(path) {
    const parts = path.split(".");
    for (let n = parts.length; n > 0; n--) {
      const at = parts.slice(0, n).join(".");
      const mark = pending.get(at);
      if (mark && mark.rev > 0 && settingsRev() >= mark.rev) {
        pending.delete(at);
      } else if (mark) {
        return true;
      }
    }
    return false;
  }

  // setMany changes each [path, value] of pairs in one patch. No path may
  // be another or under it. Every path waits until the settings show the
  // reply's rev. It resolves to settings.patch's answer, {applied, rev,
  // problems}, and rejects when EDSense did not answer.
  async function setMany(pairs) {
    pairs.forEach(([a], i) => {
      if (pairs.some(([b], j) => j !== i && overlaps(a, b))) {
        throw new Error("overlapping paths: " + a);
      }
    });
    const marks = pairs.map(([path, value]) => [path, { value, rev: 0 }]);
    for (const [path, mark] of marks) {
      pending.set(path, mark);
    }
    let reply = null;
    try {
      reply = await app.patch(pairs.reduce((p, [path, value]) => mergePatch(p, patchFor(path, value)), {}));
      return reply;
    } finally {
      const ours = marks.filter(([path, mark]) => pending.get(path) === mark);
      // marks a hold took the place of: the hold keeps them as after
      const behind = marks.filter(([path, mark]) => {
        const now = pending.get(path);
        return Boolean(now) && now.held && now.after === mark;
      });
      if (reply && reply.applied && reply.rev > settingsRev()) {
        // written: the config event with it is on its way
        for (const [, mark] of ours.concat(behind)) {
          mark.rev = reply.rev;
        }
        if (ours.length > 0) {
          setTimeout(() => {
            let gone = false;
            for (const [path, mark] of ours) {
              if (pending.get(path) === mark) {
                pending.delete(path);
                gone = true;
              }
            }
            if (gone) {
              app.refresh();
            }
          }, SHOW_WAIT);
        }
      } else {
        for (const [path] of ours) {
          pending.delete(path);
        }
        for (const [path] of behind) {
          delete pending.get(path).after;
        }
      }
      app.refresh();
    }
  }

  return {
    pending,

    // shows: the control el for path may take the settings' value now. It
    // does not while it has the focus, or while a change of path or of a
    // setting above it waits, is on its way, or is written but not in the
    // settings yet.
    shows(el, path) {
      return !waits(path) && !(el && el.contains(document.activeElement));
    },

    // track has the view shown again once el loses the focus, so an edit
    // that waited for it lands then.
    track(el) {
      el.addEventListener("focusout", () => setTimeout(() => app.refresh(), 0));
      return el;
    },

    // hold marks path while a write of value waits to be sent (a slider's
    // last move), so its control keeps value. The write replaces the mark.
    // A write of path still on its way stays in the mark as after, so a
    // move back to the value the settings show is still sent.
    hold(path, value) {
      const was = pending.get(path);
      const after = was && was.held ? was.after : was;
      const mark = { value, rev: 0, held: true };
      if (after && !shown(after)) {
        mark.after = after;
      }
      pending.set(path, mark);
    },

    // drop forgets the held marks at path or under it. Marks of writes on
    // their way stay, also the one a hold kept.
    drop(path) {
      for (const [at, mark] of Array.from(pending)) {
        if (mark.held && (at === path || at.startsWith(path + "."))) {
          if (mark.after && !shown(mark.after)) {
            pending.set(at, mark.after);
          } else {
            pending.delete(at);
          }
        }
      }
    },

    setMany,

    // set changes path to value, as setMany does.
    set(path, value) {
      return setMany([[path, value]]);
    },
  };
}
