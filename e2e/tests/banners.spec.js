// The conflict strip and the read-only banner in a real browser.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
const { readConfig, expectConfig, expectUnchanged, handEdit } = require("./config");

let server;

test.beforeEach(async ({ page }) => {
  server = await startServer("live");
  await login(page, server.url);
  await page.getByRole("link", { name: "Accounts", exact: true }).click();
  await expect(page.getByRole("list", { name: "Accounts" })).toBeVisible();
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

test("hand edit while stale shows the conflict strip", async ({ page }) => {
  handEdit(server.configPath, (c) => { c.settings.default_cap = 80; });
  await page.getByRole("button", { name: "Remove account claude-spare" }).click();
  const strip = page.getByRole("alertdialog", { name: "config.json changed outside the UI" });
  await expect(strip).toBeVisible();
  // Nothing is written until the user chooses.
  expect(readConfig(server.configPath).accounts["claude-spare"]).toBeDefined();
  await strip.getByRole("button", { name: "Apply my change on top" }).click();
  await expect(strip).toHaveCount(0);
  await expectConfig(server.configPath, (c) => c.accounts["claude-spare"], undefined, "my removal is applied");
  await expectConfig(server.configPath, (c) => c.settings.default_cap, 80, "the hand edit is kept");
});

test("broken file is read-only, fixing it clears the banner", async ({ page }) => {
  const good = fs.readFileSync(server.configPath, "utf8");
  const broken = "{ not json";
  fs.writeFileSync(server.configPath, broken);
  await page.reload();
  await expect(page.getByRole("alert").filter({ hasText: "Cannot edit — " })).toBeVisible();
  // Every control is inert, so no edit can be sent; the file stays as it was.
  const add = page.getByRole("region", { name: "Add account" });
  await expect(add.getByRole("button", { name: "Add", exact: true })).toHaveJSProperty("inert", true);
  await expect(add.getByLabel("Name")).toHaveJSProperty("inert", true);
  await expect(page.getByRole("list", { name: "Accounts" }).getByRole("listitem")).toHaveCount(0);
  expectUnchanged(server.configPath, broken);
  fs.writeFileSync(server.configPath, good);
  await page.reload();
  await expect(page.getByRole("list", { name: "Accounts" }).getByRole("listitem")).toHaveCount(5);
  await expect(page.getByText("Cannot edit — ")).toHaveCount(0);
  await expect(add.getByRole("button", { name: "Add", exact: true })).toHaveJSProperty("inert", false);
});
