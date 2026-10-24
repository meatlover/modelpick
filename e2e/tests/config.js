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

module.exports = { readConfig, expectConfig, expectUnchanged, handEdit };
