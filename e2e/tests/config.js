// Reading the config file the live e2e server edits, so a test asserts what
// is on disk and not only what the page shows.
const fs = require("node:fs");
const { expect } = require("@playwright/test");

const POLL_MS = 5000;

// readConfig parses the config file at configPath.
function readConfig(configPath) {
  return JSON.parse(fs.readFileSync(configPath, "utf8"));
}

// expectConfig polls until pick(parsed config) equals want (deep equality).
// The page sends an op and the file changes a moment later, so a one-shot
// read would race it.
async function expectConfig(configPath, pick, want, message) {
  await expect
    .poll(() => pick(readConfig(configPath)), { message, timeout: POLL_MS })
    .toEqual(want);
}

// expectUnchanged asserts the file's text is byte-for-byte what it was.
function expectUnchanged(configPath, before) {
  expect(fs.readFileSync(configPath, "utf8")).toBe(before);
}

// handEdit rewrites the file the way a user's editor would: mutate gets the
// parsed config and changes it in place.
function handEdit(configPath, mutate) {
  const cfg = readConfig(configPath);
  mutate(cfg);
  fs.writeFileSync(configPath, JSON.stringify(cfg, null, 2) + "\n");
}

// loadBalanceGo is a handEdit mutation: coder's go-1 choice and a copy of it
// on go-2 become one load balancing group at position 2, ahead of codex-main.
// Coder then reads 1 opus, 2a go-1, 2b go-2, 3 codex-main, 4 sonnet.
function loadBalanceGo(cfg) {
  const [opus, codex, go1, sonnet] = cfg.profiles.coder;
  const go2 = { ...go1, account: "go-2" };
  cfg.profiles.coder = [opus, { load_balance: "random", choices: [go1, go2] }, codex, sonnet];
}

module.exports = { readConfig, expectConfig, expectUnchanged, handEdit, loadBalanceGo };
