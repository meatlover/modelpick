// Doctor findings in a real browser: the status panel, the red states they
// set on cards, links and rows, each finding's Go to, and Diagnose.
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

const panel = (page) => page.getByRole("region", { name: "Doctor findings" });
const nav = (page, tab) => page.getByRole("link", { name: tab, exact: true }).click();
const usageRow = (page, name) =>
  page.locator(".u-row").filter({ has: page.getByText(name, { exact: true }) });
const accountRow = (page, name) =>
  page.getByRole("list", { name: "Accounts" }).getByRole("listitem").filter({ hasText: name });

test("a choice error reddens the card and the profile link", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.profiles.coder[0].thinking = "ultra"; });
  await page.reload();
  await nav(page, "Profiles");
  await expect(page.getByRole("group", { name: "Choice 1", exact: true })).toHaveClass(/is-error/);
  await expect(page.getByRole("group", { name: "Choice 2", exact: true })).not.toHaveClass(/is-error/);
  await expect(page.getByRole("link", { name: "coder", exact: true })).toHaveClass(/is-error/);
  await expect(page.getByRole("link", { name: "tester", exact: true })).not.toHaveClass(/is-error/);
  await expect(page.getByRole("button", { name: "1 error" })).toBeVisible();
});

test("a missing tool shows once and reddens each account", async ({ page }) => {
  await server.send("tool opencode-go missing");
  await login(page, server.url);
  await page.getByRole("button", { name: "1 error" }).click();
  await expect(panel(page).getByRole("listitem")).toHaveCount(1);
  await expect(panel(page).getByRole("listitem")).toContainText("go-1, go-2: opencode not found on PATH");
  for (const name of ["go-1", "go-2"]) await expect(usageRow(page, name)).toHaveClass(/is-error/);
  for (const name of ["claude-main", "codex-main"]) await expect(usageRow(page, name)).not.toHaveClass(/is-error/);
  await nav(page, "Accounts");
  for (const name of ["go-1", "go-2"]) await expect(accountRow(page, name)).toHaveClass(/is-error/);
  for (const name of ["claude-main", "codex-main"]) await expect(accountRow(page, name)).not.toHaveClass(/is-error/);
});

test("a stored failed reading shows", async ({ page }) => {
  await login(page, server.url);
  await server.send("fail codex-main boom");
  await usageRow(page, "codex-main").getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(usageRow(page, "codex-main").getByText("error: boom")).toBeVisible();
  // The Refresh re-reads the doctor: no page reload.
  await page.getByRole("button", { name: "1 warning" }).click();
  await expect(panel(page)).toContainText("codex-main: last usage reading failed: boom");
});

const goTo = (page, subject) => panel(page).getByRole("button", { name: `Go to ${subject}`, exact: true });
// flashed: the item lights up, then goes back to normal by itself.
async function flashed(loc) {
  await expect(loc).toHaveClass(/is-flash/);
  await expect(loc).not.toHaveClass(/is-flash/, { timeout: 5000 });
}

test("the panel has a Go to per finding and no other button", async ({ page }) => {
  await server.send("tool opencode-go missing");
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.profiles.coder[0].thinking = "ultra"; });
  await page.reload();
  await page.getByRole("button", { name: "2 errors" }).click();
  await expect(panel(page).getByRole("listitem")).toHaveCount(2);
  for (const item of await panel(page).getByRole("listitem").all()) {
    await expect(item.getByRole("button", { name: /^Go to / })).toHaveCount(1);
  }
  await expect(panel(page).getByRole("button")).toHaveCount(2);
});

test("a finding about the whole file has no Go to", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.version = 2; });
  await page.reload();
  await page.getByRole("button", { name: "1 error" }).click();
  await expect(panel(page).getByRole("listitem")).toHaveCount(1);
  await expect(panel(page).getByRole("listitem")).toContainText("config:");
  await expect(panel(page).getByRole("button", { name: /^Go to / })).toHaveCount(0);
});

test("Go to a choice opens its profile and lights up its card", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.profiles.tester[1].thinking = "ultra"; });
  await page.reload();
  await nav(page, "Profiles"); // on coder, the first profile
  await page.getByRole("button", { name: "1 error" }).click();
  await goTo(page, "profile tester, choice 2").click();
  await expect(page).toHaveURL(/#profiles\/tester$/);
  await expect(panel(page)).toBeHidden();
  const c2 = page.getByRole("group", { name: "Choice 2", exact: true });
  await expect(c2).toBeFocused();
  await flashed(c2);
  await expect(page.getByRole("group", { name: "Choice 1", exact: true })).not.toHaveClass(/is-flash/);
});

test("Go to a grouped account finding lights up each account's row", async ({ page }) => {
  await server.send("tool opencode-go missing");
  await login(page, server.url);
  await page.getByRole("button", { name: "1 error" }).click();
  await goTo(page, "go-1, go-2").click();
  await expect(page).toHaveURL(/#accounts$/);
  await expect(accountRow(page, "go-1")).toHaveClass(/is-flash/);
  await flashed(accountRow(page, "go-2"));
  await expect(accountRow(page, "claude-main")).not.toHaveClass(/is-flash/);
});

test("Go to a schedule opens it and lights up its link", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.schedules.idle = { days: [], utc: [] }; });
  await page.reload();
  await page.getByRole("button", { name: "1 warning" }).click();
  await goTo(page, "schedule idle").click();
  await expect(page).toHaveURL(/#schedules\/idle$/);
  await flashed(page.getByRole("link", { name: "idle", exact: true }));
});

test("the keyboard reaches Go to, and Escape returns to the status", async ({ page }) => {
  await server.send("tool opencode-go missing");
  await login(page, server.url);
  const status = page.getByRole("button", { name: "1 error" });
  const go = goTo(page, "go-1, go-2");
  await expect(status).toHaveAttribute("aria-expanded", "false");
  await status.focus();
  await expect(panel(page)).toBeVisible();
  await expect(status).toHaveAttribute("aria-expanded", "true");
  // Focus moving into the panel keeps it open.
  await page.keyboard.press("Tab");
  await expect(go).toBeFocused();
  await expect(panel(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel(page)).toBeHidden();
  await expect(status).toBeFocused();
  await expect(status).toHaveAttribute("aria-expanded", "false");
});

test("Go to from the keyboard moves the focus to the item", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => { c.profiles.coder[2].thinking = "ultra"; });
  await page.reload();
  await page.getByRole("button", { name: "1 error" }).focus();
  await page.keyboard.press("Tab");
  await expect(goTo(page, "profile coder, choice 3")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("group", { name: "Choice 3", exact: true })).toBeFocused();
  await expect(panel(page)).toBeHidden();
});

// ---------- Diagnose (doctor all), in the header ----------

const diagnose = (page) => page.getByRole("button", { name: /^(Diagnose|Diagnosing…)$/ });

test("Diagnose sits between the time zone switch and the status, and says what it does", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await login(page, server.url);
  const zoneBox = await page.getByRole("group", { name: "Time zone" }).boundingBox();
  const btn = diagnose(page);
  const box = await btn.boundingBox();
  const statusBox = await page.getByRole("button", { name: "config ok" }).boundingBox();
  expect(box.x).toBeGreaterThan(zoneBox.x + zoneBox.width);
  expect(box.x + box.width).toBeLessThan(statusBox.x);
  const mid = (b) => b.y + b.height / 2;
  expect(Math.abs(mid(box) - mid(statusBox))).toBeLessThanOrEqual(1);
  await expect(btn).toHaveAttribute("title", /doctor all/);
  // Not in the status's panel.
  await page.getByRole("button", { name: "config ok" }).click();
  await expect(panel(page).getByRole("button")).toHaveCount(0);
});

test("Diagnose runs doctor all and updates the status", async ({ page }) => {
  await login(page, server.url);
  await expect(page.getByRole("button", { name: "config ok" })).toBeVisible();
  await server.send("fail codex-main boom");
  // The browser holds the request until the test lets it go, so the running
  // state is seen, not raced.
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route("**/api/doctor/all", async (route) => {
    await gate;
    await route.continue();
  });
  const btn = diagnose(page);
  await btn.click();
  await expect(btn).toHaveText("Diagnosing…");
  await expect(btn).toHaveAttribute("aria-disabled", "true");
  release();
  await expect(btn).toHaveText("Diagnose");
  await expect(btn).not.toHaveAttribute("aria-disabled", "true");
  const status = page.getByRole("button", { name: "1 warning" });
  await expect(status).toBeVisible();
  await status.click();
  await expect(panel(page)).toContainText("codex-main: last usage reading failed: boom");
});

test("Diagnose from the keyboard keeps the focus", async ({ page }) => {
  await login(page, server.url);
  await page.getByRole("button", { name: /^Local / }).focus();
  await page.keyboard.press("Tab");
  const btn = diagnose(page);
  await expect(btn).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(btn).toHaveText("Diagnose");
  await expect(btn).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "config ok" })).toBeFocused();
});
