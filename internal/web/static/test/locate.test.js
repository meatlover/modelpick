// Cases for js/locate.js: where the server finds the choice an op addresses
// (findChoice and findGroup in internal/app). In the browser,
// locate.test.html writes the verdict into <output id="result">; elsewhere
// run() resolves it.
import { locateChoice } from "../js/locate.js";

const ch = (account, extra = {}) => ({ account, model: "m", thinking: null, cap: null,
  skip_during: [], allow_during: [], group: 0, load_balance: null, ...extra });
const mem = (account, g, extra = {}) => ch(account, { group: g, load_balance: "random", ...extra });
const grp = (...members) => ({ load_balance: "random", choices: members });

const a = ch("a"), b = ch("b"), c = ch("c");

const cases = [
  ["at its index", () => locateChoice([a, b, c], 1, b, null), 1],
  ["moved: the one identical choice", () => locateChoice([b, a, c], 1, ch("a"), null), 1],
  ["moved up from index 2", () => locateChoice([c, a, b], 2, ch("c"), null), 0],
  ["gone", () => locateChoice([a, c], 1, b, null), -1],
  ["two identical, neither at the index", () => locateChoice([b, a, b], 1, ch("b"), null), -1],
  ["two identical, one at the index", () => locateChoice([b, a, b], 2, ch("b"), null), 2],
  ["index past the end, found elsewhere", () => locateChoice([a, b], 5, ch("b"), null), 1],
  ["null and [] schedules alike", () => locateChoice([ch("a", { skip_during: null })], 0, a, null), 0],
  ["caps differ", () => locateChoice([ch("a", { cap: [90] })], 0, ch("a", { cap: [80] }), null), -1],
  ["uncapped is not []", () => locateChoice([ch("a", { cap: [] })], 0, a, null), -1],
  ["trailing default ignored", () => locateChoice([ch("a", { cap: [90] })], 0, ch("a", { cap: [90, "default"] }), null), 0],
  ["thinking differs", () => locateChoice([ch("a", { thinking: "high" })], 0, a, null), -1],
  ["a plain card never finds a member", () => locateChoice([mem("a", 1), mem("b", 1)], 0, ch("a"), null), -1],
  ["a member without member_of: only its index",
    () => locateChoice([b, mem("a", 1), mem("c", 1)], 0, mem("a", 1), null), -1],
  ["a member inside its group",
    () => locateChoice([a, mem("b", 1), mem("c", 1)], 1, mem("b", 1), grp(mem("b", 1), mem("c", 1))), 1],
  ["a member whose group moved",
    () => locateChoice([mem("b", 1), mem("c", 1), a], 1, mem("c", 1), grp(mem("b", 1), mem("c", 1))), 1],
  ["a member whose group moved, addressed at the old start",
    () => locateChoice([a, b, mem("b", 1), mem("c", 1)], 1, mem("b", 1), grp(mem("b", 1), mem("c", 1))), 2],
  ["a member whose group changed",
    () => locateChoice([mem("b", 1), mem("a", 1)], 0, mem("b", 1), grp(mem("b", 1), mem("c", 1))), -1],
  ["a member never found outside its group",
    () => locateChoice([mem("x", 1), mem("y", 1), mem("b", 2), mem("c", 2)], 0, mem("b", 1), grp(mem("x", 1), mem("y", 1))), -1],
  ["the algorithm is part of a member",
    () => locateChoice([mem("b", 1, { load_balance: "other" }), mem("c", 1, { load_balance: "other" })], 0, mem("b", 1),
      { load_balance: "other", choices: [mem("b", 1, { load_balance: "other" }), mem("c", 1, { load_balance: "other" })] }), -1],
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

// run returns "PASS n" or "FAIL <case>; <case>…".
export function run() {
  const failed = cases.map(check).filter(Boolean);
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${cases.length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
