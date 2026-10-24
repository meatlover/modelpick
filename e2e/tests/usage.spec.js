// The Usage tab in a real browser against the live back end.
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");

let server;

test.beforeEach(async ({ page }) => {
  server = await startServer("live");
  await login(page, server.url);
  await expect(page.getByText("codex-main", { exact: true })).toBeVisible();
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

// row is an account's Usage row: the grid row holding its name.
const row = (page, name) =>
  page.locator(".u-row").filter({ has: page.getByText(name, { exact: true }) });

test("refresh one account updates its row", async ({ page }) => {
  // codex-main's stored reading is 20 minutes old.
  await expect(row(page, "codex-main").locator(".u-age")).toHaveText("20 mins ago");
  await row(page, "codex-main").getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(row(page, "codex-main").locator(".u-age")).toHaveText("just now");
  await expect(row(page, "go-1").locator(".u-age")).toHaveText("1 min ago");
});

test("a failed refresh shows in the row", async ({ page }) => {
  await server.send("fail codex-main boom");
  await row(page, "codex-main").getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(row(page, "codex-main").getByText("error: boom")).toBeVisible();
  await expect(row(page, "go-1").getByText("error: boom")).toHaveCount(0);
});

test("Refresh all re-reads the doctor once, after the batch", async ({ page }) => {
  await server.send("fail codex-main boom");
  let doctorReads = 0;
  page.on("request", (r) => {
    if (r.method() === "GET" && new URL(r.url()).pathname === "/api/doctor") doctorReads++;
  });
  const all = page.locator(".u-head").getByRole("button");
  await expect(all).toHaveText("Refresh all");
  await all.click();
  await expect(row(page, "codex-main").getByText("error: boom")).toBeVisible();
  // The failed reading reaches the status without a page reload.
  await expect(page.getByRole("button", { name: "1 warning" })).toBeVisible();
  await expect(all).toHaveText("Refresh all");
  expect(doctorReads, "GET /api/doctor during Refresh all").toBe(1);
});
