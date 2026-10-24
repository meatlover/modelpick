// The compact Profiles card (owner, 2026-10-24), measured in a real
// browser: two rows (dropdowns; caps and schedules), the field labels once
// above the list, Remove level with the dropdowns, the cap editor floating
// over the card, and the schedules at the right end of the caps line or,
// when they do not fit there, at the left of their own line.
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
const { handEdit } = require("./config");

let server;

test.beforeEach(async () => {
  server = await startServer("live");
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

async function profiles(page) {
  await login(page, server.url);
  await page.getByRole("link", { name: "Profiles", exact: true }).click();
  await expect(card(page, 1)).toBeVisible();
}

const card = (page, n) => page.getByRole("group", { name: `Choice ${n}`, exact: true });
const rect = (loc) => loc.evaluate((el) => {
  const r = el.getBoundingClientRect();
  return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, width: r.width, height: r.height };
});
const mid = (r) => r.top + r.height / 2;

test("the field labels show once, above the cards", async ({ page }) => {
  await profiles(page);
  const cols = page.locator(".p-cols");
  await expect(cols.getByText("Account", { exact: true })).toBeVisible();
  await expect(cols.getByText("Thinking", { exact: true })).toBeVisible();
  const c1 = card(page, 1);
  // No label line above the dropdowns: they start at the card's padding.
  const top = (await rect(c1)).top;
  expect((await rect(c1.getByLabel("Account"))).top - top).toBeLessThanOrEqual(14);
  // Each column's label sits over its dropdown.
  for (const name of ["Account", "Model", "Thinking"]) {
    const label = await rect(cols.getByText(name, { exact: true }));
    expect(Math.abs(label.left - (await rect(c1.getByLabel(name))).left)).toBeLessThanOrEqual(1);
  }
});

test("Remove sits level with the dropdowns", async ({ page }) => {
  await profiles(page);
  const c1 = card(page, 1);
  const remove = await rect(c1.getByRole("button", { name: "Remove choice 1" }));
  expect(Math.abs(mid(remove) - mid(await rect(c1.getByLabel("Account"))))).toBeLessThanOrEqual(1);
});

test("a card is two rows: dropdowns, then caps and schedules on one line", async ({ page }) => {
  await profiles(page);
  const c2 = card(page, 2);
  const box = await rect(c2.getByRole("textbox", { name: "5h cap" }));
  const add = await rect(c2.getByRole("button", { name: "+ schedule" }));
  const nocaps = await rect(c2.getByRole("button", { name: "No caps" }));
  expect(box.height).toBeLessThanOrEqual(32); // the box's value line, inside a 32 px box
  expect(Math.abs(mid(add) - mid(nocaps))).toBeLessThanOrEqual(1);
  expect((await rect(c2)).height).toBeLessThanOrEqual(110);
});

test("opening a cap editor keeps the card's height; the editor floats over it", async ({ page }) => {
  await profiles(page);
  const c1 = card(page, 1);
  const before = await rect(c1);
  await c1.getByRole("textbox", { name: "5h cap" }).click();
  const frame = c1.locator(".cap-edit");
  await expect(frame).toBeVisible();
  expect((await rect(c1)).height).toBe(before.height);
  const f = await rect(frame);
  expect(f.width).toBe(224);
  expect(f.bottom).toBeGreaterThan(before.bottom); // it reaches past the card
  // On top: the point under the editor's bottom edge belongs to it, not to
  // the card below.
  const onTop = await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y).closest(".cap-edit"),
    [f.left + 20, f.bottom - 6]);
  expect(onTop).toBe(true);
});

test("an editor opened near the window's bottom is scrolled into view", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 640 });
  await profiles(page);
  const box = card(page, 3).getByRole("textbox", { name: "5h cap" });
  await box.scrollIntoViewIfNeeded();
  const r0 = await rect(box);
  // Bring the box to the bottom edge, so its editor would end below it.
  await page.evaluate((b) => window.scrollBy(0, b - innerHeight + 4), r0.bottom);
  await box.click();
  const f = await rect(card(page, 3).locator(".cap-edit"));
  const vh = await page.evaluate(() => innerHeight);
  expect(f.bottom).toBeLessThanOrEqual(vh);
  expect(f.top).toBeGreaterThanOrEqual(0);
});

test("schedules: right end of the caps line, else left of their own line", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.profiles.coder[2].skip_during = ["deepseek-peak", "night"]; });
  for (const [width, ownLine] of [[1600, false], [900, true]]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.reload();
    await page.getByRole("link", { name: "Profiles", exact: true }).click();
    const c3 = card(page, 3);
    const caps = await rect(c3.locator(".p-caps"));
    const chips = await rect(c3.locator(".p-chips"));
    const main = await rect(c3.locator(".p-card-main"));
    if (ownLine) {
      expect(chips.top, `${width}px: below the caps`).toBeGreaterThanOrEqual(caps.bottom);
      expect(Math.abs(chips.left - main.left), `${width}px: at the left`).toBeLessThanOrEqual(1);
    } else {
      expect(Math.abs(chips.top - caps.top), `${width}px: on the caps line`).toBeLessThanOrEqual(1);
      expect(Math.abs(chips.right - main.right), `${width}px: at the right`).toBeLessThanOrEqual(1);
    }
  }
});
