// Opens every JS unit test page (internal/web/static/test/*.test.html) in
// Chromium against the live server. A page passes when its #result reads
// "PASS <n>"; a FAIL verdict, or a page that never leaves "running" (a
// module that failed to load), fails its test.
const fs = require("node:fs");
const path = require("node:path");
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");

const PAGE_DIR = path.join(__dirname, "..", "..", "internal", "web", "static", "test");
const RESULT_MS = 10000;

const pages = fs
  .readdirSync(PAGE_DIR)
  .filter((f) => f.endsWith(".test.html"))
  .sort();

// verdictOf polls #result until it leaves "running". It reads through a
// locator: page.waitForFunction evaluates a string, which the page's CSP
// (no 'unsafe-eval') refuses.
async function verdictOf(page, name, errors) {
  const result = page.locator("#result");
  const deadline = Date.now() + RESULT_MS;
  for (;;) {
    const text = await result.textContent();
    if (text !== "running") return text;
    if (Date.now() > deadline) {
      throw new Error(`${name}: stuck at running (module error?)\n${errors.join("\n")}`);
    }
    await page.waitForTimeout(50);
  }
}

let server;

test.beforeEach(async () => {
  server = await startServer("live");
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

test("the unit test pages are found", () => {
  expect(pages.length, `no *.test.html in ${PAGE_DIR}`).toBeGreaterThan(0);
});

for (const name of pages) {
  test(name, async ({ page }) => {
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
    await login(page, server.url);
    await page.goto(new URL(`/test/${name}`, server.url).href);
    const verdict = await verdictOf(page, name, errors);
    expect(verdict, name).toMatch(/^PASS \d+$/);
  });
}
