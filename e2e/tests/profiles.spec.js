// The Profiles tab in a real browser against the live back end. Every test
// starts its own server (so no test depends on another's edits), acts the
// way a user does and then reads config.json from disk.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
const { readConfig, expectConfig, expectUnchanged } = require("./config");

let server;

test.beforeEach(async ({ page }) => {
  server = await startServer("live");
  await login(page, server.url);
  await page.getByRole("link", { name: "Profiles", exact: true }).click();
  await expect(page.getByRole("link", { name: "coder", exact: true })).toHaveAttribute("aria-current", "true");
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

const card = (page, n) => page.getByRole("group", { name: `Choice ${n}`, exact: true });
const coder = (cfg) => cfg.profiles.coder;
const accountsOf = (cfg) => coder(cfg).map((c) => `${c.account}/${c.model}`);

const ORIGINAL = [
  "claude-main/claude-opus-5-5",
  "codex-main/gpt-6.1-sol",
  "go-1/opencode-go/deepseek-v4.1",
  "claude-main/claude-sonnet-5-5",
];

test("add and remove a choice", async ({ page }) => {
  await page.getByRole("button", { name: "+ Add Model" }).click();
  // The last choice is the uncapped fallback, so a new choice goes before it.
  await expectConfig(server.configPath, (c) => coder(c).length, 5, "a fifth choice is written");
  await expectConfig(server.configPath, (c) => coder(c)[4].model, "claude-sonnet-5-5", "the fallback stays last");
  await expect(card(page, 5)).toBeVisible();
  await card(page, 4).getByRole("button", { name: "Remove choice 4" }).click();
  await expectConfig(server.configPath, (c) => coder(c).length, 4, "the new choice is removed");
  await expect(card(page, 5)).toHaveCount(0);
  await expectConfig(server.configPath, accountsOf, ORIGINAL, "the other four are untouched");
});

test("change account, model and thinking", async ({ page }) => {
  const c4 = card(page, 4);
  await c4.getByLabel("Account").selectOption("claude-spare");
  await expectConfig(server.configPath, (c) => coder(c)[3].account, "claude-spare", "account");
  await c4.getByLabel("Model").selectOption("claude-fable-5-1");
  await expectConfig(server.configPath, (c) => coder(c)[3].model, "claude-fable-5-1", "model");
  await c4.getByLabel("Thinking").selectOption("low");
  await expectConfig(
    server.configPath,
    (c) => [coder(c)[3].account, coder(c)[3].model, coder(c)[3].thinking],
    ["claude-spare", "claude-fable-5-1", "low"],
    "the choice has all three",
  );
  await expect(c4.getByLabel("Account")).toHaveValue("claude-spare");
});

// capBox is the closed box (or, once focused, the open frame's number box)
// of a card's first window.
const capBox = (card_) => card_.getByRole("textbox", { name: /cap$/ }).first();

test("cap by typing, fixed and pace", async ({ page }) => {
  const c3 = card(page, 3);
  await capBox(c3).click();
  await capBox(c3).fill("90");
  await capBox(c3).press("Enter");
  await expectConfig(server.configPath, (c) => coder(c)[2].cap, [90, 95, 98], "fixed 90 in the first window");
  await capBox(c3).click();
  await c3.getByRole("button", { name: "Time elapsed + N" }).click();
  await capBox(c3).fill("10");
  await capBox(c3).press("Enter");
  await expectConfig(server.configPath, (c) => coder(c)[2].cap, ["pace+10", 95, 98], "pace+10");
  await expect(c3.getByText("Cap at time elapsed +")).toBeVisible();
  await expect(capBox(c3)).toHaveValue("10");
});

// dragRange presses the open frame's slider at fromFrac of its width and
// releases it at toFrac.
async function dragRange(page, card_, fromFrac, toFrac) {
  const box = await card_.getByRole("slider").boundingBox();
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width * fromFrac, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * ((fromFrac + toFrac) / 2), y, { steps: 5 });
  await page.mouse.move(box.x + box.width * toFrac, y, { steps: 5 });
  await page.mouse.up();
}

// The first cap of the go-1 choice, as the file holds it.
const firstCap = () => coder(readConfig(server.configPath))[2].cap[0];

// A switch from fixed to "Time elapsed + N" shows N = 5 (the smallest).
const PACE_START = 5;

test("cap by drag, fixed and pace", async ({ page }) => {
  const c3 = card(page, 3);
  const start = firstCap();
  expect(start).toBe(95);
  await capBox(c3).click();
  await dragRange(page, c3, 0.95, 0.3);
  await expect.poll(firstCap).not.toBe(start);
  const fixed = firstCap();
  expect(typeof fixed).toBe("number");
  expect(fixed).toBeLessThan(start);
  expect(fixed).toBeGreaterThanOrEqual(0);
  expect(coder(readConfig(server.configPath))[2].cap.slice(1)).toEqual([95, 98]);
  await capBox(c3).click();
  await c3.getByRole("button", { name: "Time elapsed + N" }).click();
  await dragRange(page, c3, 0.02, 0.8);
  await expect.poll(() => String(firstCap())).toMatch(/^pace\+\d+$/);
  const n = Number(String(firstCap()).slice(5));
  expect(n).toBeGreaterThan(PACE_START);
  expect(n).toBeLessThanOrEqual(50);
});

test('trash on a box writes "default"', async ({ page }) => {
  const c1 = card(page, 1);
  await capBox(c1).click();
  await c1.getByRole("button", { name: /^Use default cap for / }).click();
  await expectConfig(server.configPath, (c) => coder(c)[0].cap, ["default", 98], "the first box is default");
  await expect(capBox(c1)).toHaveValue("95%");
});

// dragCard presses card from's handle and drops it on the lower part of card
// to, in steps so the browser sees the pointer travel (a real drag). The
// "Drop here" slot shifts the cards while the pointer moves, so the drop
// point is read again once the pointer is over the target.
async function dragCard(page, from, to) {
  // All four cards must be inside the viewport: a pointer outside it sends
  // no drag events.
  await page.setViewportSize({ width: 1280, height: 1400 });
  const handle = await card(page, from).locator(".p-handle").boundingBox();
  const hx = handle.x + handle.width / 2;
  const hy = handle.y + handle.height / 2;
  await page.mouse.move(hx, hy);
  await page.mouse.down();
  await page.mouse.move(hx, hy + 12, { steps: 4 });
  const near = await card(page, to).boundingBox();
  await page.mouse.move(near.x + 40, near.y + near.height / 2, { steps: 12 });
  const now = await card(page, to).boundingBox();
  await page.mouse.move(now.x + 40, now.y + now.height - 8, { steps: 4 });
  await page.mouse.up();
}

test("reorder by mouse drag", async ({ page }) => {
  await dragCard(page, 1, 3);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[1], ORIGINAL[2], ORIGINAL[0], ORIGINAL[3]],
    "the first choice is dropped after the third",
  );
  await expect(card(page, 3)).toBeFocused();
});

test("reorder by keyboard", async ({ page }) => {
  const mod = (await page.evaluate(() => navigator.platform)).startsWith("Mac") ? "Meta" : "Control";
  await card(page, 2).focus();
  await page.keyboard.press(`${mod}+ArrowDown`);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[0], ORIGINAL[2], ORIGINAL[1], ORIGINAL[3]],
    "the second choice moved down one",
  );
  await expect(card(page, 3)).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[0], ORIGINAL[1], ORIGINAL[2], ORIGINAL[3]],
    "moved back up one",
  );
  await expect(card(page, 2)).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[1], ORIGINAL[0], ORIGINAL[2], ORIGINAL[3]],
    "moved up to the top",
  );
});

async function attach(page, n, side, name) {
  const c = card(page, n);
  await c.getByRole("button", { name: "+ schedule" }).click();
  await c.getByRole("button", { name: side, exact: true }).click();
  await c.getByRole("button", { name, exact: true }).click();
}

test("attach a skip chip and an allow chip", async ({ page }) => {
  await attach(page, 4, "Skip", "night");
  await expectConfig(server.configPath, (c) => coder(c)[3].skip_during, ["night"], "skip_during");
  await expect(card(page, 4).getByTitle("Skip during night")).toBeVisible();
  await attach(page, 2, "Allow", "night");
  await expectConfig(server.configPath, (c) => coder(c)[1].allow_during, ["night"], "allow_during");
  await expect(card(page, 2).getByTitle("Allow only during night")).toBeVisible();
  await expectConfig(server.configPath, (c) => coder(c)[1].skip_during ?? null, null, "no skip on the allow card");
});

test("global default cap", async ({ page }) => {
  const box = page.getByLabel("Global default cap");
  await box.fill("80");
  await box.press("Enter");
  await expectConfig(server.configPath, (c) => c.settings.default_cap, 80, "default_cap 80");
  await expect(box).toHaveValue("80%");
});

test("global default cap: the page bounds 101, the server refuses it", async ({ page }) => {
  const box = page.getByLabel("Global default cap");
  await box.fill("101");
  await box.press("Enter");
  await expectConfig(server.configPath, (c) => c.settings.default_cap, 100, "the page clamps 101 to 100");
  await expect(box).toHaveValue("100%");
  const before = fs.readFileSync(server.configPath, "utf8");
  const state = await (await page.request.get(new URL("/api/state", server.url).href)).json();
  const r = await page.request.post(new URL("/api/op", server.url).href, {
    headers: { Origin: new URL(server.url).origin },
    data: { base: state.sha256, op: "settings.default_cap", args: { value: 101 } },
  });
  expect(r.ok()).toBe(false);
  expect((await r.json()).reason).toBe("Global default cap: 101 is outside 0–100%");
  expectUnchanged(server.configPath, before);
});

// This test is its own setup: its page registers the listeners before the
// login, so the first load and the first render of each tab are watched too.
test("no console errors on any tab", async ({ context }) => {
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  page.on("console", (m) => {
    if (m.type() !== "error") return;
    if (/status of (409|422)/.test(m.text())) return;
    errors.push(m.text());
  });
  await login(page, server.url);
  for (const tab of ["Usage", "Profiles", "Schedules", "Accounts"]) {
    await page.getByRole("link", { name: tab, exact: true }).click();
    await expect(page.getByRole("link", { name: tab, exact: true })).toHaveAttribute("aria-current", "page");
    await page.waitForLoadState("networkidle");
  }
  expect(errors).toEqual([]);
});
