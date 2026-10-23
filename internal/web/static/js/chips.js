// Schedule chips and "+ schedule" on a choice card (spec 8.3 "Schedules on
// a card", mockup Profiles.dc.html): a chip per attached schedule (slash =
// skip, check = allow) with a remove ×, and a picker that attaches one more
// (hovering or focusing an option previews its week) or, last, opens
// "+ New schedule" on the Schedules tab. Every edit is one choice.set of
// skip_during / allow_during.
import { h, clear } from "./dom.js";
import { store, sendOp } from "./api.js";
import { newSchedule } from "./schedules.js";
import { weekPeek } from "./weekpeek.js";

const SVG = "http://www.w3.org/2000/svg";
const KINDS = ["skip_during", "allow_during"];
const WORD = { skip_during: "Skip", allow_during: "Allow" };
const PATHS = {
  skip_during: ["M3.2 10.8l7.6-7.6"],
  allow_during: ["M4.4 7.2l1.8 1.8 3.4-3.6"],
};

// ---------- values (pure) ----------

// attached lists a choice's schedules as {kind, name}: skip ones first.
export function attached(choice) {
  return KINDS.flatMap((kind) => (choice[kind] || []).map((name) => ({ kind, name })));
}

// lockedKind is the kind the picker's switch is fixed to: that of the
// schedules already on the choice, or null when it has none.
export function lockedKind(choice) {
  return KINDS.find((k) => (choice[k] || []).length > 0) || null;
}

// available lists the schedule names not yet on the choice, in order.
export function available(names, choice) {
  const used = new Set(attached(choice).map((a) => a.name));
  return names.filter((n) => !used.has(n));
}

export function chipTitle(kind, name) {
  return `${kind === "skip_during" ? "Skip during" : "Allow only during"} ${name}`;
}

// ---------- state that survives repaints ----------

// open is the one open picker: {key, kind}; kind is the chosen switch side.
let open = null;
// mounts: card key → {container, ctx} as last rendered.
const mounts = new Map();
// Edits run one after another, each built from the card as the previous
// one left it, so a quick second edit never carries a stale expect.
let chain = Promise.resolve();
let listening = false;
let pruning = false;

function cardKey(ctx) {
  return `${ctx.profile}\u0000${ctx.index}`;
}

function liveCtx(key) {
  const m = mounts.get(key);
  return m && m.container.isConnected ? m.ctx : null;
}

function scheduleNames() {
  return store.state && Array.isArray(store.state.schedules) ? store.state.schedules.map((s) => s.name) : [];
}

// ---------- drawing ----------

function icon(kind, w) {
  const el = document.createElementNS(SVG, "svg");
  const attrs = { width: w, height: w, viewBox: "0 0 14 14", fill: "none", stroke: "currentColor",
    "stroke-width": "1.5", "aria-hidden": "true" };
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
  const circle = document.createElementNS(SVG, "circle");
  for (const [k, v] of Object.entries({ cx: 7, cy: 7, r: 5.5 })) circle.setAttribute(k, String(v));
  el.append(circle);
  for (const d of PATHS[kind]) {
    const p = document.createElementNS(SVG, "path");
    p.setAttribute("d", d);
    el.append(p);
  }
  return el;
}

function cross() {
  const el = document.createElementNS(SVG, "svg");
  const attrs = { width: 10, height: 10, viewBox: "0 0 10 10", stroke: "currentColor",
    "stroke-width": "1.5", "aria-hidden": "true" };
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
  const p = document.createElementNS(SVG, "path");
  p.setAttribute("d", "M2 2l6 6M8 2l-6 6");
  el.append(p);
  return el;
}

function chip(ctx, a) {
  const key = cardKey(ctx);
  return h("span", { class: ["sched-chip", a.kind === "allow_during" && "is-allow"], title: chipTitle(a.kind, a.name) },
    icon(a.kind, 13), a.name,
    h("button", { type: "button", class: "sched-x", "aria-label": `Remove ${a.name}`,
      "data-key": `c${ctx.index}-sched-x-${a.kind}-${a.name}`,
      onClick: () => edit(key, a.kind, (list) => list.filter((n) => n !== a.name), true) }, cross()));
}

// modeButton is one side of the picker's switch.
function modeButton(ctx, kind, shown, locked) {
  const pressed = kind === shown;
  const blocked = locked !== null && locked !== kind;
  return h("button", { type: "button", class: "sched-mode", "aria-pressed": String(pressed),
    "aria-disabled": blocked ? "true" : null, "data-key": `c${ctx.index}-sched-${kind}`,
    onClick: () => {
      if (blocked || pressed) return;
      open.kind = kind;
      refill(cardKey(ctx), `c${ctx.index}-sched-${kind}`);
    } }, icon(kind, 12), WORD[kind]);
}

// ---------- hover preview ----------

// unpeek removes the week preview from a picker.
function unpeek(pop) {
  for (const el of pop.querySelectorAll(".sched-peek")) el.remove();
}

// peek shows schedule name's week beside the picker, level with the option
// opt: on the right when it fits in the viewport, else on the left, else
// below, shifted left as far as it must to stay inside the viewport, so it
// never covers the options or widens the page.
function peek(opt, name) {
  const pop = opt.closest(".sched-pop");
  const sched = store.state ? store.state.schedules.find((s) => s.name === name) : null;
  if (!pop || !sched) return;
  unpeek(pop);
  const el = weekPeek(sched);
  pop.append(el);
  const r = pop.getBoundingClientRect();
  const room = document.documentElement.clientWidth;
  const gap = 6;
  let side = "below";
  if (r.right + gap + el.offsetWidth <= room - 8) side = "right";
  else if (r.left - gap - el.offsetWidth >= 8) side = "left";
  el.classList.add(`is-${side}`);
  if (side !== "below") el.style.top = `${Math.max(0, Math.min(opt.offsetTop, pop.clientHeight - el.offsetHeight))}px`;
  else el.style.left = `${Math.min(0, Math.max(8 - r.left, room - 8 - r.left - el.offsetWidth))}px`;
}

function pickOption(ctx, shown, name) {
  const key = cardKey(ctx);
  const leave = (e) => {
    const pop = e.currentTarget.closest(".sched-pop");
    if (pop) unpeek(pop);
  };
  return h("button", { type: "button", class: "sched-pick", "data-key": `c${ctx.index}-sched-pick-${name}`,
    onClick: () => choose(key, shown, name),
    onPointerEnter: (e) => peek(e.currentTarget, name), onFocus: (e) => peek(e.currentTarget, name),
    onPointerLeave: leave, onBlur: leave }, name);
}

// newOption closes the picker and opens the New schedule input on the
// Schedules tab; nothing is attached to the choice.
function newOption(ctx) {
  const key = cardKey(ctx);
  return h("button", { type: "button", class: "sched-pick sched-new", "data-key": `c${ctx.index}-sched-new`,
    onClick: () => {
      dismiss(key);
      newSchedule();
    } }, "+ New schedule");
}

function picker(ctx, names) {
  const locked = lockedKind(ctx.choice);
  const shown = locked || open.kind;
  return h("div", { class: "sched-pop", role: "dialog", "aria-label": "Add a schedule" },
    names.length > 0 && h("div", { class: "sched-switch", role: "group", "aria-label": "Mode" },
      KINDS.map((k) => modeButton(ctx, k, shown, locked))),
    names.map((name) => pickOption(ctx, shown, name)),
    newOption(ctx));
}

// focusFirst is the control the picker focuses when it opens: the
// switch's side, or "+ New schedule" when no schedule is left to attach.
function focusFirst(ctx, free) {
  return free.length > 0 ? `c${ctx.index}-sched-${open.kind}` : `c${ctx.index}-sched-new`;
}

function addButton(ctx, free) {
  const key = cardKey(ctx);
  const isOpen = open !== null && open.key === key;
  return h("button", { type: "button", class: "sched-add", "aria-expanded": String(isOpen),
    "data-key": `c${ctx.index}-sched-add`,
    // Whether it is open is read at click time: dismiss() closes the picker
    // without redrawing this button, so the render-time isOpen goes stale.
    onClick: () => {
      if (open !== null && open.key === key) {
        dismiss(key);
        return;
      }
      open = { key, kind: lockedKind(ctx.choice) || "skip_during" };
      refill(key, focusFirst(ctx, free));
    } }, "+ schedule");
}

function fill(container, ctx) {
  clear(container);
  const free = available(scheduleNames(), ctx.choice);
  const key = cardKey(ctx);
  for (const a of attached(ctx.choice)) container.append(chip(ctx, a));
  container.append(h("div", { class: "sched-wrap" }, addButton(ctx, free),
    open !== null && open.key === key ? picker(ctx, free) : null));
}

// refill redraws one card's chips in place and focuses the control with
// data-key want (null: leave focus alone).
function refill(key, want) {
  const m = mounts.get(key);
  if (!m || !m.container.isConnected) return;
  fill(m.container, m.ctx);
  const el = want ? [...m.container.querySelectorAll("[data-key]")].find((x) => x.dataset.key === want) : null;
  if (el) el.focus();
}

// ---------- applying ----------

// edit sends kind's list as change() makes it from the card as it stands
// when the op's turn comes. refocus then puts focus on the card's
// + schedule (after a remove, the clicked x is gone).
function edit(key, kind, change, refocus) {
  chain = chain.then(async () => {
    const ctx = liveCtx(key);
    if (!ctx) return;
    const cur = ctx.choice[kind] || [];
    const next = change(cur);
    if (JSON.stringify(next) === JSON.stringify(cur)) return;
    await sendOp("choice.set", { profile: ctx.profile, index: ctx.index, expect: ctx.choice,
      field: kind, value: next });
    if (refocus) focusAdd(key);
  }).catch(() => {});
  return chain;
}

function focusAdd(key) {
  const m = mounts.get(key);
  const b = m && m.container.isConnected ? m.container.querySelector(".sched-add") : null;
  if (b && !b.disabled) b.focus();
}

// dismiss closes the picker by removing its node only: the chip area is not
// rebuilt, so a press that began on a chip's x still ends on it.
function dismiss(key) {
  const m = mounts.get(key);
  open = null;
  if (!m || !m.container.isConnected) return;
  const pop = m.container.querySelector(".sched-pop");
  if (pop) pop.remove();
  const add = m.container.querySelector(".sched-add");
  if (add) add.setAttribute("aria-expanded", "false");
}

function choose(key, kind, name) {
  const m = mounts.get(key);
  open = null;
  if (m) refill(key, `c${m.ctx.index}-sched-add`);
  edit(key, kind, (list) => (list.includes(name) ? list : [...list, name]));
}

// close shuts the picker and puts focus back on its + schedule.
function close() {
  const key = open.key;
  dismiss(key);
  focusAdd(key);
}

function listen() {
  if (listening) return;
  listening = true;
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || open === null || document.querySelector("dialog[open]")) return;
    e.stopPropagation();
    close();
  });
  document.addEventListener("pointerdown", (e) => {
    if (open === null) return;
    const m = mounts.get(open.key);
    const wrap = m && m.container.isConnected ? m.container.querySelector(".sched-wrap") : null;
    if (wrap && wrap.contains(e.target)) return;
    dismiss(open.key);
  });
}

// prune forgets the cards a repaint did not render again, and closes a
// picker whose card is gone.
function prune() {
  pruning = false;
  for (const [key, m] of mounts) if (!m.container.isConnected) mounts.delete(key);
  if (open !== null && !mounts.has(open.key)) open = null;
}

// renderChips fills container with the card's schedule chips. ctx is
// {profile, index, choice}: profile is the profile's name, index the
// choice's position (0-based), choice the choice as rendered (sent as
// `expect` with every choice.* op).
export function renderChips(container, ctx) {
  listen();
  mounts.set(cardKey(ctx), { container, ctx });
  fill(container, ctx);
  if (!pruning) {
    pruning = true;
    queueMicrotask(prune);
  }
}
