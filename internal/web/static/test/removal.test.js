// Cases for js/removal.js: what removing an account takes with it, as the
// confirm dialog lists it. subjects.test.js explains the harness.
import { removalPlan } from "../js/removal.js";

const ch = (account, model) => ({ account, model });
const profiles = [
  { name: "tester", choices: [ch("go-1", "m1"), ch("go-2", "m2"), ch("claude", "m3")] },
  { name: "coder", choices: [ch("go-2", "m4"), ch("go-1", "m5")] },
  { name: "solo", choices: [ch("go-2", "m6")] },
];

const cases = [
  ["unused account has no choices", () => removalPlan(profiles, "nope").count, 0],
  ["count is every choice", () => removalPlan(profiles, "go-2").count, 3],
  ["lines are sorted by profile, 1-based",
    () => removalPlan(profiles, "go-2").lines.join("|"),
    "coder: choice 1 (m4)|solo: choice 1 (m6)|solo has no other choice and is removed too|tester: choice 2 (m2)"],
  ["one choice, singular label", () => removalPlan(profiles, "claude").label, "Remove account and 1 choice"],
  ["many choices, plural label", () => removalPlan(profiles, "go-2").label, "Remove account and 3 choices"],
  ["title", () => removalPlan(profiles, "go-2").title, "Remove account go-2 and the choices that use it?"],
  ["a profile with other choices is not removed",
    () => removalPlan(profiles, "go-1").lines.some((l) => l.includes("removed too")), false],
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
