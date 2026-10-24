// The Accounts tab in a real browser against the live back end. Every test
// starts its own server, acts the way a user does and then reads config.json.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
const { expectConfig, expectUnchanged, readConfig } = require("./config");

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

const FIRST = "Remove account go-2 and the choices that use it?";
const LINE = "tester: choice 2 (opencode-go/deepseek-v4.1)";

async function openRemoveGo2(page) {
  const btn = page.getByRole("button", { name: "Remove account go-2" });
  await btn.click();
  const dlg = page.getByRole("dialog", { name: FIRST });
  await expect(dlg).toBeVisible();
  await expect(dlg).toContainText(LINE);
  await expect(dlg.getByRole("button", { name: "Remove account and 1 choice", exact: true })).toBeVisible();
  return { btn, dlg };
}

test("removing a used account asks first; Cancel changes nothing", async ({ page }) => {
  const before = fs.readFileSync(server.configPath, "utf8");
  const { btn, dlg } = await openRemoveGo2(page);
  await dlg.getByRole("button", { name: "Cancel" }).click();
  await expect(dlg).toHaveCount(0);
  await expect(btn).toBeFocused();
  await expect(rows(page).filter({ hasText: "go-2" })).toHaveCount(1);
  expectUnchanged(server.configPath, before);
  await expectConfig(server.configPath, (c) => Object.keys(c.accounts).includes("go-2"), true, "go-2 stays");
  await expectConfig(server.configPath, (c) => c.profiles.tester.length, 3, "tester is unchanged");
});

test("Escape cancels the remove dialog", async ({ page }) => {
  const before = fs.readFileSync(server.configPath, "utf8");
  const { btn, dlg } = await openRemoveGo2(page);
  await page.keyboard.press("Escape");
  await expect(dlg).toHaveCount(0);
  await expect(btn).toBeFocused();
  expectUnchanged(server.configPath, before);
});

test("confirming removes the account and its choices", async ({ page }) => {
  const { dlg } = await openRemoveGo2(page);
  const tester = readConfig(server.configPath).profiles.tester;
  await dlg.getByRole("button", { name: "Remove account and 1 choice", exact: true }).click();
  await expectConfig(server.configPath, (c) => Object.keys(c.accounts).sort(),
    ["claude-main", "claude-spare", "codex-main", "go-1"], "only go-2 is gone");
  await expectConfig(server.configPath, (c) => c.profiles.tester, [tester[0], tester[2]],
    "tester lost only its second choice");
  await expect(rows(page).filter({ hasText: "go-2" })).toHaveCount(0);
});
