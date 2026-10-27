// Usage tab: one row per account, saved readings only; only the Refresh
// buttons call a vendor.
import { h, clear } from "./dom.js";
import { store, refresh, reload } from "./api.js";
import { fmtReset, tick } from "./time.js";
import { currentTab } from "./header.js";
import { accountErrorSet } from "./subjects.js";

export const uses = ["usage", "doctor", "state"];

const BAR_PX = 180;
const DAY_MS = 24 * 60 * 60 * 1000;
const RATE = "rate-limited until ";

let host = null;
const busy = new Set();
let busyAll = false;
// wanted is the data-focus key of the button the user last used ("*" =
// Refresh all); paint puts focus back on it while it is set.
let wanted = null;
let listening = false;

function listen() {
  if (listening) return;
  listening = true;
  document.addEventListener("pointerdown", (e) => {
    const b = e.target.closest ? e.target.closest("[data-focus]") : null;
    wanted = b ? b.dataset.focus : null;
  });
  document.addEventListener("focusin", (e) => {
    const t = e.target;
    if (t && t.dataset && t.dataset.focus !== undefined) wanted = t.dataset.focus;
    else if (t !== document.body && t !== document.documentElement) wanted = null;
  });
}

// stateText shows a rate-limit end as a time (just the clock within a day).
function stateText(state, now = new Date()) {
  if (!state.startsWith(RATE)) return state;
  const until = state.slice(RATE.length);
  const t = Date.parse(until);
  if (Number.isNaN(t)) return state;
  const full = fmtReset(until, now);
  return RATE + (t - now < DAY_MS ? full.slice(-5) : full);
}

function errorAccounts() {
  return accountErrorSet(store.doctor ? store.doctor.findings : []);
}

// A scoped window can exhaust one model without exhausting the account.
// Only general windows select the account's highlighted reset time.
function fullGeneral(w) {
  return !w.scope && w.used_pct >= 100;
}

function nearestFullReset(windows) {
  let nearest = -1;
  let time = Infinity;
  windows.forEach((w, i) => {
    if (!fullGeneral(w)) return;
    const at = Date.parse(w.resets_at);
    if (Number.isFinite(at) && at < time) {
      nearest = i;
      time = at;
    }
  });
  return nearest;
}

function windowRow(w, limited, highlightReset) {
  const used = Math.round(w.used_pct);
  const fill = h("div", { class: "bar-fill" });
  fill.style.width = `${Math.min(100, Math.max(0, used)) * BAR_PX / 100}px`;
  return h("div", { class: ["u-win", limited && (fullGeneral(w) ? "is-full" : "is-limited"),
    highlightReset && "is-next-reset"] },
    h("span", { class: "mono u-label" }, w.label),
    h("strong", { class: "u-pct" }, `${used}%`),
    h("div", { class: "bar", "aria-hidden": "true" }, fill),
    h("span", { class: "mono u-left", "data-left": w.resets_at }),
    h("span", { class: "mono u-reset", "data-reset": w.resets_at }));
}

// takenAt is when each row's reading was taken: the time the row object
// arrived (a load or a refresh makes new objects) minus its age.
const taken = new WeakMap();

function takenAt(row) {
  const s = row.reading_age_seconds;
  if (s === null || s === undefined) return "";
  if (!taken.has(row)) taken.set(row, String(Date.now() - s * 1000));
  return taken.get(row);
}

function ageCell(row) {
  const at = takenAt(row);
  return h("span", { class: "u-age", "data-age-at": at });
}

// refreshOne refreshes one account; own is false inside Refresh all, which
// reloads once after the batch instead of once per account.
async function refreshOne(account, own = true) {
  if (busy.has(account)) return;
  if (own) wanted = account;
  busy.add(account);
  paint();
  try {
    await refresh(account, { reload: own });
  } finally {
    busy.delete(account);
    paint();
  }
}

async function refreshAll() {
  if (busyAll) return;
  wanted = "*";
  busyAll = true;
  paint();
  try {
    const todo = store.usage.filter((r) => r.can_refresh && !busy.has(r.account));
    await Promise.allSettled(todo.map((r) => refreshOne(r.account, false)));
    await reload();
  } finally {
    busyAll = false;
    paint();
  }
}

function refreshButton(row) {
  const wait = busy.has(row.account);
  return h("button", { type: "button", class: ["btn", "btn-md", "btn-outline", "ro-ok", (wait || busyAll) && "is-busy"],
    "data-focus": row.account, disabled: !row.can_refresh, "aria-disabled": wait || busyAll,
    onClick: () => { if (!wait && !busyAll) refreshOne(row.account).catch(() => {}); } },
  wait ? "Refreshing…" : "Refresh");
}

function accountRow(row, red) {
  const windows = row.windows || [];
  const limited = windows.some(fullGeneral);
  const nextReset = nearestFullReset(windows);
  const state = row.state === "ok" && limited ? "Limit reached" : stateText(row.state);
  const warn = state !== "ok";
  return h("div", { class: ["u-grid", "u-row", red.has(row.account) && "is-error"] },
    h("span", { class: "u-name" }, row.account),
    h("div", {}, h("span", { class: ["chip", warn ? "chip-warn" : "chip-ok"] }, state)),
    h("div", { class: "u-wins" }, windows.map((w, i) => windowRow(w, limited, i === nextReset))),
    ageCell(row),
    h("div", { class: "u-act" }, refreshButton(row)));
}

function headRow() {
  return h("div", { class: "u-grid u-head" },
    h("span", {}, "Account"), h("span", {}, "State"), h("span", {}, "Windows"),
    h("span", {}, "Resets"), h("span", {}, "Last update"),
    h("div", { class: "u-act" },
      h("button", { type: "button", class: ["btn", "btn-md", "btn-primary", "ro-ok", busyAll && "is-busy"],
        "data-focus": "*", "aria-disabled": busyAll,
        onClick: () => { if (!busyAll) refreshAll().catch(() => {}); } },
      busyAll ? "Refreshing…" : "Refresh all")));
}

// rows follow the config's account order; an account without a usage row is skipped.
function ordered() {
  const by = new Map(store.usage.map((r) => [r.account, r]));
  const names = store.state ? store.state.accounts.map((a) => a.name) : [];
  return names.filter((n) => by.has(n)).map((n) => by.get(n));
}

function restoreFocus() {
  if (wanted === null) return;
  const a = document.activeElement;
  if (a !== document.body && !host.contains(a)) return;
  const again = [...host.querySelectorAll("[data-focus]")].find((b) => b.dataset.focus === wanted);
  if (again && !again.disabled) again.focus();
}

function paint() {
  if (!host || currentTab() !== "usage") return;
  listen();
  const red = errorAccounts();
  clear(host);
  host.append(h("div", { class: "panel u-box" }, headRow(), ordered().map((r) => accountRow(r, red))));
  tick(host);
  restoreFocus();
}

export function render(root) {
  host = root;
  paint();
}
