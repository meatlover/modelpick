// Cases for js/time.js. In the browser, time.test.html writes the verdict
// into <output id="result">; elsewhere run() returns it.
import { setZone, fmtLeft, fmtReset, fmtAge, utcCellsFor, shownFraction } from "../js/time.js";

const NOW = "2026-10-23T07:10:00Z";

// The demo's deepseek-peak: Mon–Fri (days 1–5, 0 = Sun) UTC 01–03 and 06–09
// full; Wed 12 a quarter and Sat 07 half, for the partial cases.
function fixture() {
  const cells = Array.from({ length: 7 }, () => new Array(24).fill(0));
  for (let d = 1; d <= 5; d++) for (const hr of [1, 2, 3, 6, 7, 8, 9]) cells[d][hr] = 1;
  cells[3][12] = 0.25;
  cells[6][7] = 0.5;
  return cells;
}

const json = (x) => JSON.stringify(x);

export const cases = [
  ["fmtLeft 09:00", () => fmtLeft("2026-10-23T09:00:00Z", NOW), "in 1h 50m"],
  ["fmtLeft 11:10", () => fmtLeft("2026-10-23T11:10:00Z", NOW), "in 4h"],
  ["fmtLeft Oct 25 19:00", () => fmtLeft("2026-10-25T19:00:00Z", NOW), "in 2d 12h"],
  ["fmtLeft Oct 26 14:00", () => fmtLeft("2026-10-26T14:00:00Z", NOW), "in 3d 7h"],
  ["fmtLeft Oct 29 00:00", () => fmtLeft("2026-10-29T00:00:00Z", NOW), "in 5d 17h"],
  ["fmtReset Oct 23 09:00", () => fmtReset("2026-10-23T09:00:00Z", NOW), "Fri 09:00"],
  ["fmtReset Nov 17 00:00", () => fmtReset("2026-11-17T00:00:00Z", NOW), "Nov 17 00:00"],
  ["fmtAge 240", () => fmtAge(240), "4 mins ago"],
  ["fmtAge null", () => fmtAge(null), "never"],
  ["fmtLeft exactly 3d", () => fmtLeft("2026-10-26T07:10:00Z", NOW), "in 3d"],
  ["fmtLeft 30 s", () => fmtLeft("2026-10-23T07:10:30Z", NOW), "in 0m"],
  ["fmtLeft 23h 59m", () => fmtLeft("2026-10-24T07:09:00Z", NOW), "in 23h 59m"],
  ["fmtLeft 24h", () => fmtLeft("2026-10-24T07:10:00Z", NOW), "in 1d"],
  ["fmtReset exactly 7 days", () => fmtReset("2026-10-30T07:10:00Z", NOW), "Oct 30 07:10"],
  ["fmtLeft invalid", () => fmtLeft("not a time", NOW), ""],
  ["fmtReset invalid", () => fmtReset("", NOW), ""],
  ["utcCellsFor Sun 21 at -240", () => json(utcCellsFor(0, 21, -240)), "[[1,1]]"],
  ["utcCellsFor Sat 22 at -240 wraps the week", () => json(utcCellsFor(6, 22, -240)), "[[0,2]]"],
  ["utcCellsFor Mon 00 at +330", () => json(utcCellsFor(1, 0, 330)), "[[0,18],[0,19]]"],
  ["utcCellsFor Sun 00 at -570", () => json(utcCellsFor(0, 0, -570)), "[[0,9],[0,10]]"],
  ["utcCellsFor Sun 03 at +540 wraps back", () => json(utcCellsFor(0, 3, 540)), "[[6,18]]"],
  ["utcCellsFor Thu 05 at 0", () => json(utcCellsFor(4, 5, 0)), "[[4,5]]"],
  ["shownFraction Mon 02 at -240", () => shownFraction(fixture(), 1, 2, -240), 1],
  ["shownFraction Mon 00 at -240", () => shownFraction(fixture(), 1, 0, -240), 0],
  ["shownFraction Wed 08 at -240 partial", () => shownFraction(fixture(), 3, 8, -240), 0.25],
  ["shownFraction Fri 06 at +30 half", () => shownFraction(fixture(), 5, 6, 30), 0.5],
  ["shownFraction Mon 06 at +345 weighted", () => shownFraction(fixture(), 1, 6, 345), 0.25],
  ["shownFraction Sat 12 at +330 weighted", () => shownFraction(fixture(), 6, 12, 330), 0.25],
];

function check([name, fn, want]) {
  let got;
  try {
    got = fn();
  } catch (e) {
    got = "threw " + e.message;
  }
  return got === want ? null : `${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`;
}

function storedZone() {
  try {
    return localStorage.getItem("modelpick.tz");
  } catch {
    return null;
  }
}

// run returns "PASS n" or "FAIL <case>; <case>…". The test shares the app's
// localStorage: a stored zone is put back, and none stored stays none.
export function run() {
  const before = storedZone();
  setZone("utc");
  const failed = cases.map(check).filter(Boolean);
  if (before !== null) {
    setZone(before);
  } else {
    try {
      localStorage.removeItem("modelpick.tz");
    } catch {
      // no storage: nothing was written either
    }
  }
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${cases.length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
