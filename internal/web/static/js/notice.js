// Banners under the header, the bottom-right toast and in-page dialogs.
// Never window.alert/confirm/prompt: they block browser automation.
import { h, clear } from "./dom.js";

const TOAST_MS = 10000;

function region(id, attrs) {
  let el = document.getElementById(id);
  if (!el) {
    el = h("div", { id, ...attrs });
    document.body.append(el);
  }
  return el;
}

// banner shows (text non-empty) or removes the red strip with this id.
function banner(id, text) {
  const old = document.getElementById(id);
  if (old) old.remove();
  if (!text) return;
  const strip = h("div", { id, class: "banner", role: "alert" },
    h("div", { class: "banner-in" }, h("strong", {}, text)));
  region("notices").append(strip);
}

// stopped shows or hides the "has stopped" banner; it returns true when that
// changed what is on screen.
export function stopped(on) {
  if (Boolean(on) === Boolean(document.getElementById("notice-stopped"))) return false;
  banner("notice-stopped", on ? "modelpick ui has stopped" : "");
  return true;
}

// readOnly shows state.read_only (the parse error) verbatim; "" removes it.
export function readOnly(text) {
  const old = document.getElementById("notice-readonly");
  if (old && old.textContent === text) return;
  banner("notice-readonly", text);
}

let toastTimer = 0;

// toast replaces the previous toast and hides after 10 s. strong, when
// given, leads the text in bold ("Not applied.").
export function toast(text, strong) {
  const box = region("toasts", { "aria-live": "polite" });
  clear(box);
  clearTimeout(toastTimer);
  box.append(h("div", { class: "toast" },
    h("span", {}, strong ? h("strong", {}, strong) : null, strong ? " " : null, text)));
  toastTimer = setTimeout(() => clear(box), TOAST_MS);
}

let conflictOpen = null;

function conflictStrip(done) {
  return h("div", { id: "notice-conflict", class: "banner", role: "alertdialog",
    "aria-labelledby": "notice-conflict-title" },
  h("div", { class: "banner-in" },
    h("strong", { id: "notice-conflict-title" }, "config.json changed outside the UI"),
    h("div", { class: "banner-actions" },
      h("button", { type: "button", class: "btn btn-lg btn-danger", onClick: () => done("load") },
        "Load the file's version"),
      h("button", { type: "button", class: "btn btn-lg btn-danger-ghost", onClick: () => done("apply") },
        "Apply my change on top"))));
}

// conflictDialog resolves "load" or "apply" with the user's choice. A second
// call while it is open gets the same answer.
export function conflictDialog() {
  if (conflictOpen) return conflictOpen;
  conflictOpen = new Promise((resolve) => {
    const strip = conflictStrip((choice) => {
      strip.remove();
      conflictOpen = null;
      resolve(choice);
    });
    region("notices").append(strip);
    strip.querySelector("button").focus();
  });
  return conflictOpen;
}

// confirmDialog resolves true on okLabel, false on Cancel or Escape.
export function confirmDialog(text, okLabel) {
  return new Promise((resolve) => {
    const dlg = h("dialog", { class: "dlg", "aria-label": text },
      h("p", { class: "dlg-text" }, text),
      h("div", { class: "dlg-actions" },
        h("button", { type: "button", class: "btn", onClick: () => dlg.close("") }, "Cancel"),
        h("button", { type: "button", class: "btn btn-primary", onClick: () => dlg.close("ok") }, okLabel)));
    dlg.addEventListener("close", () => {
      dlg.remove();
      resolve(dlg.returnValue === "ok");
    });
    document.body.append(dlg);
    dlg.showModal();
  });
}
