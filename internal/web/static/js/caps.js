// Caps on a choice card (spec 8.3 "Caps on a card", mockup
// CapsEditor.dc.html): one box per window position; focusing a box opens
// the one editing frame on the page (window label, the sentence "Cap at
// 90%" with only the number editable, % / time-pace switch, trash, drag
// bar and its caption). Edits apply on Enter, on leaving the frame and on
// the bar's release, as one choice.set field=cap; a refusal reverts.
import { h, clear, svg, trashIcon } from "./dom.js";
import { store, sendOp } from "./api.js";

const BAR = 240; // drawn bar width, px
// The bar's fill is an illustration with fixed values, not the account's
// readings (owner, 2026-10-23): token usage in fixed mode, time elapsed in
// pace mode. ELAPSED also sets the pace geometry.
const USED = 33; // fixed mode's fill (%)
const ELAPSED = 50; // pace mode's fill (%)
const PACE_MAX = 100 - ELAPSED; // the largest N the bar covers
const THUMB = 14; // width of the overlaid range's thumb (.cap-range)
const KNOB = 14; // the drawn ring's outer width
// Owner ruling 2026-10-23 (round 3): a typed or dragged cap is bounded to
// fixed 0–100 and pace+0 to pace+50 (the demo server refuses the rest).
const RANGE = { fixed: [0, 100], pace: [0, PACE_MAX] };
// Owner ruling 2026-10-23 (round 3): what a switch to the kind that is not
// stored shows.
const SWITCHED = { fixed: 90, pace: 5 };
const NUMBER = /^-?\d+(\.\d+)?$/;

// ---------- values (pure) ----------

function clamp(n, lo, hi) {
  return Math.min(hi, Math.max(lo, n));
}

// bound puts n in kind's range; pace N is a whole number.
function bound(kind, n) {
  const [lo, hi] = RANGE[kind];
  return clamp(kind === "pace" ? Math.round(n) : n, lo, hi);
}

// readCap reads 90, 90%, pace+10 or pace+10% (any case, spaces around) as
// written: {kind: "fixed"|"pace", n}, {kind: "empty"} or {kind: "bad", raw}.
function readCap(text) {
  const raw = String(text).trim();
  if (raw === "") return { kind: "empty" };
  let t = raw.toLowerCase();
  if (t.endsWith("%")) t = t.slice(0, -1).trim();
  if (t.startsWith("pace+")) {
    const d = t.slice(5);
    return /^\d+$/.test(d) ? { kind: "pace", n: Number(d) } : { kind: "bad", raw };
  }
  return NUMBER.test(t) ? { kind: "fixed", n: Number(t) } : { kind: "bad", raw };
}

// parseCap reads a cap text like readCap, with the number bounded to its
// kind's range (120 → 100, pace+70 → pace+50).
export function parseCap(text) {
  const p = readCap(text);
  return p.kind === "fixed" || p.kind === "pace" ? { kind: p.kind, n: bound(p.kind, p.n) } : p;
}

// boundNumber reads the frame's number box for kind: the number (an
// optional % after it, an optional + before a pace one) bounded to the
// kind's range, or null when it is not a number (the box then reverts).
export function boundNumber(text, kind) {
  let t = String(text).trim();
  if (t.endsWith("%")) t = t.slice(0, -1).trim();
  if (kind === "pace" && t.startsWith("+")) t = t.slice(1);
  return NUMBER.test(t) ? bound(kind, Number(t)) : null;
}

// capText shows one config cap element: 90 → "90%", "pace+10" → "pace+10%".
export function capText(v) {
  return typeof v === "number" || String(v).startsWith("pace+") ? `${v}%` : String(v);
}

// elementOf reads one config cap element as stored (a hand-written pace+60
// stays 60: the page shows what is in effect); null when it is neither form.
export function elementOf(v) {
  if (typeof v === "number") return { kind: "fixed", n: v };
  const p = readCap(String(v));
  return p.kind === "pace" ? p : null;
}

// capArg is the wire value for a parsed box: a number, "pace+N", or the
// text as typed (the server refuses it with its reason).
export function capArg(p) {
  if (p.kind === "fixed") return p.n;
  if (p.kind === "pace") return `pace+${p.n}`;
  return p.raw;
}

// The cap array runs from the shortest window up. Owner ruling 2026-10-23
// (round 5): an element is a number, "pace+N" or "default" (the position
// uses the Global default cap); positions past its end use it too, and
// trailing "default" elements are dropped when writing.
const DEFAULT = "default";

// isSet says a cap element is set (a number or pace+N), not the default
// (a "default" element, or a position past the array's end).
export function isSet(v) {
  return v !== undefined && v !== DEFAULT;
}

// trimmed drops trailing "default" elements.
function trimmed(cap) {
  let n = cap.length;
  while (n > 0 && !isSet(cap[n - 1])) n--;
  return cap.slice(0, n);
}

// writeCap is cap with position pos set to value, any gap before it filled
// with "default"; null makes a set position the default. Trailing defaults
// are dropped. null when nothing changes (nothing to send).
export function writeCap(cap, pos, value) {
  if (!Number.isInteger(pos) || pos < 0) return null;
  if (value === null && !isSet(cap[pos])) return null;
  const out = cap.slice();
  while (out.length < pos) out.push(DEFAULT);
  out[pos] = value === null ? DEFAULT : value;
  return trimmed(out);
}

// trashAction is what trash does on position pos: "default" (a set one
// becomes the default), "uncap" (the first box when none is set),
// "discard" (a default box holding a value typed but not yet saved: trash
// puts the default back) or null (disabled). drafted says the box holds
// such a value (typed, or sent and not yet answered).
export function trashAction(cap, pos, drafted = false) {
  if (isSet(cap[pos])) return "default";
  if (pos === 0 && !cap.some(isSet)) return "uncap";
  return drafted ? "discard" : null;
}

// switchKind is what the frame shows after a switch to kind: the stored
// value when stored is of that kind, else 90 (fixed) or pace+5.
export function switchKind(kind, stored) {
  return stored.kind === kind ? stored : { kind, n: SWITCHED[kind] };
}

// previewOf is what the bar shows while the number box holds text in mode
// kind: the typed number bounded, else what a switch to kind shows.
export function previewOf(kind, text, stored) {
  const n = boundNumber(text, kind);
  return { kind, n: n !== null ? n : bound(kind, switchKind(kind, stored).n) };
}

// lineParts is the frame's sentence around the number.
export function lineParts(kind) {
  return kind === "pace" ? ["Cap at time elapsed + ", "%"] : ["Cap at ", "%"];
}

export function capLine(shown) {
  const [pre, post] = lineParts(shown.kind);
  return `${pre}${shown.n}${post}`;
}

// barGeometry places the handle: x is its centre on the bar (px); left and
// width place the overlaid range so its thumb centre meets x at every value.
// Pace N runs from the elapsed mark (pace+0) to the bar's end (pace+50).
export function barGeometry(shown) {
  if (shown.kind === "pace") {
    const v = clamp(shown.n, 0, PACE_MAX);
    const start = (BAR * ELAPSED) / 100;
    return { x: (BAR * (ELAPSED + v)) / 100, left: start - THUMB / 2, width: BAR - start + THUMB,
      range: { min: 0, max: PACE_MAX, value: Math.round(v) } };
  }
  const v = clamp(shown.n, 0, 100);
  return { x: (BAR * v) / 100, left: -THUMB / 2, width: BAR + THUMB,
    range: { min: 0, max: 100, value: Math.round(v) } };
}

// scaleText names what the bar's fill stands for in mode kind.
export function scaleText(kind) {
  return kind === "pace" ? `Time elapsed: ${ELAPSED}%` : `Token usage: ${USED}%`;
}

// positions lists the boxes: one per window label when the account has a
// stored reading, else numbered (at least one; + adds one default box after
// the array). A hand-written cap longer than that keeps a numbered box per
// extra value. explicit: the position is set. Every box is editable.
export function positions(cap, labels, extra) {
  const named = Array.isArray(labels) && labels.length > 0;
  const n = named ? Math.max(labels.length, cap.length)
    : Math.max(cap.length, Math.min(Math.max(1, extra), cap.length + 1));
  return Array.from({ length: n }, (_, i) => ({
    label: named && i < labels.length ? labels[i] : String(i + 1), explicit: isSet(cap[i]) }));
}

// plusShown says the numbered boxes end with + : the last box is set.
export function plusShown(list, named) {
  return !named && list.length > 0 && list[list.length - 1].explicit;
}

// ---------- state that survives repaints ----------

// edit is the open frame: {key, pos, kind, text}; kind and text are the
// mode and the number box's text, both null while it shows the stored (or
// pending) value. One per page.
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
  const v = ctx.choice.cap[p];
  return isSet(v) ? capText(v) : `${defaultCap()}%`;
}

function boxText(ctx, p) {
  const t = pending.get(pendKey(cardKey(ctx), p));
  return t !== undefined ? t : storedText(ctx, p);
}

// baseOf is the value a box stands for: its pending text, else the stored
// element as it is, else the default cap.
function baseOf(ctx, p) {
  const t = pending.get(pendKey(cardKey(ctx), p));
  const v = t !== undefined ? parseCap(t) : null;
  if (v && (v.kind === "fixed" || v.kind === "pace")) return v;
  const cap = ctx.choice.cap;
  return (isSet(cap[p]) && elementOf(cap[p])) || { kind: "fixed", n: defaultCap() };
}

// frameKind and frameText are what the open frame shows.
function frameKind(ctx, p) {
  return edit.kind !== null ? edit.kind : baseOf(ctx, p).kind;
}

function frameText(ctx, p) {
  return edit.text !== null ? edit.text : String(baseOf(ctx, p).n);
}

// setDraft records the frame's mode and text; the stored value itself
// leaves nothing to apply.
function setDraft(ctx, p, kind, text) {
  const b = baseOf(ctx, p);
  const same = kind === b.kind && text === String(b.n);
  edit.kind = same ? null : kind;
  edit.text = same ? null : text;
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

// apply saves the frame's draft (kind, text) into position p: a number
// out of range is saved bounded; text that is not a number, or the value
// the box already stands for, sends nothing (the box shows that value).
function apply(key, p, kind, text) {
  const ctx = liveCtx(key);
  const n = ctx ? boundNumber(text, kind) : null;
  if (n === null) return;
  const b = baseOf(ctx, p);
  if (kind !== b.kind || n !== b.n) commit(key, p, { kind, n });
}

// commit sends value ({kind, n}) for position p; the box shows it until
// the op is answered.
function commit(key, p, value) {
  const text = capText(capArg(value));
  pending.set(pendKey(key, p), text);
  enqueue(key, () => {
    const now = liveCtx(key);
    if (!now) return unpend(key, p, text); // the card is gone
    const cap = now.choice.cap || [];
    const next = writeCap(cap, p, capArg(value));
    if (next !== null && JSON.stringify(next) !== JSON.stringify(cap)) return send(key, p, next, text);
    unpend(key, p, text);
    rerender(key);
    return null;
  });
}

// drafted says box p holds a value not yet in the stored cap: typed in
// the open frame, or sent and not yet answered.
function drafted(key, p) {
  return (isOpen(key, p) && edit.text !== null) || pending.has(pendKey(key, p));
}

// trashNow applies trash on position p to the card as it stands when its
// turn comes: a value sent before it has been saved by then, and a typed
// one that was never sent leaves nothing to remove.
function trashNow(key, p) {
  const ctx = liveCtx(key);
  const cap = ctx ? ctx.choice.cap : null;
  const act = cap === null ? null : trashAction(cap, p);
  if (act === null) {
    rerender(key);
    return null;
  }
  return send(key, p, act === "uncap" ? null : writeCap(cap, p, null), undefined);
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

// settle applies the open frame's draft and shows what was saved (or, for
// text that is not a number, the stored value again).
function settle() {
  const e = edit;
  if (!e || e.text === null) return;
  const { kind, text } = e;
  e.kind = null;
  e.text = null;
  apply(e.key, e.pos, kind, text);
}

// leave closes the open frame and applies its draft.
function leave() {
  const key = edit.key;
  settle();
  edit = null;
  rerender(key);
}

function openBox(ctx, p) {
  const key = cardKey(ctx);
  if (isOpen(key, p)) return;
  if (edit) leave();
  edit = { key, pos: p, kind: null, text: null };
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

// place draws shown on the frame: switch state, fill, handle, alarm clock,
// the range's span and value and the fill's caption.
function place(els, shown) {
  const g = barGeometry(shown);
  const pace = shown.kind === "pace";
  els.bar.classList.toggle("is-pace", pace);
  els.pct.setAttribute("aria-pressed", String(!pace));
  els.pace.setAttribute("aria-pressed", String(pace));
  els.line.style.left = `${g.x - 1}px`;
  els.knob.style.left = `${g.x - KNOB / 2}px`;
  els.clock.style.left = `${g.x - 8}px`;
  els.fill.style.width = `${(BAR * (pace ? ELAPSED : USED)) / 100}px`;
  els.clock.hidden = !pace;
  const r = els.range;
  r.style.left = `${g.left}px`;
  r.style.width = `${g.width}px`;
  r.min = String(g.range.min);
  r.max = String(g.range.max);
  r.value = String(g.range.value);
  r.setAttribute("aria-label", pace ? `${els.label} cap above time elapsed` : `${els.label} cap`);
  r.setAttribute("aria-valuetext", capLine(shown));
  els.scale.textContent = scaleText(shown.kind);
}

// fit sizes the number box to its text, so the sentence reads as one line.
function fit(input) {
  input.style.width = `${Math.max(1, input.value.length)}ch`;
}

// draft records text typed (or dragged) in the frame and redraws the bar
// with it, bounded.
function draft(ctx, p, els, text) {
  const kind = frameKind(ctx, p);
  setDraft(ctx, p, kind, text);
  els.input.value = text;
  fit(els.input);
  place(els, previewOf(kind, text, baseOf(ctx, p)));
  trashState(ctx, p, els);
}

// trashState enables the frame's trash by the box as it shows now, so
// typing a value into the first default box enables it at once.
function trashState(ctx, p, els) {
  els.trash.disabled = trashAction(ctx.choice.cap, p, drafted(cardKey(ctx), p)) === null;
}

// keep stops a press on a frame button from taking focus from the value.
function keep(e) {
  e.preventDefault();
}

function switchRow(ctx, p, els) {
  const key = cardKey(ctx);
  const to = (kind) => {
    if (kind === frameKind(ctx, p)) return;
    setDraft(ctx, p, kind, String(switchKind(kind, baseOf(ctx, p)).n));
    rerender(key);
  };
  els.pct = h("button", { type: "button", class: "cap-sw", "data-key": `${boxKey(ctx, p)}-pct`,
    onMouseDown: keep, onClick: () => to("fixed") }, "%");
  els.pace = h("button", { type: "button", class: "cap-sw cap-sw-pace", "aria-label": "Time pace",
    "data-key": `${boxKey(ctx, p)}-pace`, onMouseDown: keep, onClick: () => to("pace") },
  clock(12), h("span", { class: "cap-raquo" }, "»"));
  els.trash = h("button", { type: "button", class: "btn btn-trash cap-trash", "aria-label": "Delete this cap",
    "data-key": `${boxKey(ctx, p)}-trash`, onMouseDown: keep, onClick: () => trash(key, p) }, trashIcon());
  trashState(ctx, p, els);
  return h("div", { class: "cap-row" },
    h("div", { class: "cap-switch", role: "group", "aria-label": "Cap type" }, els.pct, els.pace), els.trash);
}

function bar(ctx, p, els) {
  const key = cardKey(ctx);
  els.fill = h("div", { class: "cap-fill" });
  els.line = h("div", { class: "cap-line" });
  els.knob = h("div", { class: "cap-knob" });
  els.clock = h("span", { class: "cap-clock", "aria-hidden": "true" }, clock(11), h("span", {}, "»"));
  els.range = h("input", { type: "range", class: "cap-range", step: "1", "data-key": `${boxKey(ctx, p)}-range`,
    onInput: () => draft(ctx, p, els, els.range.value),
    onChange: () => {
      settle();
      rerender(key);
    } });
  els.bar = h("div", { class: "cap-bar" }, els.fill, els.line, els.knob,
    els.clock, els.range);
  return els.bar;
}

// valueLine is the frame's second line, "Cap at 90%" or "Cap at time
// elapsed + 10%", with only the number editable.
function valueLine(ctx, p, pos, els) {
  const key = cardKey(ctx);
  const [pre, post] = lineParts(frameKind(ctx, p));
  els.input = h("input", { type: "text", inputmode: "decimal", class: "cap-edit-val",
    "aria-label": `${pos.label} cap`, "data-key": boxKey(ctx, p), value: frameText(ctx, p),
    onInput: (e) => draft(ctx, p, els, e.target.value),
    onKeyDown: (e) => {
      if (e.key === "Enter") settle();
      if (e.key === "Escape") edit.kind = edit.text = null;
      if (e.key === "Enter" || e.key === "Escape") rerender(key);
    } });
  fit(els.input);
  return h("div", { class: "cap-edit-line" }, pre, els.input, post);
}

function frame(ctx, p, pos) {
  const key = cardKey(ctx);
  const els = { label: pos.label };
  els.scale = h("span", { class: "cap-scale" });
  // tabindex -1: a click on the frame's padding, bar or text keeps focus
  // inside it instead of moving it to body (which would close the frame).
  const el = h("div", { class: "cap-edit", tabindex: "-1", "data-key": `${boxKey(ctx, p)}-frame`,
    onFocusOut: () => afterLeave(key, p) },
    h("span", { class: "mono cap-edit-win" }, pos.label), valueLine(ctx, p, pos, els), switchRow(ctx, p, els),
    bar(ctx, p, els), els.scale);
  place(els, previewOf(frameKind(ctx, p), frameText(ctx, p), baseOf(ctx, p)));
  return el;
}

// box draws a closed position: solid when set, dashed when it uses the
// default.
function box(ctx, p, pos) {
  const v = pos.explicit ? elementOf(ctx.choice.cap[p]) : null;
  return h("label", { class: ["cap-box", !pos.explicit && "is-default", v && v.kind === "pace" && "is-wide"] },
    h("span", { class: "mono cap-win" }, pos.label),
    h("input", { type: "text", class: "cap-val", "aria-label": `${pos.label} cap`, "data-key": boxKey(ctx, p),
      value: boxText(ctx, p), onFocus: () => openBox(ctx, p) }));
}

function addCaps(ctx) {
  const key = cardKey(ctx);
  return h("button", { type: "button", class: "cap-addcaps", "aria-label": "Add caps",
    "data-key": `c${ctx.index}-addcaps`,
    onClick: () => enqueue(key, async () => {
      const now = liveCtx(key);
      if (!now) return;
      await sendOp("choice.set", { profile: now.profile, index: now.index, expect: now.choice,
        field: "cap", value: [] });
      focusCard(mounts.get(key).container); // the button is gone after a repaint
    }) }, "+ caps");
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
  if (plusShown(list, Array.isArray(labels) && labels.length > 0)) container.append(plus(ctx, list.length));
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
