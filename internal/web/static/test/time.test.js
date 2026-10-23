// Cases for js/time.js. In the browser, time.test.html writes the verdict
// into <output id="result">; elsewhere run() returns it.
import { zone, setZone, fmtLeft, fmtReset, fmtAge } from "../js/time.js";

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

// run returns "PASS n" or "FAIL <case>; <case>…". It restores the zone the
// page had, since the test shares the app's localStorage.
export function run() {
  const before = zone();
  setZone("utc");
  const failed = cases.map(check).filter(Boolean);
  setZone(before);
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${cases.length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
