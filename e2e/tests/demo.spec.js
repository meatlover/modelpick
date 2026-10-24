// A smoke set against the in-memory demo (cmd/modelpick-ui-demo). The demo
// keeps its config in memory, so every case asserts visible text. Its
// Refresh and Check all pretend to wait 600 ms for the vendor.
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");

let server;

test.beforeEach(async ({ page }) => {
  server = await startServer("demo");
  await login(page, server.url);
  await expect(page.getByText("codex-main", { exact: true })).toBeVisible();
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

const nav = (page, tab) => page.getByRole("link", { name: tab, exact: true }).click();
const panel = (page) => page.getByRole("region", { name: "Doctor findings" });
const usageRow = (page, name) =>
  page.locator(".u-row").filter({ has: page.getByText(name, { exact: true }) });
const accountRows = (page) => page.getByRole("list", { name: "Accounts" }).getByRole("listitem");

test("every tab loads", async ({ page }) => {
  await expect(usageRow(page, "claude-main")).toBeVisible();
  await nav(page, "Profiles");
  await expect(page.getByRole("link", { name: "coder-difficult", exact: true })).toHaveAttribute("aria-current", "true");
  await expect(page.getByLabel("Global default cap")).toHaveValue("95%");
  await nav(page, "Schedules");
  await expect(page.getByRole("link", { name: "deepseek-peak", exact: true })).toHaveAttribute("aria-current", "true");
  await expect(page.getByRole("group", { name: "schedule of deepseek-peak" })).toBeVisible();
  await nav(page, "Accounts");
  await expect(accountRows(page)).toHaveCount(6);
});

test("Profiles: the default cap changes", async ({ page }) => {
  await nav(page, "Profiles");
  const box = page.getByLabel("Global default cap");
  await box.fill("80");
  await box.press("Enter");
  await expect(box).toHaveValue("80%");
  await page.reload();
  await nav(page, "Profiles");
  await expect(page.getByLabel("Global default cap")).toHaveValue("80%");
});

test("Schedules: a new schedule", async ({ page }) => {
  await nav(page, "Schedules");
  await page.getByRole("button", { name: "+ New schedule" }).click();
  await page.getByLabel("New schedule name").fill("weekend");
  await page.getByLabel("New schedule name").press("Enter");
  await expect(page.getByRole("link", { name: "weekend", exact: true })).toHaveAttribute("aria-current", "true");
  await expect(page.getByRole("link", { name: "deepseek-peak", exact: true })).toBeVisible();
});

test("Accounts: add an account", async ({ page }) => {
  await nav(page, "Accounts");
  const add = page.getByRole("region", { name: "Add account" });
  await add.getByLabel("Name").fill("claude-work");
  await add.getByLabel("Vendor").selectOption("claude");
  await add.getByLabel("Config dir").fill("~/.claude-work");
  await add.getByRole("button", { name: "Add", exact: true }).click();
  await expect(accountRows(page).filter({ hasText: "claude-work" })).toHaveCount(1);
  await expect(accountRows(page)).toHaveCount(7);
});

test("refresh shows a new age", async ({ page }) => {
  const age = usageRow(page, "claude-main").locator(".u-age");
  await expect(age).toHaveText("4 mins ago");
  await usageRow(page, "claude-main").getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(age).toHaveText("just now");
});

test("Check all adds the failed reading", async ({ page }) => {
  const failed = "codex-main: last usage reading failed: usage endpoint returned HTTP 503";
  await page.getByRole("button", { name: "1 warning" }).click();
  await expect(panel(page)).not.toContainText(failed);
  // The browser holds the request until the running state is seen; it then
  // reaches the demo, which still waits its 600 ms.
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route("**/api/doctor/all", async (route) => {
    await gate;
    await route.continue();
  });
  const check = panel(page).getByRole("button", { name: /^(Check all|Checking…)$/ });
  await check.click();
  await expect(check).toHaveText("Checking…");
  release();
  await expect(check).toHaveText("Check all");
  await expect(usageRow(page, "codex-main").getByText("error: usage endpoint returned HTTP 503")).toBeVisible();
  await expect(panel(page)).toContainText(failed);
  await expect(page.getByRole("button", { name: "2 warnings" })).toBeVisible();
  // A good Refresh of codex-main clears the row and the warning.
  await usageRow(page, "codex-main").getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(usageRow(page, "codex-main").getByText("error: usage endpoint returned HTTP 503")).toHaveCount(0);
  // The Refresh re-reads the doctor, so the status follows without a reload.
  // (The click on Refresh, outside the panel, closed it.)
  await page.getByRole("button", { name: "1 warning" }).click();
  await expect(panel(page)).toBeVisible();
  await expect(panel(page)).not.toContainText(failed);
});

test("conflict strip after edit", async ({ page }) => {
  await nav(page, "Profiles");
  await server.send("edit");
  const box = page.getByLabel("Global default cap");
  await box.fill("80");
  await box.press("Enter");
  const strip = page.getByRole("alertdialog", { name: "config.json changed outside the UI" });
  await expect(strip).toBeVisible();
  await strip.getByRole("button", { name: "Apply my change on top" }).click();
  await expect(strip).toHaveCount(0);
  await expect(box).toHaveValue("80%");
});

test("read-only after break, normal after fix", async ({ page }) => {
  await server.send("break");
  await page.reload();
  const banner = page.getByRole("alert").filter({ hasText: "Cannot edit — " });
  await expect(banner).toContainText("config.json: line 12: invalid character '}'");
  await server.send("fix");
  await page.reload();
  await expect(page.getByText("Cannot edit — ")).toHaveCount(0);
  await expect(usageRow(page, "claude-main")).toBeVisible();
});
