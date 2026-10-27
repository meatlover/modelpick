// The compact Profiles card (owner, 2026-10-24), measured in a real
// browser: two rows (dropdowns; caps and schedules), the field labels once
// above the list, Remove level with the dropdowns, the cap editor floating
// over the card, and the schedules at the right end of the caps line,
// right-aligned with Remove (owner, 2026-10-25), or, when they do not fit
// there, at the left of their own line.
const { test, expect } = require("@playwright/test");
const { startServer, login } = require("./helpers");
const { handEdit, loadBalanceGo } = require("./config");

let server;

test.beforeEach(async () => {
  server = await startServer("live");
});

test.afterEach(async () => {
  if (server) await server.stop();
  server = null;
});

async function profiles(page) {
  await login(page, server.url);
  await page.getByRole("link", { name: "Profiles", exact: true }).click();
  await expect(card(page, 1)).toBeVisible();
}

const card = (page, n) => page.getByRole("group", { name: `Choice ${n}`, exact: true });
const rect = (loc) => loc.evaluate((el) => {
  const r = el.getBoundingClientRect();
  return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, width: r.width, height: r.height };
});
const mid = (r) => r.top + r.height / 2;

test("the field labels show once, above the cards", async ({ page }) => {
  await profiles(page);
  const cols = page.locator(".p-cols");
  await expect(cols.getByText("Account", { exact: true })).toBeVisible();
  await expect(cols.getByText("Thinking", { exact: true })).toBeVisible();
  const c1 = card(page, 1);
  // No label line above the dropdowns: they start at the card's padding.
  const top = (await rect(c1)).top;
  expect((await rect(c1.getByLabel("Account"))).top - top).toBeLessThanOrEqual(14);
  // Each column's label sits over its dropdown.
  for (const name of ["Account", "Model", "Thinking"]) {
    const label = await rect(cols.getByText(name, { exact: true }));
    expect(Math.abs(label.left - (await rect(c1.getByLabel(name))).left)).toBeLessThanOrEqual(1);
  }
});

test("Remove sits level with the dropdowns", async ({ page }) => {
  await profiles(page);
  const c1 = card(page, 1);
  const remove = await rect(c1.getByRole("button", { name: "Remove choice 1" }));
  expect(Math.abs(mid(remove) - mid(await rect(c1.getByLabel("Account"))))).toBeLessThanOrEqual(1);
});

test("a card is two rows: dropdowns, then caps and schedules on one line", async ({ page }) => {
  await profiles(page);
  const c2 = card(page, 2);
  const box = await rect(c2.getByRole("textbox", { name: "5h cap" }));
  const add = await rect(c2.getByRole("button", { name: "+ schedule" }));
  const nocaps = await rect(c2.getByRole("button", { name: "Remove all caps" }));
  expect(box.height).toBeLessThanOrEqual(32); // the box's value line, inside a 32 px box
  expect(Math.abs(mid(add) - mid(nocaps))).toBeLessThanOrEqual(1);
  const remove = await rect(c2.getByRole("button", { name: "Remove choice 2" }));
  expect(Math.abs(add.right - remove.right), "+ schedule right-aligned with Remove").toBeLessThanOrEqual(1);
  expect((await rect(c2)).height).toBeLessThanOrEqual(110);
});

test("opening a cap editor keeps the card's height; the editor floats over it", async ({ page }) => {
  await profiles(page);
  const c1 = card(page, 1);
  const before = await rect(c1);
  await c1.getByRole("textbox", { name: "5h cap" }).click();
  const frame = c1.locator(".cap-edit");
  await expect(frame).toBeVisible();
  expect((await rect(c1)).height).toBe(before.height);
  const f = await rect(frame);
  expect(f.width).toBe(224);
  expect(f.bottom).toBeGreaterThan(before.bottom); // it reaches past the card
  // On top: the point under the editor's bottom edge belongs to it, not to
  // the card below.
  const onTop = await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y).closest(".cap-edit"),
    [f.left + 20, f.bottom - 6]);
  expect(onTop).toBe(true);
});

test("an editor opened near the window's bottom is scrolled into view", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 640 });
  await profiles(page);
  const box = card(page, 3).getByRole("textbox", { name: "5h cap" });
  await box.scrollIntoViewIfNeeded();
  const r0 = await rect(box);
  // Bring the box to the bottom edge, so its editor would end below it.
  await page.evaluate((b) => window.scrollBy(0, b - innerHeight + 4), r0.bottom);
  await box.click();
  const f = await rect(card(page, 3).locator(".cap-edit"));
  const vh = await page.evaluate(() => innerHeight);
  expect(f.bottom).toBeLessThanOrEqual(vh);
  expect(f.top).toBeGreaterThanOrEqual(0);
});

// Which card fits depends on the font (CI's Linux font is wider than
// macOS's), so each case is far from the edge: card 1's two caps and one
// short schedule fit with room to spare at 1280 px; card 3's three caps and
// two schedules cannot fit at 760 px (since the line reaches under Remove,
// they fit at 900 px by 16 px on macOS). The width check says when a case's
// setup, not the layout, is what changed.
test("schedules: right end of the caps line, else left of their own line", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, (c) => {
    c.profiles.coder[0].skip_during = ["night"];
    c.profiles.coder[2].skip_during = ["deepseek-peak", "night"];
  });
  for (const [width, n, ownLine] of [[1280, 1, false], [760, 3, true]]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.reload();
    await page.getByRole("link", { name: "Profiles", exact: true }).click();
    const c = card(page, n);
    const caps = await rect(c.locator(".p-caps"));
    const chips = await rect(c.locator(".p-chips"));
    const foot = await rect(c.locator(".p-card-foot"));
    const remove = await rect(c.getByRole("button", { name: `Remove choice ${n}` }));
    const fits = caps.width + 10 + chips.width <= foot.width;
    expect(fits, `${width}px, card ${n}: the setup ${ownLine ? "fits, so cannot test wrapping" : "no longer fits"}`)
      .toBe(!ownLine);
    if (ownLine) {
      expect(chips.top, `${width}px: below the caps`).toBeGreaterThanOrEqual(caps.bottom);
      expect(Math.abs(chips.left - foot.left), `${width}px: at the left`).toBeLessThanOrEqual(1);
    } else {
      expect(Math.abs(chips.top - caps.top), `${width}px: on the caps line`).toBeLessThanOrEqual(1);
      expect(Math.abs(chips.right - remove.right), `${width}px: right-aligned with Remove`).toBeLessThanOrEqual(1);
    }
  }
});

// A member card is a card like any other: inside the group's frame it keeps
// the labels over its dropdowns, Remove level with them, and two rows.
test("member cards keep the single-card layout rules", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, loadBalanceGo);
  await page.reload();
  await page.getByRole("link", { name: "Profiles", exact: true }).click();
  const m = card(page, "2b");
  await expect(m).toBeVisible();
  const cols = page.locator(".p-cols");
  for (const name of ["Account", "Model", "Thinking"]) {
    const label = await rect(cols.getByText(name, { exact: true }));
    expect(Math.abs(label.left - (await rect(m.getByLabel(name))).left), `${name} over its dropdown`)
      .toBeLessThanOrEqual(1);
  }
  const remove = await rect(m.getByRole("button", { name: "Remove choice 2b" }));
  expect(Math.abs(mid(remove) - mid(await rect(m.getByLabel("Account"))))).toBeLessThanOrEqual(1);
  const add = await rect(m.getByRole("button", { name: "+ schedule" }));
  const nocaps = await rect(m.getByRole("button", { name: "Remove all caps" }));
  expect(Math.abs(mid(add) - mid(nocaps))).toBeLessThanOrEqual(1);
  expect(Math.abs(add.right - remove.right), "+ schedule right-aligned with Remove").toBeLessThanOrEqual(1);
  expect((await rect(m)).height).toBeLessThanOrEqual(110);
  // The member's edges are a plain card's: the frame sits around them.
  const plain = await rect(card(page, 1));
  const r = await rect(m);
  expect(Math.abs(r.left - plain.left)).toBeLessThanOrEqual(1);
  expect(Math.abs(r.right - plain.right)).toBeLessThanOrEqual(1);
  const frame = await rect(card(page, 2));
  expect(frame.left).toBeLessThan(r.left);
  expect(frame.right).toBeGreaterThan(r.right);
});

// Remove, Remove all caps, + schedule and + caps rest half transparent and
// grey; they take full opacity and their colour while the pointer is over
// their card, or focus is inside it (owner, 2026-10-27).
const look = (loc) => () => loc.evaluate((el) => {
  const s = getComputedStyle(el);
  return `${s.opacity} ${s.filter}`;
});
const REST = "0.5 grayscale(1)";
const LIVE = "1 none";

test("a card's Remove, Remove all caps, + schedule and + caps rest dim and grey, full on hover or focus", async ({ page }) => {
  await profiles(page);
  const c2 = card(page, 2);
  const c4 = card(page, 4);
  const onC2 = [
    c2.getByRole("button", { name: "Remove choice 2" }),
    c2.getByRole("button", { name: "Remove all caps" }),
    c2.getByRole("button", { name: "+ schedule" }),
  ];
  const addCaps = c4.getByRole("button", { name: "Add caps" });
  await page.mouse.move(0, 0);
  for (const b of [...onC2, addCaps]) await expect.poll(look(b)).toBe(REST);
  await c2.hover();
  for (const b of onC2) await expect.poll(look(b)).toBe(LIVE);
  await expect.poll(look(addCaps), "another card's + caps stays at rest").toBe(REST);
  await page.mouse.move(0, 0);
  await c4.focus();
  await expect.poll(look(addCaps), "focus inside the card shows it").toBe(LIVE);
});

// Every Remove is a danger ghost: red frame and red text, an x before the
// word. Remove all caps is a plain text link, nothing like a cap box.
test("Remove is a red ghost button; Remove all caps a text link", async ({ page }) => {
  await profiles(page);
  const c2 = card(page, 2);
  const look = (loc) => loc.evaluate((el) => {
    const s = getComputedStyle(el);
    return { color: s.color, border: s.borderTopColor, width: s.borderTopWidth, line: s.textDecorationLine,
      icon: Boolean(el.querySelector("svg")) };
  });
  const remove = await look(c2.getByRole("button", { name: "Remove choice 2" }));
  expect(remove).toMatchObject({ color: "rgb(110, 20, 20)", border: "rgb(155, 44, 44)", icon: true });
  const all = await look(c2.getByRole("button", { name: "Remove all caps" }));
  expect(all).toMatchObject({ width: "0px", line: "underline", icon: false });
});

// The selected card's blue frame shows under the pointer, not after a
// click; the keyboard still shows it on the card it reaches. Member cards
// of a group do the same, and a group's frame is grey, not blue (owner,
// 2026-10-27).
const frameOf = (loc) => () => loc.evaluate((el) => {
  const s = getComputedStyle(el);
  return s.borderTopColor === "rgb(43, 95, 208)" && s.boxShadow.includes("rgb(201, 216, 246)");
});

test("a card's blue frame shows on hover and keyboard focus, not on a click", async ({ page }) => {
  await profiles(page);
  const c1 = card(page, 1);
  await c1.click({ position: { x: 4, y: 4 } });
  await expect(c1).toBeFocused();
  await page.mouse.move(0, 0);
  await expect.poll(frameOf(c1), "clicked, pointer away: no frame").toBe(false);
  await c1.hover();
  await expect.poll(frameOf(c1), "under the pointer").toBe(true);
  await page.mouse.move(0, 0);
  await c1.press("ArrowDown");
  await expect(card(page, 2)).toBeFocused();
  await expect.poll(frameOf(card(page, 2)), "reached by the keyboard").toBe(true);
});

test("a member card's frame shows on hover; the group's frame is grey", async ({ page }) => {
  await login(page, server.url);
  handEdit(server.configPath, loadBalanceGo);
  await page.reload();
  await page.getByRole("link", { name: "Profiles", exact: true }).click();
  const m = card(page, "2b");
  await m.click({ position: { x: 4, y: 4 } });
  await page.mouse.move(0, 0);
  await expect.poll(frameOf(m), "clicked, pointer away: no frame").toBe(false);
  await m.hover();
  await expect.poll(frameOf(m), "under the pointer").toBe(true);
  const group = await card(page, 2).evaluate((el) => {
    const s = getComputedStyle(el);
    return { bg: s.backgroundColor, border: s.borderTopColor };
  });
  expect(group).toEqual({ bg: "rgb(241, 240, 235)", border: "rgb(222, 221, 215)" });
});
