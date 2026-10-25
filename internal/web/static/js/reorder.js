// Reordering choice cards: drag by the handle (HTML5 drag and drop, a
// dashed "Drop here" slot marks the landing place) or, on a focused card,
// ⌘↑/⌘↓ (Ctrl off a Mac); a focused group frame moves the same way. Plain
// ↑/↓ move focus through the cards and frames.
// The list is ol > li.p-item, each holding one .p-card[data-index] or a
// load balancing group's .p-group frame of several (ol.p-members >
// li.p-member > .p-card); data-index is the card's flat index. A card drops
// between items (it lands plain) or inside a frame between its members (it
// joins that group); a frame drags by its own handle and drops only between
// items, as one position.
import { h } from "./dom.js";

// dropIndex is where a dragged card lands among the other items: the number
// of them whose vertical midpoint (mids, top to bottom) is above y. With no
// group it is the `to` of choice.move, an index into the list without the
// dragged card; cardsBefore counts a group's members.
export function dropIndex(mids, y) {
  let n = 0;
  for (const m of mids) if (y > m) n++;
  return n;
}

// dropAt says where a drop at y lands among the other items (each
// {mid, inner}: inner is a frame's member area {top, bottom, mids} as laid
// out now, or null for a plain card): {k, j: null} lands between items,
// before items[k]; {k, j} lands inside items[k]'s frame, before its j-th
// other member. Over a frame's header or its + Add Model the drop is
// between items, so at the edge between two groups the slot the pointer
// is on decides. whole: a group is dragged, and a group never joins one.
export function dropAt(y, items, whole) {
  if (!whole) {
    for (let k = 0; k < items.length; k++) {
      const m = items[k].inner;
      if (m && y >= m.top && y <= m.bottom) return { k, j: dropIndex(m.mids, y) };
    }
  }
  return { k: dropIndex(items.map((it) => it.mid), y), j: null };
}

// slotAbove says whether the "Drop here" slot goes above the dragged card
// while the pointer (y) is over the card's own place: above when y is in the
// upper half of the card (top, height as laid out now), else below. Either
// side is a drop with no move. The slot shifts only the card itself, by the
// slot's height: above pushes the card down, so y lands higher in it; below
// leaves it, so the side chosen chooses itself again and does not flicker.
export function slotAbove(y, top, height) {
  return y < top + height / 2;
}

// keyAction maps a keydown on a card to {move: ±1}, {focus: ±1} or null.
// mac picks the modifier: ⌘ on a Mac, Ctrl elsewhere.
export function keyAction(e, mac) {
  const d = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
  if (!d || e.altKey || e.shiftKey) return null;
  const mod = mac ? e.metaKey : e.ctrlKey;
  const other = mac ? e.ctrlKey : e.metaKey;
  if (other) return null;
  return mod ? { move: d } : { focus: d };
}

// spans are the list's positions, top to bottom: {start, end, group}, end
// exclusive, group true for a load balancing group's run of members.

// keyMove is the choice.move for ⌘↑/⌘↓ (d = -1/1) on the card at flat
// index i: {from, to, group}, with group null (it lands plain) or the flat
// index of a member of the group it stays in; null at the list's edge. A
// member at its frame's edge (in the direction d) moves out, plain, just
// past that edge; any other member moves within its group; a plain card
// moves past the next position, a whole group included, never into it.
export function keyMove(spans, i, d) {
  const n = spans.findIndex((s) => i >= s.start && i < s.end);
  const s = spans[n];
  if (s.group) {
    if (i === (d < 0 ? s.start : s.end - 1)) return { from: i, to: i, group: null };
    return { from: i, to: i + d, group: s.start };
  }
  const t = spans[n + d];
  if (!t) return null;
  return { from: i, to: d < 0 ? t.start : t.end - 1, group: null };
}

// keyGroupMove is the group.move for ⌘↑/⌘↓ (d = -1/1) on the group at
// position n: {index, to, start} (index: its first member; to: the
// position it goes to; start: its first member's flat index after the
// move), or null at the list's edge.
export function keyGroupMove(spans, n, d) {
  const t = spans[n + d];
  if (!t) return null;
  const size = spans[n].end - spans[n].start;
  return { index: spans[n].start, to: n + d, start: d < 0 ? t.start : t.end - size };
}

function isMac() {
  return String(navigator.platform || "").startsWith("Mac");
}

// drag is the drag in progress: el, the dragged .p-card, or a group's
// .p-group when whole; li, the item holding it; frame, the group the card
// is a member of (or null); from, the card's flat index (whole: the
// group's position) and index, the group's first member; slot; and at,
// where a drop would land now (target).
let drag = null;

// armed is the card or frame made draggable by a press on its handle; one
// set of document listeners disarms it when the press ends without a drag.
// The check waits a task because Chromium may send pointercancel around
// dragstart, and a drag in progress keeps its element until endDrag.
let armed = null;
let disarming = false;

function disarmSoon() {
  setTimeout(() => {
    if (!armed || (drag && drag.el === armed)) return;
    armed.draggable = false;
    armed = null;
  }, 0);
}

function listenDisarm() {
  if (disarming) return;
  disarming = true;
  for (const type of ["pointerup", "pointercancel", "dragend"]) document.addEventListener(type, disarmSoon);
}

// dragHandle makes el (a card, or a group's frame) draggable only while the
// pointer is down on handle, so selects and text inside keep their own
// pointer behaviour.
export function dragHandle(el, handle) {
  listenDisarm();
  handle.addEventListener("pointerdown", () => {
    if (armed && armed !== el) armed.draggable = false;
    armed = el;
    el.draggable = true;
  });
}

function endDrag() {
  if (!drag) return;
  drag.slot.remove();
  drag.el.classList.remove("is-dragging");
  drag.el.draggable = false;
  if (armed === drag.el) armed = null;
  drag = null;
}

// items are the list's positions' items, top to bottom.
function items(ol) {
  return [...ol.children].filter((li) => li.classList.contains("p-item"));
}

// otherMembers are a frame's member items, the dragged card's left out.
function otherMembers(frame) {
  return [...frame.querySelector(".p-members").children]
    .filter((li) => li.classList.contains("p-member") && li !== drag.li);
}

// cardsBefore is the `to` of choice.move for a drop before items[k]: the
// cards the items above it hold (a group's item holds several), the dragged
// card left out.
function cardsBefore(list, k) {
  let n = 0;
  for (const li of list.slice(0, k)) n += [...li.querySelectorAll(".p-card")].filter((c) => c !== drag.el).length;
  return n;
}

function midOf(el) {
  const r = el.getBoundingClientRect();
  return r.top + r.height / 2;
}

// layout is each of list as dropAt reads it, as laid out now.
function layout(list) {
  return list.map((li) => {
    const frame = li.querySelector(".p-group");
    if (!frame) return { mid: midOf(li), inner: null };
    const r = frame.querySelector(".p-members").getBoundingClientRect();
    return { mid: midOf(li), inner: { top: r.top, bottom: r.bottom, mids: otherMembers(frame).map(midOf) } };
  });
}

// target is the landing for dropAt's answer at among list (the other
// items): {to, start, frame, parent, before}. to is choice.move's (whole:
// group.move's, a position); start is the dragged group's first flat index
// after the move; frame is the group the card joins, or null; the slot goes
// into parent before before.
function target(ol, list, at) {
  const cards = cardsBefore(list, at.k);
  if (at.j === null) {
    return { to: drag.whole ? at.k : cards, start: cards, frame: null, parent: ol, before: list[at.k] || null };
  }
  const frame = list[at.k].querySelector(".p-group");
  return { to: cards + at.j, frame, parent: frame.querySelector(".p-members"),
    before: otherMembers(frame)[at.j] || null };
}

// stays says whether landing t is the dragged item's own place: no move.
function stays(d, t) {
  return t.to === d.from && t.frame === d.frame;
}

// place moves the slot to where a drop at y would land.
function place(ol, y) {
  const list = items(ol).filter((li) => li !== drag.li);
  const t = target(ol, list, dropAt(y, layout(list), drag.whole));
  drag.at = t;
  let before = t.before;
  // Landing in its own place (t.parent then holds drag.li): the slot goes
  // above or below the dragged item.
  if (stays(drag, t)) {
    const r = drag.li.getBoundingClientRect();
    if (slotAbove(y, r.top, r.height)) before = drag.li;
  }
  if (drag.slot.parentNode !== t.parent || drag.slot.nextSibling !== before) t.parent.insertBefore(drag.slot, before);
}

function startDrag(e) {
  const el = e.target;
  if (!el.classList || !el.draggable) return;
  const whole = el.classList.contains("p-group");
  if (!whole && !el.classList.contains("p-card")) return;
  const li = el.closest("li");
  const index = Number((whole ? el.querySelector(".p-card") : el).dataset.index);
  const from = whole ? items(li.parentNode).indexOf(li) : index;
  drag = { el, li, whole, frame: whole ? null : el.closest(".p-group"), from, index, at: null,
    slot: h("li", { class: "p-slot", "aria-hidden": "true" }, "Drop here") };
  e.dataTransfer.effectAllowed = "move";
  e.dataTransfer.setData("text/plain", String(from));
  el.classList.add("is-dragging");
}

// land sends the move for drag d's drop, unless it lands in its own place.
// A card joining a group names it by another member's flat index.
function land(d, on) {
  const t = d.at;
  if (!t || stays(d, t)) return;
  if (d.whole) {
    on.group(d.index, t.to, t.start);
    return;
  }
  const member = t.frame ? [...t.frame.querySelectorAll(".p-card")].find((c) => c !== d.el) : null;
  on.card(d.from, t.to, member ? Number(member.dataset.index) : null);
}

function wireDrag(ol, on) {
  ol.addEventListener("dragstart", startDrag);
  ol.addEventListener("dragover", (e) => {
    if (!drag || !ol.contains(drag.el)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    place(ol, e.clientY);
  });
  ol.addEventListener("drop", (e) => {
    if (!drag || !ol.contains(drag.el)) return;
    e.preventDefault();
    const d = drag;
    endDrag();
    land(d, on);
  });
  // A drop outside the list, or Escape, ends the drag with no drop.
  ol.addEventListener("dragend", endDrag);
}

// spansOf reads the list's spans (see keyMove) from its cards.
function spansOf(ol) {
  return items(ol).map((li) => {
    const idx = [...li.querySelectorAll(".p-card")].map((c) => Number(c.dataset.index));
    return { start: idx[0], end: idx[idx.length - 1] + 1, group: li.querySelector(".p-group") !== null };
  });
}

// focusNext moves focus d (±1) through the cards and group frames, in the
// order they are drawn.
function focusNext(ol, el, d) {
  const all = [...ol.querySelectorAll(".p-card, .p-group")];
  const next = all[all.indexOf(el) + d];
  if (next) next.focus();
}

function moveByKey(ol, el, d, on) {
  const spans = spansOf(ol);
  if (el.classList.contains("p-group")) {
    const m = keyGroupMove(spans, items(ol).indexOf(el.closest("li")), d);
    if (m) on.group(m.index, m.to, m.start);
    return;
  }
  const m = keyMove(spans, Number(el.dataset.index), d);
  if (m) on.card(m.from, m.to, m.group);
}

function wireKeys(ol, on) {
  ol.addEventListener("keydown", (e) => {
    const el = e.target;
    // Only a focused card or frame acts; inputs, selects and buttons keep
    // their keys.
    if (!el.classList || !(el.classList.contains("p-card") || el.classList.contains("p-group"))) return;
    const act = keyAction(e, isMac());
    if (!act) return;
    e.preventDefault();
    if (act.focus) focusNext(ol, el, act.focus);
    else moveByKey(ol, el, act.move, on);
  });
}

// wireReorder calls on.card(from, to, group) when a card is dropped in a
// new place or moved with the keyboard (choice.move's args: group is null
// or the flat index of a member of the group it lands in), and
// on.group(index, to, start) when a group is (group.move's args: index is
// its first member; start is that member's flat index after the move).
export function wireReorder(ol, on) {
  wireDrag(ol, on);
  wireKeys(ol, on);
}
