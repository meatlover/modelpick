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

// A scoped window can exhaust one model without exhausting the account, so
// only general windows mark it: from NEAR_PCT a window is near ("warn"), at
// 100 full. An account shows one marked window and dims the rest, since one
// window running out is enough to stop it (owner, 2026-11-02).
const NEAR_PCT = 95;

function level(w) {
  if (w.scope) return "";
  if (w.used_pct >= 100) return "full";
  return w.used_pct >= NEAR_PCT ? "warn" : "";
}

// marked is the index of the window the account shows, or -1: full beats
// warn; among the same level the latest reset (a window without a reset
// time loses to one with it), and on a tie the first.
function marked(windows) {
  const rank = { full: 2, warn: 1, "": 0 };
  let best = -1;
  let bestRank = 0;
  let bestAt = -Infinity;
  windows.forEach((w, i) => {
    const r = rank[level(w)];
    if (r === 0) return;
    const parsed = Date.parse(w.resets_at);
    const at = Number.isFinite(parsed) ? parsed : -Infinity;
    if (r > bestRank || (r === bestRank && at > bestAt)) {
      best = i;
      bestRank = r;
      bestAt = at;
    }
  });
  return best;
}

// windowRow draws one window; mark is "full", "warn", "dim" or "".
function windowRow(w, mark) {
  const used = Math.round(w.used_pct);
  const fill = h("div", { class: "bar-fill" });
  fill.style.width = `${Math.min(100, Math.max(0, used)) * BAR_PX / 100}px`;
  return h("div", { class: ["u-win", mark && `is-${mark}`] },
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

// chipOf is the state chip's text and class: an ok account with a marked
// window says how near it is; any other state keeps its own words.
function chipOf(row, mark) {
  if (row.state !== "ok") return [stateText(row.state), "chip-warn"];
  if (mark === "full") return ["Limit reached", "chip-full"];
  if (mark === "warn") return ["Near limit", "chip-warn"];
  return ["ok", "chip-ok"];
}

function accountRow(row, red) {
  const windows = row.windows || [];
  const shown = marked(windows);
  const mark = shown < 0 ? "" : level(windows[shown]);
  const [state, chip] = chipOf(row, mark);
  return h("div", { class: ["u-grid", "u-row", red.has(row.account) && "is-error"] },
    h("span", { class: "u-name" }, row.account),
    h("div", {}, h("span", { class: ["chip", chip] }, state)),
    h("div", { class: "u-wins" }, windows.map((w, i) =>
      windowRow(w, i === shown ? mark : shown < 0 ? "" : "dim"))),
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
