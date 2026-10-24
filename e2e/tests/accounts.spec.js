// The Accounts tab in a real browser against the live back end. Every test
// starts its own server, acts the way a user does and then reads config.json.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
const { expectConfig, expectUnchanged } = require("./config");

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

const rows = (page) => page.getByRole("list", { name: "Accounts" }).getByRole("listitem");

test("add an account", async ({ page }) => {
  const add = page.getByRole("region", { name: "Add account" });
  await add.getByLabel("Name").fill("claude-work");
  await add.getByLabel("Vendor").selectOption("claude");
  await add.getByLabel("Config dir").fill("~/.claude-work");
  await add.getByRole("button", { name: "Add", exact: true }).click();
  await expectConfig(server.configPath, (c) => c.accounts["claude-work"],
    { vendor: "claude", config_dir: "~/.claude-work" }, "the account is in the file");
  await expectConfig(server.configPath, (c) => Object.keys(c.accounts).sort(),
    ["claude-main", "claude-spare", "claude-work", "codex-main", "go-1", "go-2"], "the others are kept");
  await expect(rows(page).filter({ hasText: "claude-work" })).toHaveCount(1);
});

test("remove an unused account", async ({ page }) => {
  await page.getByRole("button", { name: "Remove account claude-spare" }).click();
  await expectConfig(server.configPath, (c) => Object.keys(c.accounts).sort(),
    ["claude-main", "codex-main", "go-1", "go-2"], "only claude-spare is gone");
  await expect(rows(page).filter({ hasText: "claude-spare" })).toHaveCount(0);
});

test("removing a used account is refused", async ({ page }) => {
  const before = fs.readFileSync(server.configPath, "utf8");
  // go-2 is used by the tester profile's second choice only.
  await page.getByRole("button", { name: "Remove account go-2" }).click();
  await expect(page.getByText("Account go-2 is used by profile tester, choice 2")).toBeVisible();
  await expect(rows(page).filter({ hasText: "go-2" })).toHaveCount(1);
  expectUnchanged(server.configPath, before);
});
