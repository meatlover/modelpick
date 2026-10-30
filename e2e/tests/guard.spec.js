// The guard in helpers.js: an action that comes after an op the test did
// not wait for fails, whatever the timing; through act it does not. If these
// fail, every other spec has lost the guard too.
const { test, expect } = require("@playwright/test");
const { startServer, login, act } = require("./helpers");

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

const removeSpare = (page) => page.getByRole("button", { name: "Remove account claude-spare" }).click();
const name = (page) => page.getByRole("region", { name: "Add account" }).getByLabel("Name");

test("an action after an op the test did not wait for fails, naming the problem", async ({ page }) => {
  await removeSpare(page);
  // Settled by now or not, the next action is refused: the count is the page's.
  await expect(page.getByRole("button", { name: "Remove account claude-spare" })).toHaveCount(0);
  await expect(name(page).click()).rejects.toThrow(/^acted on an unsettled page: click on .*Name.* came after 1 op\(s\)/);
  await expect(page.keyboard.press("Tab")).rejects.toThrow(/^acted on an unsettled page: keyboard\.press\("Tab"\)/);
  await expect(page.mouse.move(1, 1)).rejects.toThrow(/^acted on an unsettled page: mouse\.move\(1\)/);
});

test("an action after act is let through, on a settled page", async ({ page }) => {
  await act(page, () => removeSpare(page));
  await expect(page.locator("body")).toHaveAttribute("data-settled", "true");
  await name(page).click();
  await expect(name(page)).toBeFocused();
});

test("a reload of the page starts the count again", async ({ page }) => {
  await removeSpare(page);
  await expect(page.getByRole("button", { name: "Remove account claude-spare" })).toHaveCount(0);
  await page.reload();
  await name(page).click();
  await expect(name(page)).toBeFocused();
});
