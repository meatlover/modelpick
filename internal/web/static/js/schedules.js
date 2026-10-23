// Schedules tab: the schedule list and the selected schedule's week grid
// (#schedules/<name>; the first schedule when the name is missing or
// unknown). The grid shows the UTC cells in the header's zone. A pointer
// drag, or Space on the keyboard cell, sends one schedule.paint; Clear
// sends one that turns every cell off. The title row's readout names the
// cell under the pointer (or the keyboard cell).
import { h, clear, trashIcon } from "./dom.js";
import { store, sendOp, notApplied } from "./api.js";
import { confirmDialog } from "./notice.js";
import { currentTab } from "./header.js";
import { zone, localOffsetMinutes, utcCellsFor, shownFraction } from "./time.js";

export const uses = ["state", "doctor"];

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MOVES = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };

let host = null;
// focusKey is the data-key of the element that had focus; paint puts focus
// back on it, since every repaint rebuilds the tab.
let focusKey = null;
let listening = false;
// The New schedule input is open (adding) holding newDraft; both survive
// a repaint.
let adding = false;
let newDraft = "";

// grid is the week grid's state, kept across repaints: the schedule and
// zone it is for, the keyboard cell (active, [day, hour] as shown), the Shift anchor,
// the pointer drag, and the rectangle whose paint op is in flight
// (pending), and the cell under the pointer (hover). el, cells and out
// (the readout) are the elements of the latest repaint.
const grid = { name: null, zone: null, active: [0, 0], anchor: null, drag: null, pending: null,
  hover: null, el: null, cells: [], out: null };

function offset() {
  return zone() === "local" ? localOffsetMinutes() : 0;
}

function shown(sched, [d, hr]) {
  return Math.min(1, Math.max(0, shownFraction(sched.cells, d, hr, offset())));
}

// rect is the rectangle between two shown cells, inclusive.
function rect([d1, h1], [d2, h2]) {
  return { d0: Math.min(d1, d2), d1: Math.max(d1, d2), h0: Math.min(h1, h2), h1: Math.max(h1, h2) };
}

function inRect(r, d, hr) {
  return r !== null && d >= r.d0 && d <= r.d1 && hr >= r.h0 && hr <= r.h1;
}

// utcPairs lists the distinct UTC cells under the shown rectangle.
function utcPairs(r) {
  const seen = new Map();
  for (let d = r.d0; d <= r.d1; d++) {
    for (let hr = r.h0; hr <= r.h1; hr++) {
      for (const c of utcCellsFor(d, hr, offset())) seen.set(c.join(","), c);
    }
  }
  return [...seen.values()];
}

function previewRect() {
  if (grid.drag) return rect(grid.drag.start, grid.drag.end);
  if (grid.pending) return grid.pending;
  return grid.anchor ? rect(grid.anchor, grid.active) : null;
}

// cellLabel names a shown cell: "Tue 14:00–15:00".
function cellLabel([d, hr]) {
  const hh = (n) => `${String(n).padStart(2, "0")}:00`;
  return `${DAYS[d]} ${hh(hr)}–${hh(hr + 1)}`;
}

// keyboardFocus: the grid has focus and shows the active cell's ring
// (:focus-visible, as in app.css).
function keyboardFocus() {
  try {
    return grid.el !== null && grid.el.matches(":focus-visible");
  } catch {
    return grid.el !== null && document.activeElement === grid.el;
  }
}

// readout shows the cell under a drag or the pointer, else the keyboard
// cell while it is shown, else nothing.
function readout() {
  if (!grid.out) return;
  let c = grid.drag ? grid.drag.end : grid.hover;
  if (!c && keyboardFocus()) c = grid.active;
  grid.out.textContent = c ? cellLabel(c) : "";
}

// mark redraws the preview, the active cell and the readout on the
// current cells without rebuilding them.
function mark() {
  const r = previewRect();
  grid.cells.forEach((col, d) => col.forEach((el, hr) => {
    el.classList.toggle("is-preview", inRect(r, d, hr));
    el.classList.toggle("is-active", d === grid.active[0] && hr === grid.active[1]);
  }));
  readout();
}

function current() {
  return store.state ? store.state.schedules.find((s) => s.name === grid.name) || null : null;
}

// apply paints the rectangle: on when the start cell is not full, off when
// it is. The preview stays until the op (and its reload) has settled.
async function apply(r, start) {
  const sched = current();
  if (!sched) return;
  const on = shown(sched, start) < 1;
  grid.pending = r;
  mark();
  await sendOp("schedule.paint", { name: sched.name, cells: utcPairs(r), on });
  grid.pending = null;
  mark();
}

// ---------- pointer ----------

// index is the last of the boxes starting at or before pos (0 before all).
function index(boxes, pos) {
  let i = 0;
  boxes.forEach((b, k) => { if (b <= pos) i = k; });
  return i;
}

// cellAt maps a pointer position to the nearest shown cell, so a drag past
// the grid's edge keeps the edge row or column.
function cellAt(x, y) {
  const cols = grid.cells.map((col) => col[0].getBoundingClientRect().left);
  const rows = grid.cells[0].map((el) => el.getBoundingClientRect().top);
  return [index(cols, x), index(rows, y)];
}

// hoverAt tracks the cell under the pointer; a gap between cells counts
// as the cell before it, so the readout does not flicker.
function hoverAt(e) {
  if (!grid.cells.length) return;
  const first = grid.cells[0][0].getBoundingClientRect();
  const last = grid.cells[6][23].getBoundingClientRect();
  const inside = e.clientX >= first.left && e.clientX < last.right && e.clientY >= first.top &&
    e.clientY < last.bottom;
  grid.hover = inside ? cellAt(e.clientX, e.clientY) : null;
  readout();
}

function unhover() {
  grid.hover = null;
  readout();
}

function startDrag(e) {
  const t = e.target.closest ? e.target.closest("[data-day]") : null;
  if (!t || e.button !== 0 || grid.pending) return;
  const start = [Number(t.dataset.day), Number(t.dataset.hour)];
  try {
    grid.el.setPointerCapture(e.pointerId); // the press itself focuses the grid
  } catch {
    // the pointer is already gone; the window listeners still end the drag
  }
  grid.drag = { id: e.pointerId, start, end: start };
  grid.active = start;
  grid.anchor = null;
  mark();
}

function moveDrag(e) {
  if (!grid.drag || e.pointerId !== grid.drag.id || !grid.el.isConnected) return;
  const end = cellAt(e.clientX, e.clientY);
  if (end[0] === grid.drag.end[0] && end[1] === grid.drag.end[1]) return;
  grid.drag.end = end;
  grid.active = end;
  mark();
}

function endDrag(e) {
  if (!grid.drag || e.pointerId !== grid.drag.id) return;
  const { start, end } = grid.drag;
  grid.drag = null;
  apply(rect(start, end), start);
}

function cancel() {
  if (!grid.drag && !grid.anchor) return false;
  grid.drag = null;
  grid.anchor = null;
  mark();
  return true;
}

// ---------- keyboard ----------

function clampMove([d, hr], [dd, dh]) {
  return [Math.min(6, Math.max(0, d + dd)), Math.min(23, Math.max(0, hr + dh))];
}

function onGridKey(e) {
  if (MOVES[e.key]) {
    e.preventDefault();
    if (!e.shiftKey) grid.anchor = null;
    else if (!grid.anchor) grid.anchor = grid.active;
    grid.active = clampMove(grid.active, MOVES[e.key]);
    mark();
  } else if (e.key === " ") {
    e.preventDefault();
    if (e.repeat || grid.pending || grid.drag) return;
    const start = grid.anchor || grid.active;
    grid.anchor = null;
    apply(rect(start, grid.active), start);
  }
}

function listen() {
  if (listening) return;
  listening = true;
  document.addEventListener("pointerdown", (e) => {
    if (currentTab() !== "schedules") return;
    const k = e.target.closest ? e.target.closest("[data-key]") : null;
    focusKey = k ? k.dataset.key : null;
  });
  document.addEventListener("focusin", (e) => {
    if (currentTab() !== "schedules") return;
    const t = e.target;
    if (t && t.dataset && t.dataset.key !== undefined) focusKey = t.dataset.key;
    else if (t !== document.body && t !== document.documentElement) focusKey = null;
  });
  window.addEventListener("pointermove", moveDrag);
  window.addEventListener("pointerup", endDrag);
  window.addEventListener("pointercancel", (e) => {
    if (!grid.drag || e.pointerId !== grid.drag.id) return;
    grid.drag = null;
    mark();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && currentTab() === "schedules" && cancel()) e.preventDefault();
  });
}

function restoreFocus() {
  if (focusKey === null) return;
  const a = document.activeElement;
  if (a && a !== document.body && !host.contains(a)) return;
  const el = [...host.querySelectorAll("[data-key]")].find((x) => x.dataset.key === focusKey);
  if (el && el !== a) el.focus({ preventScroll: true });
}

// ---------- the week grid ----------

function cell(sched, d, hr) {
  const f = shown(sched, [d, hr]);
  let fill = null;
  if (f > 0) {
    fill = h("span", { class: "s-fill" });
    fill.style.height = `${f * 100}%`;
  }
  const el = h("span", { class: "s-cell", "data-day": d, "data-hour": hr }, fill);
  grid.cells[d][hr] = el;
  return el;
}

function week(sched) {
  grid.cells = DAYS.map(() => []);
  const kids = [h("span", { "aria-hidden": "true" }), DAYS.map((d) => h("span", { class: "s-day" }, d))];
  for (let hr = 0; hr < 24; hr++) {
    kids.push(h("span", { class: "mono s-hour", "aria-hidden": "true" },
      hr % 3 === 0 ? String(hr).padStart(2, "0") : ""));
    for (let d = 0; d < 7; d++) kids.push(cell(sched, d, hr));
  }
  grid.el = h("div", { class: "s-grid", tabindex: "0", role: "group", "aria-label": "Week",
    "data-key": "week", onKeyDown: onGridKey, onPointerDown: startDrag, onPointerMove: hoverAt,
    onPointerLeave: unhover, onFocus: readout, onBlur: readout }, kids);
  mark();
  return h("section", { class: "panel s-week" }, grid.el);
}

// ---------- schedule list ----------

// errorSubjects is the set of doctor error subjects ("schedule s", …).
function errorSubjects() {
  const found = store.doctor ? store.doctor.findings || [] : [];
  return new Set(found.filter((f) => f.severity === "error").map((f) => f.subject));
}

function selected(schedules) {
  const rest = location.hash.slice(1).split("/").slice(1).join("/");
  let want = "";
  try {
    want = decodeURIComponent(rest);
  } catch {
    // a malformed name selects the first schedule
  }
  return schedules.find((s) => s.name === want) || schedules[0] || null;
}

function go(name) {
  const want = `#schedules/${encodeURIComponent(name)}`;
  if (location.hash === want) paint();
  else location.hash = want;
}

// afterBlur runs fn once a blur has settled, unless the input was removed
// by a repaint (its draft then lives on in the rebuilt input).
function afterBlur(el, fn) {
  setTimeout(() => {
    if (el.isConnected && el.value !== el.dataset.sent) fn();
  }, 0);
}

function closeNew(keepFocus) {
  adding = false;
  newDraft = "";
  if (keepFocus) focusKey = "new-schedule-btn";
  paint();
}

async function addSchedule(el) {
  const name = el.value.trim();
  if (!name) {
    closeNew(false);
    return;
  }
  el.dataset.sent = el.value;
  const r = await sendOp("schedule.add", { name });
  if (!r.ok) return; // the toast says why; the input keeps the name
  adding = false;
  newDraft = "";
  focusKey = `sl-${name}`;
  go(name);
}

function newInput() {
  return h("input", { type: "text", class: "input p-new-input", "aria-label": "New schedule name",
    "data-key": "new-schedule", value: newDraft,
    onInput: (e) => { newDraft = e.target.value; },
    onKeyDown: (e) => {
      if (e.key === "Enter") addSchedule(e.target);
      if (e.key === "Escape") closeNew(true);
    },
    onBlur: (e) => afterBlur(e.target, () => addSchedule(e.target)) });
}

function openNew() {
  adding = true;
  newDraft = "";
  focusKey = "new-schedule";
  paint();
}

// listRow is a schedule's link with its Delete button beside it (a link
// cannot hold a button); the button shows on hover or focus in the row.
function listRow(s, sel, red) {
  return h("div", { class: "s-row" },
    h("a", { href: `#schedules/${encodeURIComponent(s.name)}`,
      class: ["p-link", red.has(`schedule ${s.name}`) && "is-error"],
      "aria-current": s === sel ? "true" : null, "data-key": `sl-${s.name}` }, s.name),
    h("button", { type: "button", class: "s-del", "data-key": `sl-del-${s.name}`,
      "aria-label": `Delete schedule ${s.name}`, onClick: () => deleteSchedule(s.name) }, trashIcon()));
}

function scheduleList(schedules, sel, red) {
  return h("aside", { class: "p-list", "aria-label": "Schedules" },
    h("h2", { class: "p-list-title" }, "Schedules"),
    schedules.map((s) => listRow(s, sel, red)),
    adding ? newInput()
      : h("button", { type: "button", class: "btn btn-lg btn-dashed p-new", "data-key": "new-schedule-btn",
        onClick: openNew }, "+ New schedule"));
}

// deleteSchedule removes an unused schedule at once; a used one asks first,
// with the server's reason, and is then removed from every choice too.
async function deleteSchedule(name) {
  let r = await sendOp("schedule.remove", { name, force: false }, { quiet: true });
  if (!r.ok && r.uses) {
    if (!(await confirmDialog(r.reason, "Delete anyway"))) return;
    r = await sendOp("schedule.remove", { name, force: true });
  } else if (!r.ok && r.refused) {
    notApplied(r.reason);
  }
  if (r.ok && name === grid.name) history.replaceState(null, "", "#schedules");
  if (r.ok && focusKey === `sl-del-${name}`) focusKey = "new-schedule-btn";
  paint();
}

// ALL is every UTC cell, the cells a Clear turns off.
const ALL = DAYS.flatMap((_, d) => Array.from({ length: 24 }, (__, hr) => [d, hr]));

function painted(sched) {
  return sched.cells.some((row) => (row || []).some((v) => Number(v) > 0));
}

// clearSchedule turns every cell off, without asking (owner). The button
// is then disabled, so keyboard focus moves to the grid.
function clearSchedule(name) {
  if (focusKey === "clear-schedule") focusKey = "week";
  sendOp("schedule.paint", { name, cells: ALL, on: false });
}

function scheduleMain(sched) {
  grid.out = h("span", { class: "mono s-readout" });
  const title = h("div", { class: "p-title" }, h("h1", {}, sched.name),
    h("button", { type: "button", class: "btn btn-quiet", "data-key": "clear-schedule",
      "aria-label": `Clear schedule ${sched.name}`, disabled: !painted(sched),
      onClick: () => clearSchedule(sched.name) }, "Clear"),
    grid.out);
  return h("div", { class: "p-main" }, title, week(sched));
}

function paint() {
  if (!host || currentTab() !== "schedules") return;
  listen();
  const st = store.state;
  clear(host);
  grid.cells = [];
  if (!st) return;
  const sched = selected(st.schedules);
  grid.out = null;
  if (!sched || sched.name !== grid.name) {
    Object.assign(grid, { name: sched ? sched.name : null, active: [0, 0], anchor: null, drag: null,
      hover: null });
  }
  // A Shift selection is in shown cells, which a zone switch moves.
  if (zone() !== grid.zone) Object.assign(grid, { zone: zone(), anchor: null });
  host.append(h("div", { class: "p-body" },
    scheduleList(st.schedules, sched, errorSubjects()), sched && scheduleMain(sched)));
  restoreFocus();
}

export function render(root) {
  host = root;
  paint();
}
