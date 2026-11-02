// Cases for the state chips and window highlights js/usage.js shows (owner,
// 2026-10-23: no notes under a row; 2026-11-02: one highlighted window per
// account). In the browser, usage.test.html writes the verdict into
// <output id="result">.
import { store } from "../js/api.js";
import { render } from "../js/usage.js";

function row(account, vendor, state, age) {
  return { account, vendor, state, reading_age_seconds: age, can_refresh: false, windows: [] };
}

function win(label, used_pct, resets_at, scope = null) {
  return { label, used_pct, resets_at, scope };
}

const rows = [
  row("claude-x", "claude", "expired token", null),
  row("codex-x", "codex", "expired token", null),
  row("go-x", "opencode-go", "expired token", 60),
  row("fresh", "codex", "no reading yet", null),
  row("read", "claude", "ok", 60),
  // Two full general windows: the later reset (7d) is the one shown.
  { ...row("limited", "claude", "ok", 60), windows: [
    win("7d", 100, "2030-01-08T00:00:00Z"),
    win("5h", 100, "2030-01-02T00:00:00Z"),
    win("Fable", 100, "2030-01-01T00:00:00Z", "fable"),
    win("30d", 42, "2030-01-30T00:00:00Z"),
  ] },
  // 95% is near; 94.9% is not, though it resets later.
  { ...row("near", "claude", "ok", 60), windows: [
    win("5h", 94.9, "2030-01-09T00:00:00Z"),
    win("7d", 95, "2030-01-05T00:00:00Z"),
  ] },
  // Red beats orange, even when the orange window resets later.
  { ...row("mixed", "claude", "ok", 60), windows: [
    win("5h", 96, "2030-01-09T00:00:00Z"),
    win("7d", 100, "2030-01-05T00:00:00Z"),
  ] },
  // Same colour and the same reset: the first one.
  { ...row("tie", "claude", "ok", 60), windows: [
    win("5h", 97, "2030-01-05T00:00:00Z"),
    win("7d", 99, "2030-01-05T00:00:00Z"),
  ] },
  // A window without a reset time loses to one with a known reset; with
  // none known, the first.
  { ...row("no-reset", "claude", "ok", 60), windows: [
    win("5h", 100, null),
    win("7d", 100, "2030-01-03T00:00:00Z"),
  ] },
  { ...row("no-reset-all", "claude", "ok", 60), windows: [
    win("5h", 98, null),
    win("7d", 98, ""),
  ] },
  // A model-scoped window never marks the account.
  { ...row("scoped", "claude", "ok", 60), windows: [
    win("Fable", 100, "2030-01-01T00:00:00Z", "fable"),
    win("5h", 20, "2030-01-02T00:00:00Z"),
  ] },
  // Another state keeps its text; the window is still marked.
  { ...row("limited-error", "claude", "error: stale", 60), windows: [
    win("5h", 100, "2030-01-02T00:00:00Z"),
  ] },
];

// want is each account's state chip text and colour class; no row has a note.
const want = {
  "claude-x": ["expired token", "chip-warn"], "codex-x": ["expired token", "chip-warn"],
  "go-x": ["expired token", "chip-warn"], fresh: ["no reading yet", "chip-warn"], read: ["ok", "chip-ok"],
  limited: ["Limit reached", "chip-full"], near: ["Near limit", "chip-warn"], mixed: ["Limit reached", "chip-full"],
  tie: ["Near limit", "chip-warn"], "no-reset": ["Limit reached", "chip-full"],
  "no-reset-all": ["Near limit", "chip-warn"], scoped: ["ok", "chip-ok"],
  "limited-error": ["error: stale", "chip-warn"],
};

// marks is each account's window classes, in window order ("" = unmarked).
const marks = {
  read: [],
  limited: ["is-full", "is-dim", "is-dim", "is-dim"],
  near: ["is-dim", "is-warn"],
  mixed: ["is-dim", "is-full"],
  tie: ["is-warn", "is-dim"],
  "no-reset": ["is-dim", "is-full"],
  "no-reset-all": ["is-warn", "is-dim"],
  scoped: ["", ""],
  "limited-error": ["is-full"],
};
const MARKS = ["is-full", "is-warn", "is-dim"];

function rowOf(root, name) {
  return [...root.querySelectorAll(".u-row")].find((r) => r.querySelector(".u-name").textContent === name);
}

const color = (el, prop = "color") => getComputedStyle(el)[prop];

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
  const failed = [];
  let n = 0;
  for (const [name, [text, cls]] of Object.entries(want)) {
    n++;
    const chip = rowOf(root, name).querySelector(".chip");
    if (chip.textContent !== text) failed.push(`${name}: chip ${JSON.stringify(chip.textContent)}, want ${JSON.stringify(text)}`);
    if (!chip.classList.contains(cls)) failed.push(`${name}: chip ${chip.className}, want ${cls}`);
  }
  if (root.querySelector(".u-note")) failed.push("a row still has a note");
  for (const [name, list] of Object.entries(marks)) {
    n++;
    const got = [...rowOf(root, name).querySelectorAll(".u-win")]
      .map((w) => MARKS.filter((m) => w.classList.contains(m)).join(" "));
    if (JSON.stringify(got) !== JSON.stringify(list)) failed.push(`${name}: windows ${JSON.stringify(got)}, want ${JSON.stringify(list)}`);
  }

  // Colours: red bar = red reset = red chip text; orange bar = orange reset;
  // a dimmed bar is neither, and its reset is not highlighted.
  const [dim, full] = rowOf(root, "mixed").querySelectorAll(".u-win");
  const warn = rowOf(root, "near").querySelectorAll(".u-win")[1];
  const plain = rowOf(root, "scoped").querySelectorAll(".u-win")[1];
  const red = color(full.querySelector(".bar-fill"), "backgroundColor");
  const orange = color(warn.querySelector(".bar-fill"), "backgroundColor");
  const checks = [
    [red !== orange, "red and orange bars are the same colour"],
    [color(full.querySelector(".u-reset")) === red, "full reset is not the bar's red"],
    [color(full.querySelector(".u-left")) === red, "full time left is not the bar's red"],
    [color(warn.querySelector(".u-reset")) === orange, "near reset is not the bar's orange"],
    [color(rowOf(root, "mixed").querySelector(".chip")) === red, "Limit reached chip is not red"],
    [![red, orange].includes(color(dim.querySelector(".bar-fill"), "backgroundColor")), "dimmed bar is coloured"],
    [![red, orange].includes(color(dim.querySelector(".u-reset"))), "dimmed reset is highlighted"],
    [color(dim.querySelector(".u-reset")) !== color(plain.querySelector(".u-reset")), "dimmed reset not dimmed"],
    [color(plain.querySelector(".bar-fill"), "backgroundColor") !== color(dim.querySelector(".bar-fill"), "backgroundColor"),
      "dimmed bar not dimmed"],
  ];
  for (const [ok, msg] of checks) {
    n++;
    if (!ok) failed.push(msg);
  }
  const resetStyle = getComputedStyle(full.querySelector(".u-reset"));
  n++;
  if (resetStyle.textAlign !== "right" || resetStyle.justifySelf !== "end") failed.push("reset not right aligned");
  root.remove();
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${n}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
