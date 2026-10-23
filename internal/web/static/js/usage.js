// Usage tab: one row per account, saved readings only; only the Refresh
// buttons call a vendor.
import { h, clear } from "./dom.js";
import { store, refresh } from "./api.js";
import { fmtReset, tick } from "./time.js";
import { currentTab } from "./header.js";

export const uses = ["usage", "doctor", "state"];

const BAR_PX = 180;
const DAY_MS = 24 * 60 * 60 * 1000;
const RATE = "rate-limited until ";
const EXPIRED_NOTE = "Open Claude Code with this account to refresh its login.";

let host = null;
const busy = new Set();
let busyAll = false;

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
  const names = new Set();
  const found = store.doctor ? store.doctor.findings : [];
  for (const f of found || []) if (f.severity === "error") names.add(f.subject);
  return names;
}

function windowRow(w) {
  const used = Math.round(w.used_pct);
  const fill = h("div", { class: "bar-fill" });
  fill.style.width = `${Math.min(100, Math.max(0, used)) * BAR_PX / 100}px`;
  return h("div", { class: "u-win" },
    h("span", { class: "mono u-label" }, w.label),
    h("strong", { class: "u-pct" }, `${used}%`),
    h("div", { class: "bar", "aria-hidden": "true" }, fill),
    h("span", { class: "mono u-left", "data-left": w.resets_at }),
    h("span", { class: "mono u-reset", "data-reset": w.resets_at }));
}

function noteFor(row) {
  if (row.state === "expired token") return EXPIRED_NOTE;
  if (row.reading_age_seconds === null || row.reading_age_seconds === undefined) return "no reading yet";
  return "";
}

// takenAt remembers when each reading was taken, fixed when its age first
// arrives, so a repaint that is not a reload does not move it.
const taken = new Map();

function takenAt(row) {
  const s = row.reading_age_seconds;
  if (s === null || s === undefined) return "";
  const old = taken.get(row.account);
  if (old && old.age === s) return old.at;
  const at = String(Date.now() - s * 1000);
  taken.set(row.account, { age: s, at });
  return at;
}

function ageCell(row) {
  const at = takenAt(row);
  return h("span", { class: "u-age", "data-age-at": at });
}

async function refreshOne(account) {
  if (busy.has(account)) return;
  busy.add(account);
  paint();
  try {
    await refresh(account);
  } finally {
    busy.delete(account);
    paint();
  }
}

async function refreshAll() {
  busyAll = true;
  paint();
  try {
    const todo = store.usage.filter((r) => r.can_refresh && !busy.has(r.account));
    await Promise.allSettled(todo.map((r) => refreshOne(r.account)));
  } finally {
    busyAll = false;
    paint();
  }
}

function refreshButton(row) {
  const wait = busy.has(row.account);
  return h("button", { type: "button", class: "btn btn-md btn-outline ro-ok", "data-focus": row.account,
    disabled: !row.can_refresh || wait || busyAll,
    onClick: () => refreshOne(row.account) }, wait ? "Refreshing…" : "Refresh");
}

function accountRow(row, red) {
  const note = noteFor(row);
  const warn = row.state !== "ok";
  return h("div", { class: ["u-grid", "u-row", red.has(row.account) && "is-error"] },
    h("span", { class: "u-name" }, row.account),
    h("div", {}, h("span", { class: ["chip", warn ? "chip-warn" : "chip-ok"] }, stateText(row.state))),
    h("div", { class: "u-wins" }, (row.windows || []).map(windowRow),
      note && h("span", { class: "u-note" }, note)),
    ageCell(row),
    h("div", { class: "u-act" }, refreshButton(row)));
}

function headRow() {
  return h("div", { class: "u-grid u-head" },
    h("span", {}, "Account"), h("span", {}, "State"), h("span", {}, "Windows"),
    h("span", {}, "Resets"), h("span", {}, "Last update"),
    h("div", { class: "u-act" },
      h("button", { type: "button", class: "btn btn-md btn-primary ro-ok", "data-focus": "*", disabled: busyAll,
        onClick: refreshAll }, busyAll ? "Refreshing…" : "Refresh all")));
}

// rows follow the config's account order; an account without a usage row is skipped.
function ordered() {
  const by = new Map(store.usage.map((r) => [r.account, r]));
  const names = store.state ? store.state.accounts.map((a) => a.name) : [];
  return names.filter((n) => by.has(n)).map((n) => by.get(n));
}

function paint() {
  if (!host || currentTab() !== "usage") return;
  const active = document.activeElement;
  const keep = active && host.contains(active) ? active.dataset.focus : undefined;
  clear(host);
  const red = errorAccounts();
  host.append(h("h1", {}, "Usage"),
    h("div", { class: "panel u-box" }, headRow(), ordered().map((r) => accountRow(r, red))));
  tick(host);
  if (keep !== undefined) {
    const again = [...host.querySelectorAll("[data-focus]")].find((b) => b.dataset.focus === keep);
    if (again) again.focus();
  }
}

export function render(root) {
  host = root;
  paint();
}
