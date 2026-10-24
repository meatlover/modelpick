// Doctor findings in a real browser: the status panel, the red states they
// set on cards, links and rows, and Check all.
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
  await page.reload();
  await page.getByRole("button", { name: "1 warning" }).click();
  await expect(panel(page)).toContainText("codex-main: last usage reading failed: boom");
});

test("Check all updates the list", async ({ page }) => {
  await login(page, server.url);
  await page.getByRole("button", { name: "config ok" }).click();
  await expect(panel(page).getByText("No problems found")).toBeVisible();
  await server.send("fail codex-main boom");
  // The browser holds the request until the test lets it go, so the running
  // state is seen, not raced.
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route("**/api/doctor/all", async (route) => {
    await gate;
    await route.continue();
  });
  const check = panel(page).getByRole("button", { name: /^(Check all|Checking…)$/ });
  await check.click();
  await expect(check).toHaveText("Checking…");
  await expect(check).toBeDisabled();
  release();
  await expect(check).toHaveText("Check all");
  await expect(panel(page)).toContainText("codex-main: last usage reading failed: boom");
  await expect(page.getByRole("button", { name: "1 warning" })).toBeVisible();
  await expect(panel(page).getByText("No problems found")).toBeHidden();
  await expect(check).toBeEnabled();
});
