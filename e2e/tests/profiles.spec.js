// The Profiles tab in a real browser against the live back end. Every test
// starts its own server (so no test depends on another's edits), acts the
// way a user does and then reads config.json from disk.
const fs = require("node:fs");
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
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

// dragCard presses card from's handle and drops it on the lower part of card
// to, in steps so the browser sees the pointer travel (a real drag). The
// "Drop here" slot shifts the cards while the pointer moves, so the drop
// point is read again once the pointer is over the target.
async function dragCard(page, from, to) {
  // All four cards must be inside the viewport: a pointer outside it sends
  // no drag events.
  await page.setViewportSize({ width: 1280, height: 1400 });
  const handle = await card(page, from).locator(".p-handle").boundingBox();
  const hx = handle.x + handle.width / 2;
  const hy = handle.y + handle.height / 2;
  await page.mouse.move(hx, hy);
  await page.mouse.down();
  await page.mouse.move(hx, hy + 12, { steps: 4 });
  const near = await card(page, to).boundingBox();
  await page.mouse.move(near.x + 40, near.y + near.height / 2, { steps: 12 });
  const now = await card(page, to).boundingBox();
  await page.mouse.move(now.x + 40, now.y + now.height - 8, { steps: 4 });
  await page.mouse.up();
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
  await expect(card(page, 2).getByLabel("Account")).toHaveValue("go-1");
  await expect(card(page, 3).getByLabel("Account")).toHaveValue("go-2");
  await expect(page.getByRole("button", { name: "Stop load balancing" })).toHaveCount(0);
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
  await expect(card(page, 2).getByLabel("Account")).toHaveValue("go-1");
  await expect(card(page, "2a")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Stop load balancing" })).toHaveCount(0);
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

// ---------- creating a group: selection mode ----------

const lbStart = (page) => page.getByRole("button", { name: "⇉ Load balance…", exact: true });
const lbConfirm = (page) => page.getByRole("button", { name: /^Load balance \d+ models$/ });
const lbCancel = (page) => page.getByRole("button", { name: "Cancel", exact: true });
const pick = (page, n) => page.getByRole("checkbox", { name: `Select choice ${n}`, exact: true });

// holdState holds back every /api/state answer until the returned release()
// is called, so the page is not redrawn after an op (as in "a second edit
// before the redraw still applies").
async function holdState(page) {
  let hold = true;
  await page.route("**/api/state", async (route) => {
    while (hold) await new Promise((ok) => setTimeout(ok, 50));
    await route.continue();
  });
  return () => { hold = false; };
}

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
test("Load balance: Remove, Stop, + Add Model, fields and moves are inert while selecting", async ({ page }) => {
  await grouped(page);
  // Every control pressed below must be inside the viewport.
  await page.setViewportSize({ width: 1280, height: 1400 });
  await lbStart(page).click();
  for (const key of ["c0-remove", "c1-remove", "group-1-stop", "group-1-add", "add-choice"]) {
    const box = await page.locator(`[data-key="${key}"]`).boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  }
  const account = page.locator('[data-key="c0-account"]');
  await account.focus();
  await expect(account).not.toBeFocused();
  await expect(page.locator(".p-handle")).toHaveCount(0);
  await page.locator('[data-key="card-0"]').focus();
  const mod = (await page.evaluate(() => navigator.platform)).startsWith("Mac") ? "Meta" : "Control";
  await page.keyboard.press(`${mod}+ArrowDown`);
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
