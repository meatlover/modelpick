// Cases for js/time.js. In the browser, time.test.html writes the verdict
// into <output id="result">; elsewhere run() returns it.
import { setZone, fmtLeft, fmtReset, fmtAge } from "../js/time.js";

const NOW = "2026-10-23T07:10:00Z";

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
