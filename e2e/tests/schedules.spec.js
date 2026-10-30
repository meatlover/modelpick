// The Schedules tab in a real browser against the live back end. Every test
// starts its own server (so no test depends on another's edits), acts the
// way a user does and then reads config.json from disk.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login, act } = require("./helpers");
const { readConfig, expectConfig, expectUnchanged } = require("./config");

let server;

test.beforeEach(async ({ page }) => {
  server = await startServer("live");
  await login(page, server.url);
  await page.getByRole("link", { name: "Schedules", exact: true }).click();
  await expect(page.getByRole("link", { name: "deepseek-peak", exact: true })).toHaveAttribute("aria-current", "true");
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

const DAY_NAMES = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

// cellsOf expands a schedule as the file holds it (one rule or a list of
// rules, each days × UTC ranges) into the sorted cells "day hh" it covers,
// so a test is not tied to how the rules are grouped.
function cellsOf(schedule) {
  const rules = Array.isArray(schedule) ? schedule : [schedule];
  const cells = new Set();
  for (const rule of rules) {
    for (const range of rule.utc) {
      const [from, to] = range.split("-").map((t) => Number(t.split(":")[0]));
      for (const day of rule.days) {
        for (let hour = from; hour < to; hour++) cells.add(`${day} ${String(hour).padStart(2, "0")}`);
      }
    }
  }
  return [...cells].sort();
}

const NIGHT = ["sat 22", "sun 22"];

const grid = (page, name) => page.getByRole("group", { name: `schedule of ${name}` });
const cellAt = (page, name, day, hour) => grid(page, name).locator(`[data-day="${day}"][data-hour="${hour}"]`);
const pick = (page, name) => page.getByRole("link", { name, exact: true }).click();

// cellsIn is a poll target: the cells schedule name covers in the file.
const cellsIn = (name) => (cfg) => cellsOf(cfg.schedules[name]);

test("create a schedule", async ({ page }) => {
  await page.getByRole("button", { name: "+ New schedule" }).click();
  await page.getByLabel("New schedule name").fill("weekend");
  await page.getByLabel("New schedule name").press("Enter");
  await expectConfig(server.configPath, (c) => c.schedules.weekend, { days: [], utc: [] }, "an empty schedule");
  await expect(page.getByRole("link", { name: "weekend", exact: true })).toHaveAttribute("aria-current", "true");
  await expectConfig(server.configPath, (c) => Object.keys(c.schedules).sort(),
    ["deepseek-peak", "night", "weekend"], "the others are kept");
});

test("paint and erase a cell", async ({ page }) => {
  await pick(page, "night");
  // The grid takes no click while a paint is pending: act waits for it.
  await act(page, () => cellAt(page, "night", 1, 10).click());
  await expectConfig(server.configPath, cellsIn("night"), ["mon 10", ...NIGHT], "Mon 10:00 UTC is on");
  await expect(page.getByText("Mon 10:00–11:00")).toBeVisible();
  await expect(cellAt(page, "night", 1, 10)).not.toHaveClass(/is-preview/);
  await cellAt(page, "night", 1, 10).click();
  await expectConfig(server.configPath, cellsIn("night"), NIGHT, "Mon 10:00 UTC is off again");
});

// dragCells presses the first cell and releases on the second.
async function dragCells(page, from, to) {
  const a = await from.boundingBox();
  const b = await to.boundingBox();
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 8 });
  await page.mouse.up();
}

test("drag a range", async ({ page }) => {
  await pick(page, "night");
  await dragCells(page, cellAt(page, "night", 2, 8), cellAt(page, "night", 4, 11));
  const painted = [];
  for (const day of ["tue", "wed", "thu"]) for (const hh of ["08", "09", "10", "11"]) painted.push(`${day} ${hh}`);
  await expectConfig(server.configPath, cellsIn("night"), [...painted, ...NIGHT].sort(), "3 days by 4 hours");
});

test("Clear", async ({ page }) => {
  const before = cellsOf(readConfig(server.configPath).schedules["deepseek-peak"]);
  expect(before.length).toBeGreaterThan(0);
  await page.getByRole("button", { name: "Clear schedule deepseek-peak" }).click();
  await expectConfig(server.configPath, cellsIn("deepseek-peak"), [], "no cell is on");
  await expect(page.getByRole("button", { name: "Clear schedule deepseek-peak" })).toBeDisabled();
  await expectConfig(server.configPath, cellsIn("night"), NIGHT, "the other schedule is kept");
});

test("Local zone stores UTC", async ({ page }) => {
  await pick(page, "night");
  await page.getByRole("button", { name: /^Local \(/ }).click();
  await expect(page.getByRole("button", { name: /^Local \(/ })).toHaveAttribute("aria-pressed", "true");
  // America/Caracas is UTC-4: Saturday 22:00 there is Sunday 02:00 UTC.
  await cellAt(page, "night", 6, 22).click();
  await expect(page.getByText("Sat 22:00–23:00")).toBeVisible();
  await expectConfig(server.configPath, cellsIn("night"), ["sat 22", "sun 02", "sun 22"], "stored as Sun 02:00 UTC");
  const rules = readConfig(server.configPath).schedules.night;
  const list = Array.isArray(rules) ? rules : [rules];
  expect(list.some((r) => r.days.join() === "sun" && r.utc.includes("02:00-03:00"))).toBe(true);
});

test("delete an unused schedule", async ({ page }) => {
  const row = page.getByRole("link", { name: "night", exact: true });
  await row.hover();
  await page.getByRole("button", { name: "Delete schedule night" }).click();
  await expectConfig(server.configPath, (c) => Object.keys(c.schedules), ["deepseek-peak"], "night is gone");
  await expect(row).toHaveCount(0);
});

test("deleting a used schedule is refused", async ({ page }) => {
  const before = fs.readFileSync(server.configPath, "utf8");
  await page.getByRole("link", { name: "deepseek-peak", exact: true }).hover();
  // The first remove is refused (in use); the dialog asks after it.
  await act(page, () => page.getByRole("button", { name: "Delete schedule deepseek-peak" }).click());
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText(/Schedule deepseek-peak is used by .+/);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("link", { name: "deepseek-peak", exact: true })).toBeVisible();
  expectUnchanged(server.configPath, before);
  expect(readConfig(server.configPath).profiles.tester[0].skip_during).toEqual(["deepseek-peak"]);
});
