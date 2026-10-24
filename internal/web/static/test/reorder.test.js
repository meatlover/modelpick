// Cases for the pure functions of js/reorder.js. In the browser,
// reorder.test.html writes the verdict into <output id="result">; elsewhere
// run() returns it.
import { dropIndex, keyAction, slotAbove } from "../js/reorder.js";

const json = (x) => JSON.stringify(x);

// key builds a keydown-like object.
const key = (k, mods = {}) => ({ key: k, altKey: false, shiftKey: false, metaKey: false, ctrlKey: false, ...mods });

// settles models a drag over the dragged card's own place: the slot (shift
// px including its gap) sits above the card, pushing it down, or below it,
// leaving it at top. From either side, the side chosen at y must choose
// itself again once the slot has moved there; otherwise the slot flickers.
// 68 = the slot (56 px) plus the list gap (12 px).
function settles(top, height, shift) {
  const pick = (above, y) => slotAbove(y, top + (above ? shift : 0), height);
  const bad = [];
  for (let y = top - 20; y <= top + shift + height + 20; y++) {
    for (const start of [true, false]) {
      const side = pick(start, y);
      if (pick(side, y) !== side) bad.push(`${start ? "above" : "below"}@${y}`);
    }
  }
  return bad.length ? bad.join(",") : "stable";
}

export const cases = [
  ["dropIndex above all", () => dropIndex([50, 150, 250], 10), 0],
  ["dropIndex between first and second", () => dropIndex([50, 150, 250], 100), 1],
  ["dropIndex below all", () => dropIndex([50, 150, 250], 400), 3],
  ["dropIndex on a midpoint stays above it", () => dropIndex([50, 150], 150), 1],
  ["dropIndex no other cards", () => dropIndex([], 100), 0],
  ["keyAction ↓ focuses next", () => json(keyAction(key("ArrowDown"), true)), `{"focus":1}`],
  ["keyAction ↑ focuses previous", () => json(keyAction(key("ArrowUp"), false)), `{"focus":-1}`],
  ["keyAction ⌘↑ moves on a Mac", () => json(keyAction(key("ArrowUp", { metaKey: true }), true)), `{"move":-1}`],
  ["keyAction Ctrl↓ moves off a Mac", () => json(keyAction(key("ArrowDown", { ctrlKey: true }), false)), `{"move":1}`],
  ["keyAction Ctrl↓ on a Mac is nothing", () => keyAction(key("ArrowDown", { ctrlKey: true }), true), null],
  ["keyAction ⌘↓ off a Mac is nothing", () => keyAction(key("ArrowDown", { metaKey: true }), false), null],
  ["keyAction ⇧↓ is nothing", () => keyAction(key("ArrowDown", { shiftKey: true }), true), null],
  ["keyAction ⌥↑ is nothing", () => keyAction(key("ArrowUp", { altKey: true }), true), null],
  ["keyAction other key is nothing", () => keyAction(key("Enter"), true), null],
  ["slotAbove upper half", () => slotAbove(120, 100, 80), true],
  ["slotAbove lower half", () => slotAbove(160, 100, 80), false],
  ["slotAbove exactly the midpoint is below", () => slotAbove(140, 100, 80), false],
  ["slotAbove just above the midpoint", () => slotAbove(139, 100, 80), true],
  ["slotAbove above the card (on the slot)", () => slotAbove(60, 100, 80), true],
  ["slotAbove below the card (on the slot)", () => slotAbove(200, 100, 80), false],
  ["no flicker, tall card", () => settles(100, 180, 68), "stable"],
  ["no flicker, card shorter than the slot", () => settles(100, 40, 68), "stable"],
];

function check([name, fn, want]) {
  let got;
  try {
    got = fn();
  } catch (e) {
    got = "threw " + e.message;
  }
  return got === want ? null : `${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`;
}

// run returns "PASS n" or "FAIL <case>; <case>…".
export function run() {
  const failed = cases.map(check).filter(Boolean);
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${cases.length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
