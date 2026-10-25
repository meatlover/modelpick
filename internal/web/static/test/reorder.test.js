// Cases for the pure functions of js/reorder.js. In the browser,
// reorder.test.html writes the verdict into <output id="result">; elsewhere
// run() returns it.
import { dropAt, dropIndex, keyAction, keyGroupMove, keyMove, slotAbove } from "../js/reorder.js";

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

// GROUPED is coder with a group at position 2: 1, 2a, 2b, 3, 4 (flat 0–4).
const GROUPED = [{ start: 0, end: 1, group: false }, { start: 1, end: 3, group: true },
  { start: 3, end: 4, group: false }, { start: 4, end: 5, group: false }];
// THREE is a three-member group, then a plain card.
const THREE = [{ start: 0, end: 3, group: true }, { start: 3, end: 4, group: false }];
// TWO_GROUPS is two adjacent groups of two, then a plain card.
const TWO_GROUPS = [{ start: 0, end: 2, group: true }, { start: 2, end: 4, group: true },
  { start: 4, end: 5, group: false }];

// LIST is a plain card, a group's frame (header 100–120, members 120–280),
// and a plain card, by their midpoints. ADJACENT is two frames, back to back.
const LIST = [{ mid: 50, inner: null }, { mid: 200, inner: { top: 120, bottom: 280, mids: [160, 240] } },
  { mid: 350, inner: null }];
const ADJACENT = [{ mid: 150, inner: { top: 40, bottom: 260, mids: [90, 210] } },
  { mid: 450, inner: { top: 340, bottom: 560, mids: [390, 510] } }];

export const cases = [
  ["keyMove plain ↓ past a plain card", () => json(keyMove(GROUPED, 3, 1)), `{"from":3,"to":4,"group":null}`],
  ["keyMove plain ↓ past a whole group", () => json(keyMove(GROUPED, 0, 1)), `{"from":0,"to":2,"group":null}`],
  ["keyMove plain ↑ past a whole group", () => json(keyMove(GROUPED, 3, -1)), `{"from":3,"to":1,"group":null}`],
  ["keyMove plain ↑ at the top is nothing", () => keyMove(GROUPED, 0, -1), null],
  ["keyMove plain ↓ at the bottom is nothing", () => keyMove(GROUPED, 4, 1), null],
  ["keyMove first member ↑ moves out above", () => json(keyMove(GROUPED, 1, -1)), `{"from":1,"to":1,"group":null}`],
  ["keyMove last member ↓ moves out below", () => json(keyMove(GROUPED, 2, 1)), `{"from":2,"to":2,"group":null}`],
  ["keyMove last member ↑ stays in its group", () => json(keyMove(GROUPED, 2, -1)), `{"from":2,"to":1,"group":1}`],
  ["keyMove first member ↓ stays in its group", () => json(keyMove(GROUPED, 1, 1)), `{"from":1,"to":2,"group":1}`],
  ["keyMove middle member ↑ stays in its group", () => json(keyMove(THREE, 1, -1)), `{"from":1,"to":0,"group":0}`],
  ["keyMove middle member ↓ stays in its group", () => json(keyMove(THREE, 1, 1)), `{"from":1,"to":2,"group":0}`],
  ["keyMove first member of the first group ↑ moves out to the top",
    () => json(keyMove(THREE, 0, -1)), `{"from":0,"to":0,"group":null}`],
  ["keyMove member ↓ out between two groups lands plain",
    () => json(keyMove(TWO_GROUPS, 1, 1)), `{"from":1,"to":1,"group":null}`],
  ["keyMove member ↑ out between two groups lands plain",
    () => json(keyMove(TWO_GROUPS, 2, -1)), `{"from":2,"to":2,"group":null}`],
  ["keyMove plain ↑ past a group after a group", () => json(keyMove(TWO_GROUPS, 4, -1)), `{"from":4,"to":2,"group":null}`],
  ["keyGroupMove ↓ one position", () => json(keyGroupMove(GROUPED, 1, 1)), `{"index":1,"to":2,"start":2}`],
  ["keyGroupMove ↑ one position", () => json(keyGroupMove(GROUPED, 1, -1)), `{"index":1,"to":0,"start":0}`],
  ["keyGroupMove ↑ at the top is nothing", () => keyGroupMove(THREE, 0, -1), null],
  ["keyGroupMove ↓ at the bottom is nothing", () => keyGroupMove(TWO_GROUPS, 2, 1), null],
  ["keyGroupMove ↓ past a larger group", () => json(keyGroupMove(TWO_GROUPS, 0, 1)), `{"index":0,"to":1,"start":2}`],
  ["keyGroupMove ↓ past a smaller position", () => json(keyGroupMove(THREE, 0, 1)), `{"index":0,"to":1,"start":1}`],
  ["dropAt above everything", () => json(dropAt(10, LIST, false)), `{"k":0,"j":null}`],
  ["dropAt on a frame's header lands before it", () => json(dropAt(110, LIST, false)), `{"k":1,"j":null}`],
  ["dropAt on the first member's upper half joins at the start", () => json(dropAt(150, LIST, false)), `{"k":1,"j":0}`],
  ["dropAt between members joins between them", () => json(dropAt(200, LIST, false)), `{"k":1,"j":1}`],
  ["dropAt on the last member's lower half joins at the end", () => json(dropAt(270, LIST, false)), `{"k":1,"j":2}`],
  ["dropAt below the members (+ Add Model) lands after the frame", () => json(dropAt(290, LIST, false)),
    `{"k":2,"j":null}`],
  ["dropAt a whole group never lands inside a frame", () => json(dropAt(240, LIST, true)), `{"k":2,"j":null}`],
  ["dropAt adjacent groups: inside the first one's end", () => json(dropAt(250, ADJACENT, false)), `{"k":0,"j":2}`],
  ["dropAt adjacent groups: between them", () => json(dropAt(300, ADJACENT, false)), `{"k":1,"j":null}`],
  ["dropAt adjacent groups: inside the second one's start", () => json(dropAt(350, ADJACENT, false)),
    `{"k":1,"j":0}`],
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
