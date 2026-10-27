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
  { ...row("limited", "claude", "ok", 60), windows: [
    { label: "7d", used_pct: 100, resets_at: "2030-01-08T00:00:00Z", scope: null },
    { label: "5h", used_pct: 100, resets_at: "2030-01-02T00:00:00Z", scope: null },
    { label: "Fable", used_pct: 100, resets_at: "2030-01-01T00:00:00Z", scope: "fable" },
    { label: "30d", used_pct: 42, resets_at: "2030-01-30T00:00:00Z", scope: null },
  ] },
  { ...row("scoped", "claude", "ok", 60), windows: [
    { label: "Fable", used_pct: 100, resets_at: "2030-01-01T00:00:00Z", scope: "fable" },
  ] },
  { ...row("limited-error", "claude", "error: stale", 60), windows: [
    { label: "5h", used_pct: 100, resets_at: "2030-01-02T00:00:00Z", scope: null },
  ] },
];

// want is each account's state chip; no row has a note.
const want = { "claude-x": "expired token", "codex-x": "expired token", "go-x": "expired token",
  fresh: "no reading yet", read: "ok", limited: "Limit reached", scoped: "ok",
  "limited-error": "error: stale" };

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
  const wins = [...root.querySelectorAll(".u-row")]
    .find((r) => r.querySelector(".u-name").textContent === "limited").querySelectorAll(".u-win");
  const classes = ["is-full", "is-full is-next-reset", "is-limited", "is-limited"];
  wins.forEach((w, i) => {
    for (const name of classes[i].split(" ")) {
      if (!w.classList.contains(name)) failed.push(`${i}: missing ${name}`);
    }
    if (i !== 1 && w.classList.contains("is-next-reset")) failed.push(`${i}: wrong reset highlighted`);
  });
  const css = getComputedStyle(wins[1].querySelector(".bar-fill")).backgroundColor;
  if (css !== getComputedStyle(wins[1].querySelector(".u-reset")).color) failed.push("full bar and reset differ");
  if (css === getComputedStyle(wins[0].querySelector(".u-reset")).color) failed.push("later full reset is orange");
  if (css === getComputedStyle(wins[2].querySelector(".bar-fill")).backgroundColor) failed.push("other bar is orange");
  const resetStyle = getComputedStyle(wins[1].querySelector(".u-reset"));
  if (resetStyle.textAlign !== "right" || resetStyle.justifySelf !== "end") failed.push("reset not right aligned");
  const scoped = [...root.querySelectorAll(".u-row")]
    .find((r) => r.querySelector(".u-name").textContent === "scoped").querySelector(".u-win");
  if (scoped.classList.contains("is-full") || scoped.classList.contains("is-limited")) failed.push("scoped-only account highlighted");
  root.remove();
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${Object.keys(want).length + classes.length + 5}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
