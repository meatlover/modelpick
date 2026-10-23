// Time zone choice and every time format the page shows. No DOM: the test
// runs this module outside a browser too.

const KEY = "modelpick.tz";
const MIN = 60 * 1000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

let current = null;
const listeners = [];

function stored() {
  try {
    return localStorage.getItem(KEY) === "local" ? "local" : "utc";
  } catch {
    return "utc";
  }
}

// zone returns "utc" or "local"; the default is "utc".
export function zone() {
  if (current === null) current = stored();
  return current;
}

// setZone remembers z and tells every onZone listener.
export function setZone(z) {
  current = z === "local" ? "local" : "utc";
  try {
    localStorage.setItem(KEY, current);
  } catch {
    // no storage (private window, file): the choice lasts for this page only
  }
  for (const fn of listeners) fn(current);
}

export function onZone(fn) {
  listeners.push(fn);
}

// localZoneName is the browser's IANA zone, e.g. America/Caracas.
export function localZoneName() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

// localOffsetMinutes is the local zone's offset east of UTC at the given time.
export function localOffsetMinutes(at = new Date()) {
  return -toDate(at).getTimezoneOffset();
}

function toDate(x) {
  return x instanceof Date ? x : new Date(x);
}

function words(list) {
  return list.filter(Boolean).join(" ");
}

// fmtLeft: under a day, floored to minutes (in 1h 50m, in 4h, in 12m); from a
// day, rounded to the nearest hour (in 2d 12h, in 3d). Past times are in 0m.
export function fmtLeft(resetsAt, now = new Date()) {
  const ms = Math.max(0, toDate(resetsAt) - toDate(now));
  if (ms < DAY) {
    const m = Math.floor(ms / MIN);
    const hh = Math.floor(m / 60);
    const mm = m % 60;
    return "in " + words([hh && `${hh}h`, (mm || !hh) && `${mm}m`]);
  }
  const hours = Math.round(ms / HOUR);
  const hh = hours % 24;
  return "in " + words([`${Math.floor(hours / 24)}d`, hh && `${hh}h`]);
}

function parts(date) {
  const f = new Intl.DateTimeFormat("en-US", {
    timeZone: zone() === "utc" ? "UTC" : undefined,
    hourCycle: "h23",
    weekday: "short",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
  const out = {};
  for (const p of f.formatToParts(date)) out[p.type] = p.value;
  return out;
}

// fmtReset: "Fri 09:00" less than 7 days from now, else "Oct 29 00:00", in
// the chosen zone, without a zone suffix.
export function fmtReset(resetsAt, now = new Date()) {
  const t = toDate(resetsAt);
  const p = parts(t);
  const hm = `${p.hour}:${p.minute}`;
  return t - toDate(now) < 7 * DAY ? `${p.weekday} ${hm}` : `${p.month} ${p.day} ${hm}`;
}

function ago(n, unit) {
  return `${n} ${unit}${n === 1 ? "" : "s"} ago`;
}

// fmtAge: just now, 1 min ago, 4 mins ago, 1 hour ago, 2 days ago; null or
// undefined (no reading) is never.
export function fmtAge(seconds) {
  if (seconds === null || seconds === undefined) return "never";
  const s = Math.max(0, seconds);
  if (s < 60) return "just now";
  if (s < 3600) return ago(Math.floor(s / 60), "min");
  if (s < 86400) return ago(Math.floor(s / 3600), "hour");
  return ago(Math.floor(s / 86400), "day");
}

// tick rewrites every relative or zoned time under root, so tabs need not
// re-render for the clock: data-left="<RFC 3339>" gets fmtLeft,
// data-reset="<RFC 3339>" gets fmtReset, and data-age-at="<ms since epoch
// the reading was taken>" gets fmtAge ("" = never).
export function tick(root, now = new Date()) {
  for (const el of root.querySelectorAll("[data-left]")) el.textContent = fmtLeft(el.dataset.left, now);
  for (const el of root.querySelectorAll("[data-reset]")) el.textContent = fmtReset(el.dataset.reset, now);
  for (const el of root.querySelectorAll("[data-age-at]")) {
    const at = el.dataset.ageAt;
    el.textContent = fmtAge(at === "" ? null : Math.floor((now - Number(at)) / 1000));
  }
}
