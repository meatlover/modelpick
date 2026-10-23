// Reordering choice cards: drag by the handle (HTML5 drag and drop, a
// dashed "Drop here" slot marks the landing place) or, on a focused card,
// ⌘↑/⌘↓ (Ctrl off a Mac). Plain ↑/↓ move focus between cards.
// The list is ol > li.p-item > .p-card[data-index].
import { h } from "./dom.js";

// dropIndex is where a dragged card lands among the other cards: the number
// of them whose vertical midpoint (mids, top to bottom) is above y. It is
// the `to` of choice.move, an index into the list without the dragged card.
export function dropIndex(mids, y) {
  let n = 0;
  for (const m of mids) if (y > m) n++;
  return n;
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

function isMac() {
  return String(navigator.platform || "").startsWith("Mac");
}

// drag is the drag in progress: {card, li, from, to, slot}.
let drag = null;

// armed is the card made draggable by a press on its handle; one set of
// document listeners disarms it when the press ends without a drag. The
// check waits a task because Chromium may send pointercancel around
// dragstart, and a drag in progress keeps its card until endDrag.
let armed = null;
let disarming = false;

function disarmSoon() {
  setTimeout(() => {
    if (!armed || (drag && drag.card === armed)) return;
    armed.draggable = false;
    armed = null;
  }, 0);
}

function listenDisarm() {
  if (disarming) return;
  disarming = true;
  for (const type of ["pointerup", "pointercancel", "dragend"]) document.addEventListener(type, disarmSoon);
}

// dragHandle makes card draggable only while the pointer is down on handle,
// so selects and text inside the card keep their own pointer behaviour.
export function dragHandle(card, handle) {
  listenDisarm();
  handle.addEventListener("pointerdown", () => {
    if (armed && armed !== card) armed.draggable = false;
    armed = card;
    card.draggable = true;
  });
}

function endDrag() {
  if (!drag) return;
  drag.slot.remove();
  drag.card.classList.remove("is-dragging");
  drag.card.draggable = false;
  if (armed === drag.card) armed = null;
  drag = null;
}

function others(ol) {
  return [...ol.children].filter((li) => li.classList.contains("p-item") && li !== drag.li);
}

// place moves the slot to where a drop at y would land.
function place(ol, y) {
  const items = others(ol);
  const mids = items.map((li) => {
    const r = li.getBoundingClientRect();
    return r.top + r.height / 2;
  });
  drag.to = dropIndex(mids, y);
  // Landing in its own place: show the slot just above the dragged card.
  const before = drag.to === drag.from ? drag.li : items[drag.to] || null;
  if (drag.slot.parentNode !== ol || drag.slot.nextSibling !== before) ol.insertBefore(drag.slot, before);
}

function startDrag(e) {
  const card = e.target;
  if (!card.classList || !card.classList.contains("p-card") || !card.draggable) return;
  const from = Number(card.dataset.index);
  drag = { card, li: card.closest("li"), from, to: from,
    slot: h("li", { class: "p-slot", "aria-hidden": "true" }, "Drop here") };
  e.dataTransfer.effectAllowed = "move";
  e.dataTransfer.setData("text/plain", String(from));
  card.classList.add("is-dragging");
}

function wireDrag(ol, onMove) {
  ol.addEventListener("dragstart", startDrag);
  ol.addEventListener("dragover", (e) => {
    if (!drag || !ol.contains(drag.card)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    place(ol, e.clientY);
  });
  ol.addEventListener("drop", (e) => {
    if (!drag || !ol.contains(drag.card)) return;
    e.preventDefault();
    const { from, to } = drag;
    endDrag();
    if (to !== from) onMove(from, to);
  });
  // A drop outside the list, or Escape, ends the drag with no drop.
  ol.addEventListener("dragend", endDrag);
}

function wireKeys(ol, onMove) {
  ol.addEventListener("keydown", (e) => {
    const card = e.target;
    // Only a focused card acts; inputs, selects and buttons keep their keys.
    if (!card.classList || !card.classList.contains("p-card")) return;
    const act = keyAction(e, isMac());
    if (!act) return;
    e.preventDefault();
    const cards = [...ol.querySelectorAll(".p-card")];
    const i = cards.indexOf(card);
    const j = i + (act.move || act.focus);
    if (j < 0 || j >= cards.length) return;
    if (act.focus) cards[j].focus();
    else onMove(i, j);
  });
}

// wireReorder calls onMove(from, to) when a card is dropped in a
// new place or moved with the keyboard.
export function wireReorder(ol, onMove) {
  wireDrag(ol, onMove);
  wireKeys(ol, onMove);
}
