// The Profiles tab in a real browser against the live back end. Every test
// starts its own server (so no test depends on another's edits), acts the
// way a user does and then reads config.json from disk.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login, stateGate, holdState } = require("./helpers");
const { readConfig, expectConfig, expectUnchanged, handEdit, loadBalanceGo } = require("./config");

let server;

test.beforeEach(async ({ page }) => {
  server = await startServer("live");
  await login(page, server.url);
  await page.getByRole("link", { name: "Profiles", exact: true }).click();
  await expect(page.getByRole("link", { name: "coder", exact: true })).toHaveAttribute("aria-current", "true");
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

const card = (page, n) => page.getByRole("group", { name: `Choice ${n}`, exact: true });
const coder = (cfg) => cfg.profiles.coder;
const accountsOf = (cfg) => coder(cfg).map((c) => `${c.account}/${c.model}`);

const ORIGINAL = [
  "claude-main/claude-opus-5-5",
  "codex-main/gpt-6.1-sol",
  "go-1/opencode-go/deepseek-v4.1",
  "claude-main/claude-sonnet-5-5",
];

test("add and remove a choice", async ({ page }) => {
  await page.getByRole("button", { name: "+ Add Model" }).click();
  // The last choice is the uncapped fallback, so a new choice goes before it.
  await expectConfig(server.configPath, (c) => coder(c).length, 5, "a fifth choice is written");
  await expectConfig(server.configPath, (c) => coder(c)[4].model, "claude-sonnet-5-5", "the fallback stays last");
  await expect(card(page, 5)).toBeVisible();
  await card(page, 4).getByRole("button", { name: "Remove choice 4" }).click();
  await expectConfig(server.configPath, (c) => coder(c).length, 4, "the new choice is removed");
  await expect(card(page, 5)).toHaveCount(0);
  await expectConfig(server.configPath, accountsOf, ORIGINAL, "the other four are untouched");
});

test("change account, model and thinking", async ({ page }) => {
  const c4 = card(page, 4);
  await c4.getByLabel("Account").selectOption("claude-spare");
  await expectConfig(server.configPath, (c) => coder(c)[3].account, "claude-spare", "account");
  await c4.getByLabel("Model").selectOption("claude-fable-5-1");
  await expectConfig(server.configPath, (c) => coder(c)[3].model, "claude-fable-5-1", "model");
  await c4.getByLabel("Thinking").selectOption("low");
  await expectConfig(
    server.configPath,
    (c) => [coder(c)[3].account, coder(c)[3].model, coder(c)[3].thinking],
    ["claude-spare", "claude-fable-5-1", "low"],
    "the choice has all three",
  );
  await expect(c4.getByLabel("Account")).toHaveValue("claude-spare");
});

// A model with no thinking levels shows a ghost box: dashed, unfilled, no
// arrow, still labelled Thinking. A model with levels keeps the select.
test("no thinking levels shows a ghost box", async ({ page }) => {
  const look = (sel) => sel.evaluate((el) => {
    const s = getComputedStyle(el);
    return { dashed: s.borderTopStyle === "dashed", unfilled: s.backgroundColor === "rgba(0, 0, 0, 0)",
      arrow: s.appearance !== "none", disabled: el.disabled };
  });
  const none = card(page, 3).getByLabel("Thinking");
  await expect(none).toHaveValue("");
  expect(await look(none)).toEqual({ dashed: true, unfilled: true, arrow: false, disabled: true });
  expect(await look(card(page, 4).getByLabel("Thinking")))
    .toEqual({ dashed: false, unfilled: false, arrow: true, disabled: false });
});

// The card is redrawn only when the reload after an edit returns. Held
// back here, so a second edit is made on the card as drawn before the first:
// it must still apply, on the choice as the first edit left it.
test("a second edit before the redraw still applies", async ({ page }) => {
  const c4 = card(page, 4);
  let hold = true;
  await page.route("**/api/state", async (route) => {
    while (hold) await new Promise((ok) => setTimeout(ok, 50));
    await route.continue();
  });
  await c4.getByLabel("Model").selectOption("claude-fable-5-1");
  await expectConfig(server.configPath, (c) => coder(c)[3].model, "claude-fable-5-1", "model");
  await c4.getByLabel("Thinking").selectOption("low");
  hold = false;
  await expectConfig(server.configPath, (c) => coder(c)[3].thinking, "low", "thinking, sent on the stale card");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

// The first edit's redraw comes while the second is in flight: a third edit
// made on that redrawn card is built on what the second left.
test("an edit on a card redrawn while an earlier edit is in flight still applies", async ({ page }) => {
  const c4 = card(page, 4);
  const gate = await stateGate(page);
  await c4.getByLabel("Account").selectOption("claude-spare");
  await expectConfig(server.configPath, (c) => coder(c)[3].account, "claude-spare", "account");
  await c4.getByLabel("Model").selectOption("claude-fable-5-1");
  const drawn = await c4.getByLabel("Thinking").elementHandle();
  gate.pass(1);
  await expectConfig(server.configPath, (c) => coder(c)[3].model, "claude-fable-5-1", "model");
  await expect.poll(() => drawn.evaluate((el) => el.isConnected), { message: "the account's redraw" }).toBe(false);
  await expect.poll(() => gate.held(), { message: "the model's reload, held" }).toBe(1);
  await c4.getByLabel("Thinking").selectOption("low");
  gate.open();
  await expectConfig(server.configPath, (c) => [coder(c)[3].account, coder(c)[3].model, coder(c)[3].thinking],
    ["claude-spare", "claude-fable-5-1", "low"], "all three");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

// A redraw that comes while an edit is in flight (here the earlier account
// edit's) shows the card with that edit, not as the file held it before.
for (const [what, edit, shows] of [
  ["model", (page) => card(page, 4).getByLabel("Model").selectOption("claude-fable-5-1"),
    (page) => expect(card(page, 4).getByLabel("Model")).toHaveValue("claude-fable-5-1")],
  ["cap", (page) => card(page, 4).getByRole("button", { name: "Add caps" }).click(),
    (page) => expect(card(page, 4).getByRole("button", { name: "Add caps" })).toHaveCount(0)],
  ["schedule", (page) => attach(page, 4, "Skip", "night"),
    (page) => expect(card(page, 4).getByTitle("Skip during night")).toBeVisible()],
]) {
  test(`a redraw while a ${what} edit is in flight shows the edit`, async ({ page }) => {
    const gate = await stateGate(page);
    await card(page, 4).getByLabel("Account").selectOption("claude-spare");
    await expectConfig(server.configPath, (c) => coder(c)[3].account, "claude-spare", "account");
    await edit(page);
    const drawn = await card(page, 4).getByLabel("Account").elementHandle();
    gate.pass(1);
    await expect.poll(() => drawn.evaluate((el) => el.isConnected), { message: "the account's redraw" }).toBe(false);
    await expect.poll(() => gate.held(), { message: `the ${what} edit answered, its reload held` }).toBe(1);
    await shows(page);
    gate.open();
    await expect(page.getByText("changed outside the UI")).toHaveCount(0);
    await shows(page);
  });
}

// An edit made on a card before a move's redraw is shown on that card only:
// redrawn by the move, the card now at its old place does not show it.
test("an edit made before a move's redraw is not shown on the card now in its place", async ({ page }) => {
  const mod = await modKey(page);
  const gate = await stateGate(page);
  await card(page, 4).focus();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, accountsOf, [ORIGINAL[0], ORIGINAL[1], ORIGINAL[3], ORIGINAL[2]], "sonnet third");
  await card(page, 4).getByLabel("Model").selectOption("claude-fable-5-1");
  gate.pass(1);
  await expectConfig(server.configPath, (c) => coder(c)[2].model, "claude-fable-5-1", "the moved card's model");
  await expect.poll(() => gate.held(), { message: "the model edit answered, its reload held" }).toBe(1);
  await expect(card(page, 4).getByLabel("Model")).toHaveValue("opencode-go/deepseek-v4.1");
  gate.open();
  await expect(card(page, 3).getByLabel("Model")).toHaveValue("claude-fable-5-1");
  await expect(card(page, 4).getByLabel("Model")).toHaveValue("opencode-go/deepseek-v4.1");
});

// A refused edit stops showing: redrawn after another card's edit, the card
// is as the file holds it.
for (const [what, edit, back] of [
  ["model", (page) => card(page, 4).getByLabel("Model").selectOption("claude-fable-5-1"),
    (page) => expect(card(page, 4).getByLabel("Model")).toHaveValue("claude-sonnet-5-5")],
  ["cap", (page) => card(page, 4).getByRole("button", { name: "Add caps" }).click(),
    (page) => expect(card(page, 4).getByRole("button", { name: "Add caps" })).toBeVisible()],
  ["schedule", (page) => attach(page, 4, "Skip", "night"),
    (page) => expect(card(page, 4).getByTitle("Skip during night")).toHaveCount(0)],
]) {
  test(`a refused ${what} edit shows the choice as the file holds it`, async ({ page }) => {
    const before = coder(readConfig(server.configPath))[3];
    await page.route("**/api/op", (route) => route.fulfill({ status: 422, contentType: "application/json",
      body: JSON.stringify({ reason: "refused by the test" }) }), { times: 1 });
    await edit(page);
    await expect(page.getByText("refused by the test")).toBeVisible();
    await card(page, 1).getByLabel("Model").selectOption("claude-fable-5-1");
    await expectConfig(server.configPath, (c) => coder(c)[0].model, "claude-fable-5-1", "choice 1's model");
    await expect(card(page, 1).getByLabel("Model")).toHaveValue("claude-fable-5-1");
    await back(page);
    expect(coder(readConfig(server.configPath))[3]).toEqual(before);
  });
}

// A cap and a schedule edit on one card go through different editors; the
// second, made before the first's redraw, is built on what the first left.
for (const [what, first, second] of [
  ["a schedule after a cap", (page) => card(page, 4).getByRole("button", { name: "Add caps" }).click(),
    (page) => attach(page, 4, "Skip", "night")],
  ["a cap after a schedule", (page) => attach(page, 4, "Skip", "night"),
    (page) => card(page, 4).getByRole("button", { name: "Add caps" }).click()],
]) {
  test(`${what} on one card before the redraw both apply`, async ({ page }) => {
    const before = coder(readConfig(server.configPath))[3];
    const release = await holdState(page);
    await first(page);
    await expect.poll(() => coder(readConfig(server.configPath))[3], { message: "the first edit" }).not.toEqual(before);
    await second(page);
    release();
    await expectConfig(server.configPath, (c) => [coder(c)[3].cap, coder(c)[3].skip_during], [[], ["night"]],
      "both edits");
    await expect(page.getByText("changed outside the UI")).toHaveCount(0);
  });
}

// capBox is the closed box (or, once focused, the open frame's number box)
// of a card's first window.
const capBox = (card_) => card_.getByRole("textbox", { name: /cap$/ }).first();

test("cap by typing, fixed and pace", async ({ page }) => {
  const c3 = card(page, 3);
  await capBox(c3).click();
  await capBox(c3).fill("90");
  await capBox(c3).press("Enter");
  await expectConfig(server.configPath, (c) => coder(c)[2].cap, [90, 95, 98], "fixed 90 in the first window");
  await capBox(c3).click();
  await c3.getByRole("button", { name: "Time elapsed + N" }).click();
  await capBox(c3).fill("10");
  await capBox(c3).press("Enter");
  await expectConfig(server.configPath, (c) => coder(c)[2].cap, ["pace+10", 95, 98], "pace+10");
  await expect(c3.getByText("Cap at time elapsed +")).toBeVisible();
  await expect(capBox(c3)).toHaveValue("10");
});

// dragRange presses the open frame's slider at fromFrac of its width and
// releases it at toFrac.
async function dragRange(page, card_, fromFrac, toFrac) {
  const box = await card_.getByRole("slider").boundingBox();
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width * fromFrac, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * ((fromFrac + toFrac) / 2), y, { steps: 5 });
  await page.mouse.move(box.x + box.width * toFrac, y, { steps: 5 });
  await page.mouse.up();
}

// The first cap of the go-1 choice, as the file holds it.
const firstCap = () => coder(readConfig(server.configPath))[2].cap[0];

// A switch from fixed to "Time elapsed + N" shows N = 5 (the smallest).
const PACE_START = 5;

test("cap by drag, fixed and pace", async ({ page }) => {
  const c3 = card(page, 3);
  const start = firstCap();
  expect(start).toBe(95);
  await capBox(c3).click();
  await dragRange(page, c3, 0.95, 0.3);
  await expect.poll(firstCap).not.toBe(start);
  const fixed = firstCap();
  expect(typeof fixed).toBe("number");
  expect(fixed).toBeLessThan(start);
  expect(fixed).toBeGreaterThanOrEqual(0);
  expect(coder(readConfig(server.configPath))[2].cap.slice(1)).toEqual([95, 98]);
  await capBox(c3).click();
  await c3.getByRole("button", { name: "Time elapsed + N" }).click();
  await dragRange(page, c3, 0.02, 0.8);
  await expect.poll(() => String(firstCap())).toMatch(/^pace\+\d+$/);
  const n = Number(String(firstCap()).slice(5));
  expect(n).toBeGreaterThan(PACE_START);
  expect(n).toBeLessThanOrEqual(50);
});

test('trash on a box writes "default"', async ({ page }) => {
  const c1 = card(page, 1);
  await capBox(c1).click();
  await c1.getByRole("button", { name: /^Use default cap for / }).click();
  await expectConfig(server.configPath, (c) => coder(c)[0].cap, ["default", 98], "the first box is default");
  await expect(capBox(c1)).toHaveValue("95%");
});

// An untouched default box has nothing for trash to do, so its editor shows
// none, and its line says (default). No caps, at the right of the boxes,
// makes the choice uncapped.
test("an untouched default box has no trash; No caps uncaps", async ({ page }) => {
  const c4 = card(page, 4);
  await c4.getByRole("button", { name: "Add caps" }).click();
  await expectConfig(server.configPath, (c) => coder(c)[3].cap, [], "every position default");
  await capBox(c4).click();
  await expect(capBox(c4)).toBeFocused();
  await expect(c4.locator(".cap-edit-line")).toContainText("(default)");
  await expect(c4.locator(".cap-trash")).toBeHidden();
  await capBox(c4).press("Escape");
  await c4.getByRole("button", { name: "No caps" }).click();
  await expectConfig(server.configPath, (c) => coder(c)[3].cap ?? null, null, "uncapped");
  await expect(c4.getByText("No cap", { exact: true })).toBeVisible();
  await expect(c4.getByRole("button", { name: "No caps" })).toHaveCount(0);
});

// Any change to a default box makes it a customized cap, even when the
// value or the mode ends where the default was; trash makes it default again.
test("a touched default box is saved as set, even at the default value", async ({ page }) => {
  const c4 = card(page, 4);
  await c4.getByRole("button", { name: "Add caps" }).click();
  await expectConfig(server.configPath, (c) => coder(c)[3].cap, [], "every position default");
  await capBox(c4).click();
  await capBox(c4).fill("80");
  await expect(c4.locator(".cap-edit-line")).not.toContainText("(default)");
  await expect(c4.locator(".cap-trash")).toBeVisible();
  await capBox(c4).fill("95");
  await expect(c4.locator(".cap-trash")).toBeVisible();
  await capBox(c4).press("Enter");
  await expectConfig(server.configPath, (c) => coder(c)[3].cap, [95], "95 typed is saved as 95");
  // The open editor floats over the boxes to its right: leave it first.
  await page.locator("body").click({ position: { x: 5, y: 5 } });
  await expect(c4.locator(".cap-edit")).toHaveCount(0);
  const second = c4.getByRole("textbox", { name: /cap$/ }).nth(1);
  await second.click();
  await c4.getByRole("button", { name: "Time elapsed + N" }).click();
  await c4.getByRole("button", { name: "%", exact: true }).click();
  await expect(c4.locator(".cap-edit-line")).not.toContainText("(default)");
  await second.press("Enter");
  await expectConfig(server.configPath, (c) => coder(c)[3].cap, [95, 95], "a mode switched there and back is saved");
  await capBox(c4).click();
  await expect(c4.locator(".cap-edit-line")).not.toContainText("(default)");
  await c4.locator(".cap-trash").click();
  await expectConfig(server.configPath, (c) => coder(c)[3].cap, ["default", 95], "trash makes it default again");
});

// A drag on a default box makes it a customized cap too, even one that ends
// where it started: the handle is pressed at 95, moved away and brought back.
test("a drag on a default box saves it as set", async ({ page }) => {
  const c4 = card(page, 4);
  await c4.getByRole("button", { name: "Add caps" }).click();
  await expectConfig(server.configPath, (c) => coder(c)[3].cap, [], "every position default");
  await capBox(c4).click();
  const r = await c4.getByRole("slider").boundingBox();
  // The thumb's centre at 95: the range overhangs the bar by half a 14 px
  // thumb on each side.
  const at95 = r.x + 7 + ((r.width - 14) * 95) / 100;
  const y = r.y + r.height / 2;
  await page.mouse.move(at95, y);
  await page.mouse.down();
  await page.mouse.move(r.x + r.width * 0.3, y, { steps: 5 });
  await page.mouse.move(at95, y, { steps: 5 });
  await page.mouse.up();
  await expectConfig(server.configPath, (c) => typeof coder(c)[3].cap[0], "number", "the dragged box is set");
});

// dragTo presses grip (a drag handle, or a box {x, y, width, height}) and drops it on target at the height
// at(box) gives (default: the lower part of target), in steps so the browser
// sees the pointer travel (a real drag). The "Drop here" slot shifts the
// cards while the pointer moves, so the drop point is read again once the
// pointer is over the target.
async function dragTo(page, grip, target, at = (b) => b.y + b.height - 8) {
  // Every card must be inside the viewport: a pointer outside it sends no
  // drag events.
  await page.setViewportSize({ width: 1280, height: 1400 });
  const handle = grip.boundingBox ? await grip.boundingBox() : grip;
  const hx = handle.x + handle.width / 2;
  const hy = handle.y + handle.height / 2;
  await page.mouse.move(hx, hy);
  await page.mouse.down();
  await page.mouse.move(hx, hy + 12, { steps: 4 });
  const near = await target.boundingBox();
  await page.mouse.move(near.x + 40, near.y + near.height / 2, { steps: 12 });
  const now = await target.boundingBox();
  await page.mouse.move(now.x + 40, at(now), { steps: 4 });
  await page.mouse.up();
}

// dragCard drags card from by its handle onto the lower part of card to.
async function dragCard(page, from, to) {
  await dragTo(page, card(page, from).locator(".p-handle"), card(page, to));
}

test("reorder by mouse drag", async ({ page }) => {
  await dragCard(page, 1, 3);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[1], ORIGINAL[2], ORIGINAL[0], ORIGINAL[3]],
    "the first choice is dropped after the third",
  );
  await expect(card(page, 3)).toBeFocused();
});

test("reorder by keyboard", async ({ page }) => {
  const mod = (await page.evaluate(() => navigator.platform)).startsWith("Mac") ? "Meta" : "Control";
  await card(page, 2).focus();
  await page.keyboard.press(`${mod}+ArrowDown`);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[0], ORIGINAL[2], ORIGINAL[1], ORIGINAL[3]],
    "the second choice moved down one",
  );
  await expect(card(page, 3)).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[0], ORIGINAL[1], ORIGINAL[2], ORIGINAL[3]],
    "moved back up one",
  );
  await expect(card(page, 2)).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(
    server.configPath,
    accountsOf,
    [ORIGINAL[1], ORIGINAL[0], ORIGINAL[2], ORIGINAL[3]],
    "moved up to the top",
  );
});

async function attach(page, n, side, name) {
  const c = card(page, n);
  await c.getByRole("button", { name: "+ schedule" }).click();
  await c.getByRole("button", { name: side, exact: true }).click();
  await c.getByRole("button", { name, exact: true }).click();
}

test("attach a skip chip and an allow chip", async ({ page }) => {
  await attach(page, 4, "Skip", "night");
  await expectConfig(server.configPath, (c) => coder(c)[3].skip_during, ["night"], "skip_during");
  await expect(card(page, 4).getByTitle("Skip during night")).toBeVisible();
  await attach(page, 2, "Allow", "night");
  await expectConfig(server.configPath, (c) => coder(c)[1].allow_during, ["night"], "allow_during");
  await expect(card(page, 2).getByTitle("Allow only during night")).toBeVisible();
  await expectConfig(server.configPath, (c) => coder(c)[1].skip_during ?? null, null, "no skip on the allow card");
});

test("global default cap", async ({ page }) => {
  const box = page.getByLabel("Global default cap");
  await box.fill("80");
  await box.press("Enter");
  await expectConfig(server.configPath, (c) => c.settings.default_cap, 80, "default_cap 80");
  await expect(box).toHaveValue("80%");
});

test("global default cap: the page bounds 101, the server refuses it", async ({ page }) => {
  const box = page.getByLabel("Global default cap");
  await box.fill("101");
  await box.press("Enter");
  await expectConfig(server.configPath, (c) => c.settings.default_cap, 100, "the page clamps 101 to 100");
  await expect(box).toHaveValue("100%");
  const before = fs.readFileSync(server.configPath, "utf8");
  const state = await (await page.request.get(new URL("/api/state", server.url).href)).json();
  const r = await page.request.post(new URL("/api/op", server.url).href, {
    headers: { Origin: new URL(server.url).origin },
    data: { base: state.sha256, op: "settings.default_cap", args: { value: 101 } },
  });
  expect(r.ok()).toBe(false);
  expect((await r.json()).reason).toBe("Global default cap: 101 is outside 0–100%");
  expectUnchanged(server.configPath, before);
});

// ---------- load balancing groups ----------

// shapeOf writes coder as account names, a group as [names] after its
// algorithm: ["claude-main", "random[go-1 go-2]", …].
const shapeOf = (cfg) => coder(cfg).map((it) =>
  (it.choices ? `${it.load_balance}[${it.choices.map((m) => m.account).join(" ")}]` : it.account));

const GROUPED = ["claude-main", "random[go-1 go-2]", "codex-main", "claude-main"];

// grouped hand-edits coder into GROUPED (more changes: mutate) and shows it.
async function grouped(page, mutate = () => {}) {
  handEdit(server.configPath, (c) => {
    loadBalanceGo(c);
    mutate(c);
  });
  await page.reload();
  await expect(card(page, "2a")).toBeVisible();
}

test("a group shows its frame, header and member numbers", async ({ page }) => {
  await grouped(page);
  const frame = card(page, 2);
  await expect(frame).toContainText("2 · Load balancing 2 models");
  await expect(frame).toContainText("· algorithm: random");
  await expect(frame.getByRole("button", { name: "Stop load balancing" })).toBeVisible();
  await expect(frame.getByRole("button", { name: "+ Add Model" })).toBeVisible();
  for (const [n, account] of [["2a", "go-1"], ["2b", "go-2"]]) {
    const m = frame.getByRole("group", { name: `Choice ${n}`, exact: true });
    await expect(m.getByLabel("Account")).toHaveValue(account);
    await expect(m.locator(".p-n")).toHaveText(n);
    await expect(m.getByRole("button", { name: `Remove choice ${n}` })).toBeVisible();
  }
  // Positions after the group count on from it.
  await expect(card(page, 1).getByLabel("Account")).toHaveValue("claude-main");
  await expect(card(page, 3).getByLabel("Account")).toHaveValue("codex-main");
  await expect(card(page, 4).getByLabel("Model")).toHaveValue("claude-sonnet-5-5");
  await expect(card(page, 5)).toHaveCount(0);
});

test("a member's fields edit that member", async ({ page }) => {
  await grouped(page);
  await card(page, "2b").getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices.map((m) => m.model),
    ["opencode-go/deepseek-v4.1", "opencode-go/kimi-k3"], "2b's model");
  await expectConfig(server.configPath, shapeOf, GROUPED, "still one group");
});

test("Stop load balancing makes members plain choices in place", async ({ page }) => {
  await grouped(page);
  await card(page, 2).getByRole("button", { name: "Stop load balancing" }).click();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "go-1", "go-2", "codex-main", "claude-main"],
    "the members are plain, at the group's place");
  // The file is written before the page redraws: until then choice 2 is
  // still the frame, holding two Account fields.
  await expect(page.getByRole("button", { name: "Stop load balancing" })).toHaveCount(0);
  await expect(card(page, 2).getByLabel("Account")).toHaveValue("go-1");
  await expect(card(page, 3).getByLabel("Account")).toHaveValue("go-2");
});

test("+ Add Model inside a group adds a member", async ({ page }) => {
  await grouped(page);
  await card(page, 2).getByRole("button", { name: "+ Add Model" }).click();
  // Both opencode-go accounts are in the group: the first unused account.
  await expectConfig(server.configPath, shapeOf,
    ["claude-main", "random[go-1 go-2 claude-main]", "codex-main", "claude-main"], "a third member");
  await expect(card(page, "2c").getByLabel("Account")).toHaveValue("claude-main");
  await expect(card(page, 2)).toContainText("2 · Load balancing 3 models");
});

test("removing the last-but-one member dissolves the group", async ({ page }) => {
  await grouped(page);
  await card(page, "2b").getByRole("button", { name: "Remove choice 2b" }).click();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "go-1", "codex-main", "claude-main"],
    "go-1 stays plain at the group's place");
  // Wait for the redraw first, as above.
  await expect(page.getByRole("button", { name: "Stop load balancing" })).toHaveCount(0);
  await expect(card(page, "2a")).toHaveCount(0);
  await expect(card(page, 2).getByLabel("Account")).toHaveValue("go-1");
});

test("a doctor finding on choice 2b marks that member card", async ({ page }) => {
  await grouped(page, (c) => { c.profiles.coder[1].choices[1].thinking = "ultra"; });
  await expect(card(page, "2b")).toHaveClass(/is-error/);
  await expect(card(page, "2a")).not.toHaveClass(/is-error/);
  await expect(card(page, 2)).not.toHaveClass(/is-error/);
  await expect(card(page, 3)).not.toHaveClass(/is-error/);
  await expect(page.getByRole("link", { name: "coder", exact: true })).toHaveClass(/is-error/);
});

// A plain card dragged past a group lands where it was dropped: the drop
// index counts the group's members.
test("a plain card dragged past a group lands where it was dropped", async ({ page }) => {
  await grouped(page);
  await dragCard(page, 1, 3);
  await expectConfig(server.configPath, shapeOf, ["random[go-1 go-2]", "codex-main", "claude-main", "claude-main"],
    "opus after codex-main");
  await expectConfig(server.configPath, (c) => coder(c)[2].model, "claude-opus-5-5", "the moved card");
});

// ---------- moving into, out of and with groups ----------

const modKey = async (page) => ((await page.evaluate(() => navigator.platform)).startsWith("Mac") ? "Meta" : "Control");
// frameGrip is a group's own drag handle, in its header.
const frameGrip = (page, n) => card(page, n).locator(".p-group-head .p-handle");
const top8 = (b) => b.y + 8;
const middle = (b) => b.y + b.height / 2;

// spare is a third member for coder's group: opus on claude-spare.
const spare = (c) => ({ ...c.profiles.coder[0], account: "claude-spare" });
const withThird = (c) => c.profiles.coder[1].choices.push(spare(c));
// twoGroups makes coder 1 opus, 2a go-1, 2b go-2, 3a codex-main,
// 3b claude-spare, 4 sonnet.
const twoGroups = (c) => {
  const [opus, g, codex, sonnet] = c.profiles.coder;
  c.profiles.coder = [opus, g, { load_balance: "random", choices: [codex, { ...opus, account: "claude-spare" }] }, sonnet];
};
const TWO_GROUPS = ["claude-main", "random[go-1 go-2]", "random[codex-main claude-spare]", "claude-main"];

test("drag: a plain card dropped between two members joins the group there", async ({ page }) => {
  await grouped(page);
  const opus = coder(readConfig(server.configPath))[0];
  await dragTo(page, card(page, 1).locator(".p-handle"), card(page, "2a"));
  await expectConfig(server.configPath, shapeOf, ["random[go-1 claude-main go-2]", "codex-main", "claude-main"],
    "opus is the group's second member");
  expect(coder(readConfig(server.configPath))[0].choices[1]).toEqual(opus);
  await expect(card(page, "1b")).toBeFocused();
});

test("drag: a plain card dropped at the end of the group just above it joins it", async ({ page }) => {
  await grouped(page);
  await dragTo(page, card(page, 3).locator(".p-handle"), card(page, "2b"));
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-1 go-2 codex-main]", "claude-main"],
    "codex-main joined at the end, from just below the frame");
  await expect(card(page, "2c")).toBeFocused();
});

test("drag: a member dragged out of a group of two lands plain and dissolves it", async ({ page }) => {
  await grouped(page);
  await dragCard(page, "2b", 3);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "go-1", "codex-main", "go-2", "claude-main"],
    "go-1 plain at the group's place, go-2 where it was dropped");
  await expect(card(page, 4)).toBeFocused();
  await expect(card(page, 4).getByLabel("Account")).toHaveValue("go-2");
});

test("drag: a member dropped on its own frame's header leaves the group, above it", async ({ page }) => {
  await grouped(page, withThird);
  await dragTo(page, card(page, "2a").locator(".p-handle"), card(page, 2).locator(".p-group-head"), middle);
  await expectConfig(server.configPath, shapeOf,
    ["claude-main", "go-1", "random[go-2 claude-spare]", "codex-main", "claude-main"], "go-1 plain, just above");
  await expect(card(page, 2)).toBeFocused();
});

test("drag: a member dropped between members of its group reorders it", async ({ page }) => {
  await grouped(page, withThird);
  await dragTo(page, card(page, "2a").locator(".p-handle"), card(page, "2b"));
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-2 go-1 claude-spare]", "codex-main",
    "claude-main"], "go-1 second, still a member");
});

test("drag: a whole group moves as one position", async ({ page }) => {
  await grouped(page);
  await dragTo(page, frameGrip(page, 2), card(page, 3));
  await expectConfig(server.configPath, shapeOf, ["claude-main", "codex-main", "random[go-1 go-2]", "claude-main"],
    "the group after codex-main");
  await expect(card(page, 3)).toBeFocused();
  await expect(card(page, 3)).toContainText("3 · Load balancing 2 models");
});

test("drag: a group dropped on another group's members never joins it", async ({ page }) => {
  await grouped(page, twoGroups);
  await dragTo(page, frameGrip(page, 2), card(page, "3b"));
  await expectConfig(server.configPath, shapeOf,
    ["claude-main", "random[codex-main claude-spare]", "random[go-1 go-2]", "claude-main"],
    "both groups whole, the dragged one after the other");
});

test("drag: between two adjacent groups, the slot dropped on decides", async ({ page }) => {
  const cases = [
    [(p) => card(p, "2b"), undefined,
      ["random[go-1 go-2 claude-main]", "random[codex-main claude-spare]", "claude-main"], "inside the first's end"],
    [(p) => card(p, "3a"), top8,
      ["random[go-1 go-2]", "random[claude-main codex-main claude-spare]", "claude-main"], "inside the second's start"],
    [(p) => card(p, 2).getByRole("button", { name: "+ Add Model" }), middle,
      ["random[go-1 go-2]", "claude-main", "random[codex-main claude-spare]", "claude-main"], "between them"],
  ];
  const original = fs.readFileSync(server.configPath, "utf8");
  for (const [target, at, want, what] of cases) {
    fs.writeFileSync(server.configPath, original);
    await grouped(page, twoGroups);
    await expectConfig(server.configPath, shapeOf, TWO_GROUPS, "the two groups");
    await dragTo(page, card(page, 1).locator(".p-handle"), target(page), at);
    await expectConfig(server.configPath, shapeOf, want, what);
  }
});

test("keyboard: a member at the frame's edge moves out; a group of two dissolves", async ({ page }) => {
  await grouped(page);
  const mod = await modKey(page);
  await card(page, "2b").focus();
  await page.keyboard.press(`${mod}+ArrowDown`);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "go-1", "go-2", "codex-main", "claude-main"],
    "go-2 plain just below, go-1 plain at the group's place");
  await expect(card(page, 3)).toBeFocused();
  await expect(card(page, 3).getByLabel("Account")).toHaveValue("go-2");
});

test("keyboard: the first member moves out above; a group of three stays", async ({ page }) => {
  await grouped(page, withThird);
  const mod = await modKey(page);
  await card(page, "2a").focus();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf,
    ["claude-main", "go-1", "random[go-2 claude-spare]", "codex-main", "claude-main"], "go-1 plain just above");
  await expect(card(page, 2)).toBeFocused();
  await expect(card(page, 2).getByLabel("Account")).toHaveValue("go-1");
});

test("keyboard: a member not at an edge moves within its group", async ({ page }) => {
  await grouped(page, withThird);
  const mod = await modKey(page);
  await card(page, "2a").focus();
  await page.keyboard.press(`${mod}+ArrowDown`);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-2 go-1 claude-spare]", "codex-main",
    "claude-main"], "go-1 second, still a member");
  await expect(card(page, "2b")).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-1 go-2 claude-spare]", "codex-main",
    "claude-main"], "back first");
  await expect(card(page, "2a")).toBeFocused();
});

test("keyboard: a plain card moves past a whole group, never into it", async ({ page }) => {
  await grouped(page);
  const mod = await modKey(page);
  await card(page, 1).focus();
  await page.keyboard.press(`${mod}+ArrowDown`);
  await expectConfig(server.configPath, shapeOf, ["random[go-1 go-2]", "claude-main", "codex-main", "claude-main"],
    "opus past the group");
  await expect(card(page, 2)).toBeFocused();
  await expect(card(page, 2).getByLabel("Model")).toHaveValue("claude-opus-5-5");
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf, GROUPED, "back above it");
  await expect(card(page, 1)).toBeFocused();
  await card(page, 3).focus();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "codex-main", "random[go-1 go-2]", "claude-main"],
    "codex-main up past the group");
  await expect(card(page, 2)).toBeFocused();
});

test("keyboard: a focused group frame moves by a position", async ({ page }) => {
  await grouped(page);
  const mod = await modKey(page);
  await card(page, 2).focus();
  await expect(card(page, 2)).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowDown`);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "codex-main", "random[go-1 go-2]", "claude-main"],
    "the group down one");
  await expect(card(page, 3)).toBeFocused();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf, GROUPED, "back");
  await expect(card(page, 2)).toBeFocused(); // redrawn: the next key moves the group from 2
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf, ["random[go-1 go-2]", "claude-main", "codex-main", "claude-main"],
    "to the top");
  await expect(card(page, 1)).toBeFocused();
});

test("keyboard: ↑/↓ reach a group's frame between cards", async ({ page }) => {
  await grouped(page);
  await card(page, 1).focus();
  await page.keyboard.press("ArrowDown");
  await expect(card(page, 2)).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(card(page, "2a")).toBeFocused();
});

// Moves sent before the redraw of an edit carry the choices as the edit
// left them, not as they were drawn.
test("a move right after an edit still applies", async ({ page }) => {
  await grouped(page);
  const mod = await modKey(page);
  const release = await holdState(page);
  await card(page, 4).getByLabel("Model").selectOption("claude-fable-5-1");
  await expectConfig(server.configPath, (c) => coder(c)[3].model, "claude-fable-5-1", "choice 4's model");
  await card(page, 4).focus();
  await page.keyboard.press(`${mod}+ArrowUp`);
  release();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-1 go-2]", "claude-main", "codex-main"],
    "moved, sent on the stale card");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

test("a move within a group right after another member's edit still applies", async ({ page }) => {
  await grouped(page);
  const mod = await modKey(page);
  const release = await holdState(page);
  await card(page, "2a").getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices[0].model, "opencode-go/kimi-k3", "2a's model");
  await card(page, "2b").focus();
  await page.keyboard.press(`${mod}+ArrowUp`);
  release();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-2 go-1]", "codex-main", "claude-main"],
    "go-2 first, the group_expect as the edit left it");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

test("a group move right after a member's edit still applies", async ({ page }) => {
  await grouped(page);
  const mod = await modKey(page);
  const release = await holdState(page);
  await card(page, "2a").getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices[0].model, "opencode-go/kimi-k3", "2a's model");
  await card(page, 2).focus();
  await page.keyboard.press(`${mod}+ArrowDown`);
  release();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "codex-main", "random[go-1 go-2]", "claude-main"],
    "moved, with the expect as the edit left it");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

// A file that parses but is invalid (default_cap 500) is read-only: the
// page still shows the profile, and main.js makes every focusable or
// draggable element inert, a group's frame included.
test("read-only: a group frame cannot be moved by keyboard or drag", async ({ page }) => {
  await grouped(page, (c) => { c.settings.default_cap = 500; });
  await expect(page.getByRole("alert").filter({ hasText: "Cannot edit — " })).toBeVisible();
  const mod = await modKey(page);
  const sent = opsSent(page);
  const before = fs.readFileSync(server.configPath, "utf8");
  const frame = card(page, 2);
  // The server refuses any op while read-only, so the file alone proves
  // nothing: each step must send no op at all.
  const steps = [
    ["the frame's drag", () => dragTo(page, frameGrip(page, 2), card(page, 3))],
    ["a card's drag into the frame", () => dragTo(page, card(page, 1).locator(".p-handle"), card(page, "2a"))],
    ["⌘/Ctrl+↓ on the frame", async () => {
      await frame.focus();
      await page.keyboard.press(`${mod}+ArrowDown`);
    }],
  ];
  for (const [name, act] of steps) {
    await act();
    await page.waitForTimeout(300);
    expect(sent, `no op sent by ${name}`).toEqual([]);
  }
  expectUnchanged(server.configPath, before);
  await expect(frame).toHaveJSProperty("inert", true);
  await frame.focus();
  await expect(frame).not.toBeFocused();
});

// ---------- creating a group: selection mode ----------

const lbStart = (page) => page.getByRole("button", { name: "⇉ Load balance…", exact: true });
const lbConfirm = (page) => page.getByRole("button", { name: /^Load balance \d+ models$/ });
const lbCancel = (page) => page.getByRole("button", { name: "Cancel", exact: true });
const pick = (page, n) => page.getByRole("checkbox", { name: `Select choice ${n}`, exact: true });


test("Load balance: selected cards become one group at the first one's place", async ({ page }) => {
  // Five plain choices: opus, codex, go-1, go-2, sonnet.
  handEdit(server.configPath, (c) => c.profiles.coder.splice(3, 0, { ...c.profiles.coder[2], account: "go-2" }));
  await page.reload();
  await expect(card(page, 5)).toBeVisible();
  const before = coder(readConfig(server.configPath));
  await lbStart(page).click();
  for (const n of [4, 1, 3]) await pick(page, n).check();
  await expect(lbConfirm(page)).toHaveText("Load balance 3 models");
  await lbConfirm(page).click();
  await expectConfig(server.configPath, shapeOf, ["random[claude-main go-1 go-2]", "codex-main", "claude-main"],
    "one group at choice 1's place, members in file order, codex-main after it");
  expect(coder(readConfig(server.configPath))[0].choices).toEqual([before[0], before[2], before[3]]);
  await expect(card(page, 1)).toContainText("1 · Load balancing 3 models");
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await expect(lbStart(page)).toBeVisible();
});

test("Load balance: confirm is disabled below 2 selected", async ({ page }) => {
  await lbStart(page).click();
  await expect(lbConfirm(page)).toHaveText("Load balance 0 models");
  await expect(lbConfirm(page)).toBeDisabled();
  await pick(page, 2).check();
  await expect(lbConfirm(page)).toHaveText("Load balance 1 models");
  await expect(lbConfirm(page)).toBeDisabled();
  await pick(page, 3).check();
  await expect(lbConfirm(page)).toHaveText("Load balance 2 models");
  await expect(lbConfirm(page)).toBeEnabled();
  await pick(page, 3).uncheck();
  await expect(lbConfirm(page)).toBeDisabled();
});

test("Load balance: Cancel and Esc leave the profile unchanged", async ({ page }) => {
  const before = fs.readFileSync(server.configPath, "utf8");
  await lbStart(page).click();
  await pick(page, 1).check();
  await pick(page, 2).check();
  await lbCancel(page).click();
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await expect(lbStart(page)).toBeVisible();
  await lbStart(page).click();
  await expect(pick(page, 1)).not.toBeChecked();
  await pick(page, 3).check();
  await pick(page, 4).check();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await expect(lbStart(page)).toBeVisible();
  expectUnchanged(server.configPath, before);
});

test("Load balance: cards already in a group have no checkbox", async ({ page }) => {
  await grouped(page);
  await lbStart(page).click();
  await expect(page.getByRole("checkbox")).toHaveCount(3);
  for (const n of [1, 3, 4]) await expect(pick(page, n)).toBeVisible();
  await expect(card(page, 2).getByRole("checkbox")).toHaveCount(0);
  await expect(card(page, 2).locator("input[type=checkbox]")).toHaveCount(0);
});

test("Load balance: the button shows only while two cards could be grouped", async ({ page }) => {
  // Choice 1 and the group: one plain card, nothing to group it with.
  await grouped(page, (c) => c.profiles.coder.splice(2, 2));
  await expect(card(page, 1)).toBeVisible();
  await expect(lbStart(page)).toHaveCount(0);
  await card(page, 2).getByRole("button", { name: "Stop load balancing" }).click();
  await expect(card(page, 3)).toBeVisible();
  await expect(lbStart(page)).toBeVisible();
});

test("Load balance: the checkboxes work from the keyboard", async ({ page }) => {
  await lbStart(page).focus();
  await page.keyboard.press("Enter");
  await expect(pick(page, 1)).toBeFocused();
  await page.keyboard.press("Space");
  await expect(pick(page, 1)).toBeChecked();
  await page.keyboard.press("Tab");
  await expect(pick(page, 2)).toBeFocused();
  await page.keyboard.press("Space");
  await expect(lbConfirm(page)).toHaveText("Load balance 2 models");
  await lbConfirm(page).click();
  await expectConfig(server.configPath, shapeOf, ["random[claude-main codex-main]", "go-1", "claude-main"],
    "the two checked from the keyboard");
});

// Every inert control is pressed the way a user would (the mouse, or a key
// on the focused card); then one ordinary edit is sent. Ops run in order, so
// once that edit is in the file anything the inert controls sent would be too.
// opsSent records every /api/op request the page sends from now on.
function opsSent(page) {
  const sent = [];
  page.on("request", (r) => {
    if (new URL(r.url()).pathname === "/api/op") sent.push(r.postData());
  });
  return sent;
}

// grips are where the drag handles of card 1, member 2a and group 2's frame
// sit: their first child, which is the handle outside selection mode.
const grips = (page) => [card(page, 1).locator(".p-pos > :first-child"),
  card(page, "2a").locator(".p-pos > :first-child"), card(page, 2).locator(".p-group-head > :first-child")];

test("Load balance: Remove, Stop, + Add Model, fields and moves are inert while selecting", async ({ page }) => {
  await grouped(page);
  // Every control pressed below must be inside the viewport.
  await page.setViewportSize({ width: 1280, height: 1400 });
  // Outside selection mode the grips are the handles; their places are
  // where the drags below start.
  const handles = [];
  for (const g of grips(page)) {
    await expect(g).toHaveClass("p-handle");
    handles.push(await g.boundingBox());
  }
  await lbStart(page).click();
  const sent = opsSent(page);
  for (const key of ["c0-remove", "c1-remove", "group-1-stop", "group-1-add", "add-choice"]) {
    const box = await page.locator(`[data-key="${key}"]`).boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  }
  const account = page.locator('[data-key="c0-account"]');
  await account.focus();
  await expect(account).not.toBeFocused();
  await expect(page.locator(".p-handle")).toHaveCount(0);
  // Neither a card nor a frame can take focus, so no ⌘/Ctrl+arrow reaches
  // them; a drag from where each handle sits outside selection mode (card
  // 1, member 2a, the frame) sends nothing.
  for (const el of [page.locator('[data-key="card-0"]'), card(page, 2)]) {
    await expect(el).not.toHaveAttribute("tabindex", /.*/);
    await el.focus();
    await expect(el).not.toBeFocused();
  }
  const before = fs.readFileSync(server.configPath, "utf8");
  const [g1, g2a, gFrame] = grips(page);
  for (const [i, g] of [g1, g2a, gFrame].entries()) {
    const box = await g.boundingBox();
    expect(Math.abs(box.x - handles[i].x) + Math.abs(box.y - handles[i].y), `grip ${i} in the handle's place`)
      .toBeLessThanOrEqual(1);
  }
  for (const [name, g, target] of [["card 1", g1, card(page, 3)], ["member 2a", g2a, card(page, 4)],
    ["the frame", gFrame, card(page, 4)]]) {
    await dragTo(page, g, target);
    // A drop sends its op at once; give a sent one time to show.
    await page.waitForTimeout(300);
    expect(sent, `no op sent by the drag from ${name}`).toEqual([]);
  }
  expectUnchanged(server.configPath, before);
  await expect(pick(page, 1)).toBeVisible();
  await lbCancel(page).click();
  await card(page, 4).getByLabel("Model").selectOption("claude-fable-5-1");
  await expectConfig(server.configPath, (c) => coder(c)[3].model, "claude-fable-5-1", "the edit after Cancel");
  await expectConfig(server.configPath, shapeOf, GROUPED, "nothing pressed while selecting was sent");
});

test("Load balance: the selection resets on another profile and on new state", async ({ page }) => {
  await lbStart(page).click();
  await pick(page, 1).check();
  await page.getByRole("link", { name: "tester", exact: true }).click();
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await page.getByRole("link", { name: "coder", exact: true }).click();
  await expect(lbStart(page)).toBeVisible();
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await lbStart(page).click();
  await pick(page, 1).check();
  handEdit(server.configPath, (c) => { c.settings.default_cap = 80; });
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(page.getByLabel("Global default cap")).toHaveValue("80%");
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await expect(lbStart(page)).toBeVisible();
});

// Group ops sent before the redraw of a member's edit carry the members as
// that edit left them, not as they were drawn.
test("Stop load balancing right after a member's edit still applies", async ({ page }) => {
  await grouped(page);
  const release = await holdState(page);
  await card(page, "2b").getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices[1].model, "opencode-go/kimi-k3", "2b's model");
  await card(page, 2).getByRole("button", { name: "Stop load balancing" }).click();
  release();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "go-1", "go-2", "codex-main", "claude-main"],
    "dissolved, sent on the stale frame");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

test("+ Add Model in a group right after a member's edit still applies", async ({ page }) => {
  await grouped(page);
  const release = await holdState(page);
  await card(page, "2a").getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices[0].model, "opencode-go/kimi-k3", "2a's model");
  await card(page, 2).getByRole("button", { name: "+ Add Model" }).click();
  release();
  await expectConfig(server.configPath, shapeOf,
    ["claude-main", "random[go-1 go-2 claude-main]", "codex-main", "claude-main"], "a third member");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

test("Load balance right after an edit still applies", async ({ page }) => {
  const release = await holdState(page);
  await card(page, 3).getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[2].model, "opencode-go/kimi-k3", "choice 3's model");
  await lbStart(page).click();
  await pick(page, 2).check();
  await pick(page, 3).check();
  await lbConfirm(page).click();
  release();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[codex-main go-1]", "claude-main"],
    "grouped, with choice 3 as the edit left it");
  await expect(page.getByText("changed outside the UI")).toHaveCount(0);
});

// memberOfs is the member_of of each choice op sent, as "op: account …" (the
// group's members) or "op: -" without one.
function memberOfs(sent) {
  return sent.map((body) => JSON.parse(body)).filter((b) => b.op.startsWith("choice."))
    .map((b) => `${b.op}: ${b.args.member_of ? b.args.member_of.choices.map((c) => `${c.account}/${c.model}`).join(" ") : "-"}`);
}

// Every op on a member card names the member's group (member_of), built when
// the op's turn comes: the server looks for a stale member only inside it.
test("ops on a member card send member_of, the group as earlier edits left it", async ({ page }) => {
  await grouped(page);
  const sent = opsSent(page);
  const mod = await modKey(page);
  await card(page, "2b").getByLabel("Model").selectOption("opencode-go/kimi-k3");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices[1].model, "opencode-go/kimi-k3", "2b's model");
  await card(page, "2a").getByRole("button", { name: "No caps" }).click();
  await expectConfig(server.configPath, (c) => "cap" in coder(c)[1].choices[0], false, "2a uncapped");
  await attach(page, "2a", "Skip", "night");
  await expectConfig(server.configPath, (c) => coder(c)[1].choices[0].skip_during, ["night"], "2a skips night");
  await card(page, 1).getByLabel("Model").selectOption("claude-fable-5-1");
  await expectConfig(server.configPath, (c) => coder(c)[0].model, "claude-fable-5-1", "choice 1's model");
  await card(page, "2b").focus();
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expectConfig(server.configPath, shapeOf, ["claude-main", "random[go-2 go-1]", "codex-main", "claude-main"], "2b first");
  await expect(card(page, "2a")).toBeFocused(); // redrawn: 2b is go-1 now
  await card(page, "2b").getByRole("button", { name: "Remove choice 2b" }).click();
  await expectConfig(server.configPath, shapeOf, ["claude-main", "go-2", "codex-main", "claude-main"], "2b removed");
  const go1 = "go-1/opencode-go/deepseek-v4.1";
  const go2 = "go-2/opencode-go/kimi-k3";
  expect(memberOfs(sent)).toEqual([
    `choice.set: ${go1} go-2/opencode-go/deepseek-v4.1`,
    `choice.set: ${go1} ${go2}`,
    `choice.set: ${go1} ${go2}`,
    "choice.set: -",
    `choice.move: ${go1} ${go2}`,
    `choice.remove: ${go2} ${go1}`,
  ]);
});

// A member's op sent before the redraw of a sibling's cap or schedule edit
// carries the group as that edit left it.
for (const [what, edit, done] of [
  ["cap", (page) => card(page, "2a").getByRole("button", { name: "No caps" }).click(),
    (c) => !("cap" in coder(c)[1].choices[0])],
  ["schedule", (page) => attach(page, "2a", "Skip", "night"),
    (c) => JSON.stringify(coder(c)[1].choices[0].skip_during) === '["night"]'],
]) {
  test(`a member's edit right after another member's ${what} edit still applies`, async ({ page }) => {
    await grouped(page);
    const release = await holdState(page);
    await edit(page);
    await expectConfig(server.configPath, done, true, `2a's ${what}`);
    await card(page, "2b").getByLabel("Model").selectOption("opencode-go/kimi-k3");
    release();
    await expectConfig(server.configPath, (c) => coder(c)[1].choices[1].model, "opencode-go/kimi-k3", "2b's model");
    await expect(page.getByText("changed outside the UI")).toHaveCount(0);
  });
}

// This test is its own setup: its page registers the listeners before the
// login, so the first load and the first render of each tab are watched too.
test("no console errors on any tab", async ({ context }) => {
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  page.on("console", (m) => {
    if (m.type() !== "error") return;
    if (/status of (409|422)/.test(m.text())) return;
    errors.push(m.text());
  });
  await login(page, server.url);
  for (const tab of ["Usage", "Profiles", "Schedules", "Accounts"]) {
    await page.getByRole("link", { name: tab, exact: true }).click();
    await expect(page.getByRole("link", { name: tab, exact: true })).toHaveAttribute("aria-current", "page");
    await page.waitForLoadState("networkidle");
  }
  expect(errors).toEqual([]);
});
