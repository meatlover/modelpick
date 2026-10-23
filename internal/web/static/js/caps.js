// Caps on a choice card (spec 8.3 "Caps on a card", mockup
// CapsEditor.dc.html): one box per window position; focusing a box opens
// the one editing frame on the page (value, % / time-pace switch, trash,
// drag bar, effect line). Edits apply on Enter, on leaving the frame and on
// the bar's release, as one choice.set field=cap; a refusal reverts.
import { h, clear } from "./dom.js";
import { store, sendOp } from "./api.js";

const SVG = "http://www.w3.org/2000/svg";
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

// writeCap is cap with position pos set to value (null: back to the
// default). Trailing default positions are dropped; a default position
// before one with its own value is written as def, the current default cap
// (the config has no placeholder for "default").
export function writeCap(cap, pos, value, def) {
  const DEF = {};
  const out = cap.slice();
  while (out.length <= pos) out.push(DEF);
  out[pos] = value === null ? DEF : value;
  while (out.length && out[out.length - 1] === DEF) out.pop();
  return out.map((v) => (v === DEF ? def : v));
}

// trashAction is what trash does on position pos: "default" (it has its
// own value), "uncap" (no position has one) or null (disabled: removing a
// default position would shift the ones after it).
export function trashAction(cap, pos) {
  if (pos < cap.length) return "default";
  return cap.length === 0 ? "uncap" : null;
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
// stored reading, else numbered (at least one, plus those added with +).
// A hand-written cap longer than that keeps a numbered box per extra value.
export function positions(cap, labels, extra) {
  const named = Array.isArray(labels) && labels.length > 0;
  const n = named ? Math.max(labels.length, cap.length) : Math.max(cap.length, 1, extra);
  return Array.from({ length: n }, (_, i) => ({
    label: named && i < labels.length ? labels[i] : String(i + 1), explicit: i < cap.length }));
}

// ---------- state that survives repaints ----------

// edit is the open frame: {key, pos, draft}; draft is the typed text, null
// while it shows the stored (or pending) value. One per page.
let edit = null;
// pending: box data-key → text shown while its op is in flight.
const pending = new Map();
// extra: card key → numbered positions shown (the + button adds one).
const extra = new Map();
// mounts: card key → {container, ctx} as last rendered.
const mounts = new Map();
// Cap ops run one after another, each built from the card as the previous
// one left it, so a quick second edit never carries a stale expect.
let chain = Promise.resolve();

function cardKey(ctx) {
  return `${ctx.profile}\u0000${ctx.index}`;
}

function boxKey(ctx, p) {
  return `c${ctx.index}-cap${p}`;
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
  const t = pending.get(boxKey(ctx, p));
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

function enqueue(fn) {
  chain = chain.then(fn).catch(() => {});
  return chain;
}

function liveCtx(key) {
  const m = mounts.get(key);
  return m ? m.ctx : null;
}

// unpend drops the pending text of a box unless a later edit replaced it.
function unpend(ctx, p, text) {
  const bk = boxKey(ctx, p);
  if (pending.get(bk) === text) pending.delete(bk);
}

async function send(key, p, value, text) {
  const ctx = liveCtx(key);
  const r = await sendOp("choice.set",
    { profile: ctx.profile, index: ctx.index, expect: ctx.choice, field: "cap", value });
  unpend(ctx, p, text);
  rerender(key);
  return r;
}

// commit applies text typed (or dragged) into position p.
function commit(key, p, text) {
  const ctx = liveCtx(key);
  if (!ctx) return;
  pending.set(boxKey(ctx, p), text);
  if (isOpen(key, p)) edit.draft = null;
  enqueue(() => {
    const now = liveCtx(key);
    const parsed = parseCap(text);
    if (parsed.kind === "empty") return trashNow(key, p, text);
    const cap = now.choice.cap || [];
    const next = writeCap(cap, p, capArg(parsed), defaultCap());
    if (JSON.stringify(next) !== JSON.stringify(cap)) return send(key, p, next, text);
    unpend(now, p, text);
    rerender(key); // "90" shows as "90%" again
    return null;
  });
}

// trashNow applies trash on position p; text is the emptied box's pending
// text, if any.
function trashNow(key, p, text) {
  const ctx = liveCtx(key);
  const cap = ctx.choice.cap;
  const act = cap === null ? null : trashAction(cap, p);
  if (act === null) {
    unpend(ctx, p, text);
    rerender(key);
    return null;
  }
  return send(key, p, act === "uncap" ? null : writeCap(cap, p, null, defaultCap()), text);
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
  enqueue(() => trashNow(key, p));
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

function svg(w, view, attrs, ...parts) {
  const el = document.createElementNS(SVG, "svg");
  const all = { width: w, height: w, viewBox: view, fill: "none", stroke: "currentColor",
    "aria-hidden": "true", ...attrs };
  for (const [k, v] of Object.entries(all)) el.setAttribute(k, String(v));
  for (const [tag, a] of parts) {
    const c = document.createElementNS(SVG, tag);
    for (const [k, v] of Object.entries(a)) c.setAttribute(k, String(v));
    el.append(c);
  }
  return el;
}

function clock(w) {
  return svg(w, "0 0 12 12", { "stroke-width": "1.4" },
    ["circle", { cx: 6, cy: 6.5, r: 4.3 }], ["path", { d: "M6 4.3v2.3l1.5 1" }],
    ["path", { d: "M2.2 2.2l1.3-1M9.8 2.2l-1.3-1" }]);
}

function trashIcon() {
  return svg(16, "0 0 16 16", { "stroke-width": "1.5", "stroke-linecap": "round" },
    ["path", { d: "M2.5 4h11M6.5 4V2.5h3V4M4 4l.7 9.5h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5" }]);
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
  const el = h("div", { class: "cap-edit", onFocusOut: () => afterLeave(key, p) },
    h("span", { class: "mono cap-edit-win" }, pos.label), els.input, switchRow(ctx, p, els), bar(ctx, p, els),
    els.effect);
  place(els, shownOf(ctx, p, els.input.value));
  return el;
}

function box(ctx, p, pos) {
  const v = pos.explicit ? elementOf(ctx.choice.cap[p]) : null;
  return h("label", { class: ["cap-box", !pos.explicit && "is-default", v && v.kind === "pace" && "is-pace"] },
    h("span", { class: "mono cap-win" }, pos.label),
    h("input", { type: "text", class: "cap-val", "aria-label": `${pos.label} cap`, "data-key": boxKey(ctx, p),
      value: boxText(ctx, p), onFocus: () => openBox(ctx, p) }));
}

function addCaps(ctx) {
  const key = cardKey(ctx);
  return h("button", { type: "button", class: "btn", "data-key": `c${ctx.index}-addcaps`,
    onClick: () => enqueue(async () => {
      const now = liveCtx(key);
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
  if (!(Array.isArray(labels) && labels.length)) container.append(plus(ctx, list.length));
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
}
