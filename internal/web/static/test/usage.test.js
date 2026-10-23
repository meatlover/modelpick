// Cases for the state chips js/usage.js shows (owner, 2026-10-23: no notes under a row). In the browser,
// usage.test.html writes the verdict into <output id="result">.
import { store } from "../js/api.js";
import { render } from "../js/usage.js";

function row(account, vendor, state, age) {
  return { account, vendor, state, reading_age_seconds: age, can_refresh: false, windows: [] };
}

const rows = [
  row("claude-x", "claude", "expired token", null),
  row("codex-x", "codex", "expired token", null),
  row("go-x", "opencode-go", "expired token", 60),
  row("fresh", "codex", "no reading yet", null),
  row("read", "claude", "ok", 60),
];

// want is each account's state chip; no row has a note.
const want = { "claude-x": "expired token", "codex-x": "expired token", "go-x": "expired token", fresh: "no reading yet", read: "ok" };

function chips(root) {
  const out = {};
  for (const r of root.querySelectorAll(".u-row")) {
    out[r.querySelector(".u-name").textContent] = r.querySelector(".chip").textContent;
  }
  return out;
}

// run returns "PASS n" or "FAIL <case>; <case>…".
export function run() {
  location.hash = "#usage";
  store.set({
    state: { accounts: rows.map((r) => ({ name: r.account })), read_only: "" },
    usage: rows,
    doctor: { errors: 0, warnings: 0, findings: [] },
  });
  const root = document.createElement("main");
  document.body.append(root);
  render(root);
  const got = chips(root);
  const failed = Object.keys(want).filter((k) => got[k] !== want[k])
    .map((k) => `${k}: chip ${JSON.stringify(got[k])}, want ${JSON.stringify(want[k])}`);
  if (root.querySelector(".u-note")) failed.push("a row still has a note");
  root.remove();
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${Object.keys(want).length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
