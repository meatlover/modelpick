// Cases for js/goto.js: where a doctor finding's Go to leads (the tab and
// the subjects of the items to show). In the browser, goto.test.html
// writes the verdict into <output id="result">; elsewhere run() resolves it.
import { findingTarget } from "../js/goto.js";

const t = (f) => {
  const r = findingTarget(f);
  return r ? `${r.hash} ${r.subjects.join("|")}` : "none";
};

const cases = [
  ["an account", () => t({ subject: "go-1" }), "#accounts go-1"],
  ["accounts grouped into one finding", () => t({ subject: "go-1, go-2", accounts: ["go-1", "go-2"] }),
    "#accounts go-1|go-2"],
  ["a choice", () => t({ subject: "profile coder, choice 3" }), "#profiles/coder profile coder, choice 3"],
  ["a member", () => t({ subject: "profile coder, choice 2b" }), "#profiles/coder profile coder, choice 2b"],
  ["a profile", () => t({ subject: "profile tester" }), "#profiles/tester profile tester"],
  ["a profile name is encoded", () => t({ subject: "profile a b/c, choice 1" }),
    "#profiles/a%20b%2Fc profile a b/c, choice 1"],
  ["a schedule", () => t({ subject: "schedule night" }), "#schedules/night schedule night"],
  ["a schedule name is encoded", () => t({ subject: "schedule peak hours" }), "#schedules/peak%20hours schedule peak hours"],
  ["the whole file has no item", () => t({ subject: "config" }), "none"],
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
