// Caps on a choice card (spec 8.3 "Caps on a card", mockup
// CapsEditor.dc.html): one box per window position; focusing a box opens
// the one editing frame on the page (value, % / time-pace switch, trash,
// drag bar, effect line). Edits apply on Enter, on leaving the frame and on
// the bar's release, as one choice.set field=cap; a refusal reverts.
import { h, clear, svg, trashIcon } from "./dom.js";
import { store, sendOp } from "./api.js";

const BAR = 240; // drawn bar width, px
const ELAPSED = 25; // the bar's fixed fill, standing for time elapsed (%)
const PACE_MAX = 100 - ELAPSED; // the largest N the bar covers
const THUMB = 14; // width of the overlaid range's thumb (.cap-range)
const KNOB = 14; // the drawn ring's outer width

// ---------- values (pure) ----------

function clamp(n, lo, hi) {
  return Math.min(hi, Math.max(lo, n));
}

// parseCap reads what was typed in a box: {kind: "fixed"|"pace", n},
// {kind: "empty"} or {kind: "bad", raw}. Accepts 90, 90%, pace+10 and
// pace+10% (any case, spaces around). Out-of-range numbers are kept: the
// server's refusal names them.
export function parseCap(text) {
  const raw = String(text).trim();
  if (raw === "") return { kind: "empty" };
  let t = raw.toLowerCase();
  if (t.endsWith("%")) t = t.slice(0, -1).trim();
  if (t.startsWith("pace+")) {
    const d = t.slice(5);
    return /^\d+$/.test(d) ? { kind: "pace", n: Number(d) } : { kind: "bad", raw };
  }
  return /^-?\d+(\.\d+)?$/.test(t) ? { kind: "fixed", n: Number(t) } : { kind: "bad", raw };
}

// capText shows one config cap element: 90 → "90%", "pace+10" → "pace+10%".
export function capText(v) {
  return typeof v === "number" || String(v).startsWith("pace+") ? `${v}%` : String(v);
}

// elementOf reads one config cap element; null when it is neither form.
export function elementOf(v) {
  if (typeof v === "number") return { kind: "fixed", n: v };
  const p = parseCap(String(v));
  return p.kind === "pace" ? p : null;
}

// capArg is the wire value for a parsed box: a number, "pace+N", or the
// text as typed (the server refuses it with its reason).
export function capArg(p) {
  if (p.kind === "fixed") return p.n;
  if (p.kind === "pace") return `pace+${p.n}`;
  return p.raw;
}

// The cap array runs from the shortest window up; positions past its end
// take the default. A default position before an explicit one never
// happens, so the page never writes the default cap as a number.

// writeCap is cap with position pos set to value: an explicit position is
// replaced, the first default one appended; null removes the last explicit
// one. Anything else is null (nothing to send).
export function writeCap(cap, pos, value) {
  if (!Number.isInteger(pos) || pos < 0) return null;
  if (value === null) return cap.length > 0 && pos === cap.length - 1 ? cap.slice(0, -1) : null;
  if (pos > cap.length) return null;
  const out = cap.slice();
  out[pos] = value;
  return out;
}

// trashAction is what trash does on position pos: "default" (the last
// explicit one), "uncap" (the first box when none is explicit) or null
// (disabled: anything else would leave a default before an explicit one).
export function trashAction(cap, pos) {
  if (cap.length === 0) return pos === 0 ? "uncap" : null;
  return pos === cap.length - 1 ? "default" : null;
}

// switchKind keeps the number: fixed v → pace+v (1–75), pace N → fixed N.
export function switchKind(shown, kind) {
  if (shown.kind === kind) return shown;
  if (kind === "pace") return { kind, n: clamp(Math.round(shown.n), 1, PACE_MAX) };
  return { kind, n: shown.n };
}

// barGeometry places the handle: x is its centre on the bar (px); left and
// width place the overlaid range so its thumb centre meets x at every value.
export function barGeometry(shown) {
  if (shown.kind === "pace") {
    const v = clamp(shown.n, 1, PACE_MAX);
    const start = (BAR * (ELAPSED + 1)) / 100;
    return { x: (BAR * (ELAPSED + v)) / 100, left: start - THUMB / 2, width: BAR - start + THUMB,
      range: { min: 1, max: PACE_MAX, value: Math.round(v) } };
  }
  const v = clamp(shown.n, 0, 100);
  return { x: (BAR * v) / 100, left: -THUMB / 2, width: BAR + THUMB,
    range: { min: 0, max: 100, value: Math.round(v) } };
}

export function effect(shown) {
  return shown.kind === "pace" ? `Cap at time elapsed + ${shown.n}%` : `Cap at ${shown.n}%`;
}

// positions lists the boxes: one per window label when the account has a
// stored reading, else numbered (at least one; + adds the one default box
// after the explicit ones). A hand-written cap longer than that keeps a
// numbered box per extra value. Only the explicit boxes and the first
// default one are editable.
export function positions(cap, labels, extra) {
  const named = Array.isArray(labels) && labels.length > 0;
  const n = named ? Math.max(labels.length, cap.length)
    : Math.max(cap.length, Math.min(Math.max(1, extra), cap.length + 1));
  return Array.from({ length: n }, (_, i) => ({
    label: named && i < labels.length ? labels[i] : String(i + 1), explicit: i < cap.length,
    editable: i <= cap.length }));
}

// ---------- state that survives repaints ----------

// edit is the open frame: {key, pos, draft}; draft is the typed text, null
// while it shows the stored (or pending) value. One per page.
let edit = null;
// pending: pendKey(card key, position) → text shown while its op is in
// flight.
const pending = new Map();
// extra: card key → numbered positions shown (the + button adds one).
const extra = new Map();
// mounts: card key → {container, ctx} as last rendered.
const mounts = new Map();
// Cap ops run one after another, each built from the card as the previous
// one left it, so a quick second edit never carries a stale expect.
let chain = Promise.resolve();
// busy: card key → ops queued or in flight for it (kept through pruning).
const busy = new Map();
let pruning = false;

function cardKey(ctx) {
  return `${ctx.profile}\u0000${ctx.index}`;
}

function boxKey(ctx, p) {
  return `c${ctx.index}-cap${p}`;
}

function pendKey(key, p) {
  return `${key}\u0001${p}`;
}

function isOpen(key, p) {
  return edit !== null && edit.key === key && edit.pos === p;
}

function defaultCap() {
  return store.state ? store.state.default_cap : 95;
}

function storedText(ctx, p) {
  const cap = ctx.choice.cap;
  return p < cap.length ? capText(cap[p]) : `${defaultCap()}%`;
}

function boxText(ctx, p) {
  const key = cardKey(ctx);
  if (isOpen(key, p) && edit.draft !== null) return edit.draft;
  const t = pending.get(pendKey(key, p));
  return t !== undefined ? t : storedText(ctx, p);
}

// shownOf is what the frame's switch, bar and effect line show: the typed
// value when it parses, else the stored one.
function shownOf(ctx, p, text) {
  const t = parseCap(text);
  if (t.kind === "fixed" || t.kind === "pace") return t;
  const cap = ctx.choice.cap;
  return (p < cap.length && elementOf(cap[p])) || { kind: "fixed", n: defaultCap() };
}

// ---------- applying ----------

function enqueue(key, fn) {
  busy.set(key, (busy.get(key) || 0) + 1);
  chain = chain.then(fn).catch(() => {}).finally(() => {
    const n = busy.get(key) - 1;
    if (n > 0) busy.set(key, n);
    else busy.delete(key);
  });
  return chain;
}

function liveCtx(key) {
  const m = mounts.get(key);
  return m ? m.ctx : null;
}

// unpend drops the pending text of a box unless a later edit replaced it.
function unpend(key, p, text) {
  const k = pendKey(key, p);
  if (pending.get(k) === text) pending.delete(k);
}

async function send(key, p, value, text) {
  const ctx = liveCtx(key);
  const r = await sendOp("choice.set",
    { profile: ctx.profile, index: ctx.index, expect: ctx.choice, field: "cap", value });
  unpend(key, p, text);
  rerender(key);
  return r;
}

// commit applies text typed (or dragged) into position p.
function commit(key, p, text) {
  const ctx = liveCtx(key);
  if (!ctx) return;
  pending.set(pendKey(key, p), text);
  if (isOpen(key, p)) edit.draft = null;
  enqueue(key, () => {
    const now = liveCtx(key);
    const parsed = parseCap(text);
    if (!now) return unpend(key, p, text); // the card is gone
    if (parsed.kind === "empty") return trashNow(key, p, text);
    const cap = now.choice.cap || [];
    const next = writeCap(cap, p, capArg(parsed));
    if (next !== null && JSON.stringify(next) !== JSON.stringify(cap)) return send(key, p, next, text);
    unpend(key, p, text);
    rerender(key); // "90" shows as "90%" again
    return null;
  });
}

// trashNow applies trash on position p; text is the emptied box's pending
// text, if any.
function trashNow(key, p, text) {
  const ctx = liveCtx(key);
  const cap = ctx ? ctx.choice.cap : null;
  const act = cap === null ? null : trashAction(cap, p);
  if (act === null) {
    unpend(key, p, text);
    rerender(key);
    return null;
  }
  return send(key, p, act === "uncap" ? null : writeCap(cap, p, null), text);
}

// focusCard puts focus on the card when the control that had it is gone.
function focusCard(container) {
  const a = document.activeElement;
  if (a && a !== document.body && a.isConnected) return;
  const card = container.isConnected ? container.closest(".p-card") : null;
  if (card) card.focus();
}

// trash closes the frame (focus goes to the card) and applies.
function trash(key, p) {
  const m = mounts.get(key);
  edit = null;
  const card = m.container.closest(".p-card");
  if (card) card.focus();
  rerender(key);
  enqueue(key, () => trashNow(key, p));
}

// leave closes the open frame and applies its draft.
function leave() {
  const e = edit;
  edit = null;
  if (e.draft !== null) commit(e.key, e.pos, e.draft);
  rerender(e.key);
}

function openBox(ctx, p) {
  const key = cardKey(ctx);
  if (isOpen(key, p)) return;
  if (edit) leave();
  edit = { key, pos: p, draft: null };
  rerender(key, boxKey(ctx, p));
}

// afterLeave runs once focus has settled outside a frame: it closes the
// frame unless focus is back inside it (a repaint rebuilds the frame and
// profiles.js refocuses it) or another box took over.
function afterLeave(key, p) {
  setTimeout(() => {
    if (!isOpen(key, p)) return;
    const m = mounts.get(key);
    const frame = m && m.container.isConnected ? m.container.querySelector(".cap-edit") : null;
    if (frame && frame.contains(document.activeElement)) return;
    leave();
  }, 0);
}

// ---------- drawing ----------

function clock(w) {
  return svg(w, "0 0 12 12", { "stroke-width": "1.4" },
    ["circle", { cx: 6, cy: 6.5, r: 4.3 }], ["path", { d: "M6 4.3v2.3l1.5 1" }],
    ["path", { d: "M2.2 2.2l1.3-1M9.8 2.2l-1.3-1" }]);
}

// place draws shown on the frame: switch state, handle, tint, alarm clock,
// the range's span and value, and the effect line.
function place(els, shown) {
  const g = barGeometry(shown);
  const pace = shown.kind === "pace";
  els.bar.classList.toggle("is-pace", pace);
  els.pct.setAttribute("aria-pressed", String(!pace));
  els.pace.setAttribute("aria-pressed", String(pace));
  els.line.style.left = `${g.x - 1}px`;
  els.knob.style.left = `${g.x - KNOB / 2}px`;
  els.clock.style.left = `${g.x - 8}px`;
  els.tint.style.width = `${Math.max(0, g.x - (BAR * ELAPSED) / 100)}px`;
  els.tint.hidden = !pace;
  els.clock.hidden = !pace;
  const r = els.range;
  r.style.left = `${g.left}px`;
  r.style.width = `${g.width}px`;
  r.min = String(g.range.min);
  r.max = String(g.range.max);
  r.value = String(g.range.value);
  r.setAttribute("aria-label", pace ? `${els.label} cap above time elapsed` : `${els.label} cap`);
  r.setAttribute("aria-valuetext", effect(shown));
  els.effect.textContent = effect(shown);
}

// redraft sets the frame's text to shown (after a switch or a drag).
function redraft(els, shown) {
  edit.draft = capText(capArg(shown));
  els.input.value = edit.draft;
  place(els, shown);
}

// keep stops a press on a frame button from taking focus from the value.
function keep(e) {
  e.preventDefault();
}

function switchRow(ctx, p, els) {
  const key = cardKey(ctx);
  const to = (kind) => redraft(els, switchKind(shownOf(ctx, p, els.input.value), kind));
  els.pct = h("button", { type: "button", class: "cap-sw", "data-key": `${boxKey(ctx, p)}-pct`,
    onMouseDown: keep, onClick: () => to("fixed") }, "%");
  els.pace = h("button", { type: "button", class: "cap-sw cap-sw-pace", "aria-label": "Time pace",
    "data-key": `${boxKey(ctx, p)}-pace`, onMouseDown: keep, onClick: () => to("pace") },
  clock(12), h("span", { class: "cap-raquo" }, "»"));
  const cap = ctx.choice.cap;
  return h("div", { class: "cap-row" },
    h("div", { class: "cap-switch", role: "group", "aria-label": "Cap type" }, els.pct, els.pace),
    h("button", { type: "button", class: "btn btn-trash cap-trash", "aria-label": "Delete this cap",
      "data-key": `${boxKey(ctx, p)}-trash`, disabled: trashAction(cap, p) === null,
      onMouseDown: keep, onClick: () => trash(key, p) }, trashIcon()));
}

function bar(ctx, p, els) {
  const key = cardKey(ctx);
  els.tint = h("div", { class: "cap-tint" });
  els.line = h("div", { class: "cap-line" });
  els.knob = h("div", { class: "cap-knob" });
  els.clock = h("span", { class: "cap-clock", "aria-hidden": "true" }, clock(11), h("span", {}, "»"));
  els.range = h("input", { type: "range", class: "cap-range", step: "1", "data-key": `${boxKey(ctx, p)}-range`,
    onInput: () => redraft(els, { kind: els.bar.classList.contains("is-pace") ? "pace" : "fixed",
      n: Number(els.range.value) }),
    onChange: () => {
      if (edit && edit.draft !== null) commit(key, p, edit.draft);
    } });
  els.bar = h("div", { class: "cap-bar" }, h("div", { class: "cap-fill" }), els.tint, els.line, els.knob,
    els.clock, els.range);
  return els.bar;
}

function frame(ctx, p, pos) {
  const key = cardKey(ctx);
  const els = { label: pos.label };
  els.input = h("input", { type: "text", class: "cap-edit-val", "aria-label": `${pos.label} cap`,
    "data-key": boxKey(ctx, p), value: boxText(ctx, p),
    onInput: (e) => {
      edit.draft = e.target.value;
      place(els, shownOf(ctx, p, e.target.value));
    },
    onKeyDown: (e) => {
      if (e.key === "Enter" && edit.draft !== null) commit(key, p, edit.draft);
      if (e.key === "Escape") {
        edit.draft = null;
        rerender(key);
      }
    } });
  els.effect = h("span", { class: "cap-effect" });
  // tabindex -1: a click on the frame's padding, bar or text keeps focus
  // inside it instead of moving it to body (which would close the frame).
  const el = h("div", { class: "cap-edit", tabindex: "-1", "data-key": `${boxKey(ctx, p)}-frame`,
    onFocusOut: () => afterLeave(key, p) },
    h("span", { class: "mono cap-edit-win" }, pos.label), els.input, switchRow(ctx, p, els), bar(ctx, p, els),
    els.effect);
  place(els, shownOf(ctx, p, els.input.value));
  return el;
}

// box draws a closed position; a default one after the first default is
// shown but disabled (it cannot be written before the ones ahead of it).
function box(ctx, p, pos) {
  const v = pos.explicit ? elementOf(ctx.choice.cap[p]) : null;
  return h("label", { class: ["cap-box", !pos.explicit && "is-default", v && v.kind === "pace" && "is-pace",
    !pos.editable && "is-locked"] },
    h("span", { class: "mono cap-win" }, pos.label),
    h("input", { type: "text", class: "cap-val", "aria-label": `${pos.label} cap`, "data-key": boxKey(ctx, p),
      value: boxText(ctx, p), disabled: !pos.editable, onFocus: () => openBox(ctx, p) }));
}

function addCaps(ctx) {
  const key = cardKey(ctx);
  return h("button", { type: "button", class: "btn", "data-key": `c${ctx.index}-addcaps`,
    onClick: () => enqueue(key, async () => {
      const now = liveCtx(key);
      if (!now) return;
      await sendOp("choice.set", { profile: now.profile, index: now.index, expect: now.choice,
        field: "cap", value: [] });
      focusCard(mounts.get(key).container); // the button is gone after a repaint
    }) }, "Add caps");
}

function plus(ctx, n) {
  const key = cardKey(ctx);
  return h("button", { type: "button", class: "cap-add", "aria-label": "Add a position",
    "data-key": `c${ctx.index}-cap-add`,
    onClick: () => {
      extra.set(key, n + 1);
      rerender(key, boxKey(ctx, n));
    } }, "+");
}

function fill(container, ctx) {
  clear(container);
  const c = ctx.choice;
  container.classList.toggle("is-uncapped", c.cap === null);
  if (c.cap === null) {
    container.append(h("span", { class: "cap-none" }, "No cap: pick as the fallback model"), addCaps(ctx));
    return;
  }
  const key = cardKey(ctx);
  const byModel = store.state && store.state.windows ? store.state.windows[c.account] : null;
  const labels = byModel ? byModel[c.model] : null;
  const list = positions(c.cap, labels, extra.get(key) || 0);
  list.forEach((pos, p) => container.append(isOpen(key, p) ? frame(ctx, p, pos) : box(ctx, p, pos)));
  const named = Array.isArray(labels) && labels.length > 0;
  if (!named && list.every((x) => x.explicit)) container.append(plus(ctx, list.length));
}

// rerender redraws one card's caps in place. Focus goes to the element
// with data-key want, else back to the element inside that had it.
function rerender(key, want) {
  const m = mounts.get(key);
  if (!m || !m.container.isConnected) return;
  const a = document.activeElement;
  const k = want || (a && m.container.contains(a) && a.dataset ? a.dataset.key : null);
  fill(m.container, m.ctx);
  const el = k ? [...m.container.querySelectorAll("[data-key]")].find((x) => x.dataset.key === k) : null;
  if (!el) return;
  el.focus();
  if (el.type === "text") el.setSelectionRange(el.value.length, el.value.length);
}

// renderCaps fills container with the card's cap boxes. ctx is
// {profile, index, choice}: profile is the profile's name, index the
// choice's position (0-based), choice the choice as rendered (sent as
// `expect` with every choice.* op).
export function renderCaps(container, ctx) {
  mounts.set(cardKey(ctx), { container, ctx });
  fill(container, ctx);
  if (!pruning) {
    pruning = true;
    queueMicrotask(prune);
  }
}

// prune runs once a repaint has rendered every card: it forgets the cards
// that were not rendered again (another profile selected, a choice gone),
// with their pending texts and added positions. A card with ops queued or
// the open frame is kept until those finish.
function prune() {
  pruning = false;
  for (const [key, m] of mounts) {
    if (m.container.isConnected || busy.has(key) || (edit && edit.key === key)) continue;
    mounts.delete(key);
    extra.delete(key);
    for (const k of pending.keys()) if (k.startsWith(`${key}\u0001`)) pending.delete(k);
  }
}
