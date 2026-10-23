// Cases for the notes js/usage.js shows under a row. In the browser,
// usage.test.html writes the verdict into <output id="result">.
import { store } from "../js/api.js";
import { render } from "../js/usage.js";

const LOGIN = "Open Claude Code with this account to refresh its login.";

function row(account, vendor, state, age) {
  return { account, vendor, state, reading_age_seconds: age, can_refresh: false, windows: [] };
}

const rows = [
  row("claude-x", "claude", "expired token", null),
  row("codex-x", "codex", "expired token", null),
  row("go-x", "opencode-go", "expired token", 60),
  row("fresh", "codex", "ok", null),
  row("read", "claude", "ok", 60),
];

// want is each account's note ("" = none).
const want = { "claude-x": LOGIN, "codex-x": "", "go-x": "", fresh: "no reading yet", read: "" };

function notes(root) {
  const out = {};
  for (const r of root.querySelectorAll(".u-row")) {
    const note = r.querySelector(".u-note");
    out[r.querySelector(".u-name").textContent] = note ? note.textContent : "";
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
  const got = notes(root);
  const failed = Object.keys(want).filter((k) => got[k] !== want[k])
    .map((k) => `${k}: note ${JSON.stringify(got[k])}, want ${JSON.stringify(want[k])}`);
  root.remove();
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${Object.keys(want).length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
