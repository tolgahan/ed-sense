// Confirm dialogs inside the window, in place of the tray's message
// boxes. One at a time; the work they ask about runs after they close.
import { h } from "./dom.js";

let made = 0; // for the ids that tie the title and text to the dialog
const shown = new Set(); // the open dialog's {finish}

// confirm asks a question and resolves to true for ok. Escape or Cancel
// is false. danger: the focus starts on Cancel, so Enter does no harm.
// items: lines listed under the text; notes: paragraphs after them. A
// second confirm while one is open answers false at once: the player is
// looking at the first.
export function confirm({ title, text, items, notes, ok, cancel = "Cancel", danger = false }) {
  if (shown.size > 0) {
    return Promise.resolve(false);
  }
  made++;
  const ids = { title: "dlg-title-" + made, text: "dlg-text-" + made, items: "dlg-items-" + made, notes: "dlg-notes-" + made };
  const list = items && items.length > 0 ? h("ul", { id: ids.items }, items.map((item) => h("li", { text: item }))) : null;
  const after = notes && notes.length > 0 ? h("div", { class: "modal-notes", id: ids.notes }, notes.map((note) => h("p", { text: note }))) : null;
  const entry = { finish: null };
  const dlg = h("dialog", {
    class: "modal", role: danger ? "alertdialog" : null,
    "aria-labelledby": ids.title,
    "aria-describedby": [ids.text, list ? ids.items : "", after ? ids.notes : ""].filter(Boolean).join(" "),
  },
  h("h2", { id: ids.title, text: title }),
  h("p", { id: ids.text, text }),
  list,
  after,
  h("div", { class: "actions end" },
    h("button", { class: danger ? "btn danger" : "btn primary", type: "button", autofocus: !danger, text: ok, onclick: () => entry.finish(true, true) }),
    h("button", { class: "btn", type: "button", autofocus: danger, text: cancel, onclick: () => entry.finish(false, true) })));

  const opener = document.activeElement;
  return new Promise((resolve) => {
    let done = false;
    // finish ends the dialog once, however it closes. refocus: the focus
    // goes back to what opened it, while that is still on the page.
    entry.finish = (yes, refocus) => {
      if (done) {
        return;
      }
      done = true;
      shown.delete(entry);
      if (dlg.open) {
        dlg.close();
      }
      dlg.remove();
      if (refocus && opener && opener !== document.body && opener.isConnected && typeof opener.focus === "function") {
        opener.focus();
      }
      resolve(yes);
    };
    // Escape closes it without a button
    dlg.addEventListener("close", () => entry.finish(false, true));
    document.body.append(dlg);
    try {
      // modal: Tab stays inside, and the page under it cannot be clicked
      dlg.showModal();
      shown.add(entry);
    } catch (err) {
      console.error(err);
      entry.finish(false, false);
    }
  });
}

// closeAll cancels the open dialog, when the page it was about goes. The
// focus stays where the new page puts it.
export function closeAll() {
  for (const entry of Array.from(shown)) {
    entry.finish(false, false);
  }
}
