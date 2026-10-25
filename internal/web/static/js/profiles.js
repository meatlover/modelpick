// Profiles tab: the profile list, and the selected profile's choices as
// cards (#profiles/<name>; the first profile when the name is missing or
// unknown), under one row holding the Global default cap and the cap
// Example. Every edit is one op; a refusal re-renders from the store, which
// reverts the control.
import { h, clear, trashIcon } from "./dom.js";
import { store, sendOp, onlyFieldEdits } from "./api.js";
import { confirmDialog } from "./notice.js";
import { currentTab } from "./header.js";
import { renderCaps } from "./caps.js";
import { renderChips } from "./chips.js";
import { wireReorder, dragHandle } from "./reorder.js";
import { choiceLabels, choiceSubject, positions, profileHasError } from "./subjects.js";
import { selection, endSelection, lbBar, lbCheck, isPicked } from "./lbselect.js";

export const uses = ["state", "doctor"];

const SVG = "http://www.w3.org/2000/svg";

let host = null;
// focusKey is the data-key of the element that had focus; paint puts focus
// back on it, since every repaint rebuilds the tab.
let focusKey = null;
let listening = false;
// The New profile input is open (adding) holding newDraft; capDraft is the
// Default cap text being typed (null: show the stored value). Both survive
// a repaint.
let adding = false;
let newDraft = "";
let capDraft = null;

function listen() {
  if (listening) return;
  listening = true;
  document.addEventListener("pointerdown", (e) => {
    if (currentTab() !== "profiles") return;
    const k = e.target.closest ? e.target.closest("[data-key]") : null;
    focusKey = k ? k.dataset.key : null;
  });
  document.addEventListener("keydown", (e) => {
    if (currentTab() !== "profiles" || e.key !== "Escape" || e.defaultPrevented) return;
    if (e.target.closest && e.target.closest("input[type=text], textarea, dialog")) return;
    if (endSelection()) repaint("lb-start");
  });
  document.addEventListener("focusin", (e) => {
    if (currentTab() !== "profiles") return;
    const t = e.target;
    if (t && t.dataset && t.dataset.key !== undefined) focusKey = t.dataset.key;
    else if (t !== document.body && t !== document.documentElement) focusKey = null;
  });
}

function restoreFocus() {
  if (focusKey === null) return;
  const a = document.activeElement;
  if (a && a !== document.body && !host.contains(a)) return;
  const el = [...host.querySelectorAll("[data-key]")].find((x) => x.dataset.key === focusKey);
  if (el && el !== a) el.focus();
}

// errorSubjects is the set of doctor error subjects (subjects.js).
function errorSubjects() {
  const found = store.doctor ? store.doctor.findings || [] : [];
  return new Set(found.filter((f) => f.severity === "error").map((f) => f.subject));
}

function selected(profiles) {
  const rest = location.hash.slice(1).split("/").slice(1).join("/");
  let want = "";
  try {
    want = decodeURIComponent(rest);
  } catch {
    // a malformed name selects the first profile
  }
  return profiles.find((p) => p.name === want) || profiles[0] || null;
}

function go(name) {
  const want = `#profiles/${encodeURIComponent(name)}`;
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

// ---------- Global default cap ----------

// capValue reads "95", "95%" or "95 %" as 95, clamped to 0–100; anything
// else is null.
function capValue(text) {
  let t = text.trim();
  if (t.endsWith("%")) t = t.slice(0, -1).trim();
  const n = t === "" ? NaN : Number(t);
  return Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : null;
}

// commitCap saves the typed cap. Garbage, or the stored value, sends
// nothing; the repaint then shows the stored value, so the input always
// shows what was saved ("150" saves and shows "100%").
async function commitCap(el) {
  el.dataset.sent = el.value;
  const value = capValue(el.value);
  capDraft = null;
  if (value === null || value === store.state.default_cap) {
    if (el.value !== `${store.state.default_cap}%`) paint();
    return;
  }
  const r = await sendOp("settings.default_cap", { value });
  if (!r.ok) paint();
}

function capInput(st) {
  return h("input", { type: "text", class: "input p-cap-input", id: "p-default-cap", "data-key": "default-cap",
    value: capDraft !== null ? capDraft : `${st.default_cap}%`,
    onInput: (e) => { capDraft = e.target.value; },
    onKeyDown: (e) => {
      if (e.key === "Enter") commitCap(e.target);
      if (e.key === "Escape") {
        capDraft = null;
        paint();
      }
    },
    onBlur: (e) => afterBlur(e.target, () => commitCap(e.target)) });
}

// exampleBox draws a cap box as a card shows it (caps.js classes, so it has
// the same size and look): dashed for the default cap, solid for a set one.
// The text sits in a smaller inner span so "customized cap" fits the box.
function exampleBox(dashed, text) {
  return h("span", { class: ["cap-box", dashed && "is-default"] },
    h("span", { class: "mono cap-win" }, "5h"),
    h("span", { class: "cap-val" }, h("span", { class: "p-defcap-val" }, text)));
}

// capGroup is a label and its value, kept together on one line.
function capGroup(label, value) {
  return h("div", { class: "p-defcap-group" }, label, value);
}

// capRow tops the main column: the Global default cap, then how a default
// and a customized cap look. It is laid out like a card: an empty column
// as wide as the card's handle column, so it starts where a card's
// Account label does. The Example is left out when no profile is shown.
function capRow(st, withExample) {
  return h("div", { class: "p-defcap" },
    h("span", { class: "p-pos", "aria-hidden": "true" }),
    h("div", { class: "p-defcap-line" },
      capGroup(h("label", { class: "p-defcap-label", for: "p-default-cap" }, "Global default cap"), capInput(st)),
      withExample && capGroup(h("span", { class: "p-defcap-label is-quiet" }, "Example:"),
        h("div", { class: "p-defcap-example" }, exampleBox(true, "default cap"), exampleBox(false, "customized cap")))));
}

// ---------- profile list ----------

function closeNew(keepFocus) {
  adding = false;
  newDraft = "";
  if (keepFocus) focusKey = "new-profile-btn";
  paint();
}

async function addProfile(el) {
  const name = el.value.trim();
  if (!name) {
    closeNew(false);
    return;
  }
  el.dataset.sent = el.value;
  const r = await sendOp("profile.add", { name });
  if (!r.ok) return; // the toast says why; the input keeps the name
  adding = false;
  newDraft = "";
  focusKey = `pl-${name}`;
  go(name);
}

function newInput() {
  return h("input", { type: "text", class: "input p-new-input", "aria-label": "New profile name",
    "data-key": "new-profile", value: newDraft,
    onInput: (e) => { newDraft = e.target.value; },
    onKeyDown: (e) => {
      if (e.key === "Enter") addProfile(e.target);
      if (e.key === "Escape") closeNew(true);
    },
    onBlur: (e) => afterBlur(e.target, () => addProfile(e.target)) });
}

function openNew() {
  adding = true;
  newDraft = "";
  focusKey = "new-profile";
  paint();
}

// listRow is a profile's link with its Delete button laid over the link's
// right end (a link cannot hold a button); the glyph shows on hover or
// focus in the row.
function listRow(p, sel, red) {
  return h("div", { class: "p-row" },
    h("a", { href: `#profiles/${encodeURIComponent(p.name)}`,
      class: ["p-link", profileHasError(red, p.name) && "is-error"],
      "aria-current": p === sel ? "true" : null, "data-key": `pl-${p.name}` }, p.name),
    h("button", { type: "button", class: "p-del", "data-key": `pl-del-${p.name}`,
      "aria-label": `Delete profile ${p.name}`, onClick: () => deleteProfile(p.name) }, trashIcon()));
}

function profileList(profiles, sel, red) {
  return h("aside", { class: "p-list", "aria-label": "Profiles" },
    h("h2", { class: "p-list-title" }, "Profiles"),
    profiles.map((p) => listRow(p, sel, red)),
    adding ? newInput()
      : h("button", { type: "button", class: "btn btn-lg btn-dashed p-new", "data-key": "new-profile-btn",
        onClick: openNew }, "+ New profile"));
}

// deleteProfile always asks first. Deleting the profile on screen shows
// the first one; focus on the deleted row's button moves to + New profile.
async function deleteProfile(name) {
  if (!(await confirmDialog(`Delete profile ${name}?`, "Delete"))) return;
  const shown = selected(store.state.profiles);
  const r = await sendOp("profile.remove", { name });
  if (r.ok && shown && shown.name === name) history.replaceState(null, "", "#profiles");
  if (r.ok && focusKey === `pl-del-${name}`) focusKey = "new-profile-btn";
  paint();
}

// ---------- cards ----------

// ctxs keeps each card's ctx for as long as the state it was drawn from is
// the store's: a repaint that brings no new state (a selection, a doctor
// reload) draws the card with the ctx its pending edits will advance. A
// ctx's sha is that state's (taken when it arrives: an op patches the
// store's sha256 before its reload brings the new state). ctx.base is the
// choice as the file holds it after this card's own answered edits;
// ctx.choice, which the card is drawn from, is base with its pending ones.
let ctxState = null;
let ctxSha = "";
let ctxs = new Map();

function ctxFor(profile, index, choice) {
  if (ctxState !== store.state) {
    ctxState = store.state;
    ctxSha = store.state.sha256;
    ctxs = new Map();
  }
  const key = `${profile}\u0000${index}`;
  if (!ctxs.has(key)) ctxs.set(key, { profile, index, key, base: choice, choice: shownChoice(key, choice), sha: ctxSha });
  return ctxs.get(key);
}

// pendingSets lists the choice.sets queued or in flight, oldest first, as
// {key, sha, field, value}: the card's ctx key and sha when the edit was
// made, and the value the card then showed.
let pendingSets = [];

// shownChoice is base with the pending edits of card key over it, those
// still at this index (only this page's field edits came after the state
// each was made on), so that a redraw from an earlier op's reload shows
// what the user chose rather than the file before it.
function shownChoice(key, base) {
  let c = base;
  for (const p of pendingSets) {
    if (p.key === key && onlyFieldEdits(p.sha)) c = { ...c, [p.field]: p.value };
  }
  return c;
}

// expectOf is an op's expect for the card of ctx, built when the op's turn
// comes. When only this page's field edits came after the card was drawn,
// it is the choice at the card's index now: the card was redrawn from an
// earlier of those edits while a later one was in flight, so ctx.base,
// advanced only by its own ops, may miss one. Otherwise it is ctx.base,
// and the server finds the card if it moved.
function expectOf(ctx) {
  if (!onlyFieldEdits(ctx.sha)) return ctx.base;
  const prof = store.state.profiles.find((p) => p.name === ctx.profile);
  return prof && prof.choices[ctx.index] ? prof.choices[ctx.index] : ctx.base;
}

// sendSet sends choice.set of field on ctx's card, for caps.js and chips.js
// too (ctx.set). The card shows value until the op is answered; the op is
// built when its turn comes, value being build(expect) when given, so that
// it changes what earlier edits left. Answered, the ctx advances.
function sendSet(ctx, field, value, build = () => value) {
  const p = { key: ctx.key, sha: ctx.sha, field, value };
  pendingSets.push(p);
  ctx.choice = shownChoice(ctx.key, ctx.base);
  return sendOp("choice.set", () => {
    const expect = expectOf(ctx);
    return { profile: ctx.profile, index: ctx.index, expect, member_of: ctx.memberOf(), field, value: build(expect) };
  }, { done: (res) => settle(ctx, p, res) });
}

// settle drops the answered edit p: ctx's base moves to what the op left
// (choice.set keeps the index, and no other op runs between the op and
// done), and ctx and the card's ctx drawn since show what is still pending.
function settle(ctx, p, res) {
  pendingSets = pendingSets.filter((q) => q !== p);
  if (res.ok && store.state) {
    const prof = store.state.profiles.find((q) => q.name === ctx.profile);
    if (prof && prof.choices[ctx.index]) ctx.base = prof.choices[ctx.index];
  }
  for (const c of new Set([ctx, ctxs.get(ctx.key)])) if (c) c.choice = shownChoice(c.key, c.base);
}

async function setField(ctx, field, value) {
  const r = await sendSet(ctx, field, value);
  if (!r.ok) paint();
}

// options lists values, with the current value first when the list lacks
// it (a hand-written config), so the select shows what the file says.
function options(values, current, label = (v) => v) {
  const list = values.includes(current) ? values : [current, ...values];
  return list.map((v) => h("option", { value: v === null ? "" : v, selected: v === current }, label(v)));
}

function select(key, cls, onChange, opts) {
  return h("select", { class: cls, "data-key": key, onChange: (e) => onChange(e.target.value) }, opts);
}

function thinkingSelect(st, ctx) {
  const c = ctx.choice;
  const model = (st.models[c.account] || []).find((m) => m.id === c.model);
  const levels = model ? model.thinking || [] : [];
  const key = `c${ctx.index}-thinking`;
  if (!levels.length && c.thinking === null) {
    return h("select", { class: "p-none", "data-key": key, disabled: true }, h("option", { value: "" }, "—"));
  }
  return select(key, null, (v) => setField(ctx, "thinking", v === "" ? null : v),
    options(levels, c.thinking, (v) => (v === null ? "—" : v)));
}

// field labels a card's dropdown. The name is read by assistive technology
// only: the names show once, above the cards (cols).
function field(label, control) {
  return h("label", { class: "p-field" }, h("span", { class: "vh" }, label), control);
}

// cols names the card's dropdowns once, over their columns (owner,
// 2026-10-24). The empty position column and a hidden Remove keep the
// names over the dropdowns whatever Remove's width.
function cols() {
  return h("div", { class: "p-cols", "aria-hidden": "true" },
    h("span", { class: "p-pos" }),
    h("div", { class: "p-cols-names" }, h("span", {}, "Account"), h("span", {}, "Model"), h("span", {}, "Thinking")),
    h("span", { class: "btn btn-quiet p-cols-side" }, "Remove"));
}

function selects(st, ctx) {
  const c = ctx.choice;
  const i = ctx.index;
  const accounts = st.accounts.map((a) => a.name);
  const models = (st.models[c.account] || []).map((m) => m.id);
  return h("div", { class: "p-selects" },
    field("Account", select(`c${i}-account`, null, (v) => setField(ctx, "account", v),
      options(accounts, c.account))),
    field("Model", select(`c${i}-model`, "mono", (v) => setField(ctx, "model", v),
      options(models, c.model))),
    field("Thinking", thinkingSelect(st, ctx)));
}

function dots() {
  const svg = document.createElementNS(SVG, "svg");
  for (const [k, v] of [["width", "14"], ["height", "20"], ["viewBox", "0 0 14 20"], ["aria-hidden", "true"]]) {
    svg.setAttribute(k, v);
  }
  for (const y of [4, 10, 16]) {
    for (const x of [4, 10]) {
      const dot = document.createElementNS(SVG, "circle");
      dot.setAttribute("cx", String(x));
      dot.setAttribute("cy", String(y));
      dot.setAttribute("r", "1.6");
      svg.append(dot);
    }
  }
  return svg;
}

function mount(cls, fill, ctx) {
  const el = h("div", { class: cls });
  fill(el, ctx);
  return el;
}

// view is what every card of the shown profile is drawn from: st, prof,
// red (errorSubjects), labels (choiceLabels), lb, the selection in
// progress (lbselect.js) or null, and ctxs, each card's ctx by flat index.

// groupExpect is the group holding flat index i as the file holds it,
// {load_balance, choices}, from its members' expectOf: called when an op's
// turn comes, it is the group as earlier edits left it.
function groupExpect(v, i) {
  const pos = positions(v.prof.choices).find((p) => i >= p.start && i < p.end);
  return { load_balance: pos.load_balance, choices: v.ctxs.slice(pos.start, pos.end).map(expectOf) };
}

// memberOf is the member_of of an op on the card at flat index i: the
// group holding it (groupExpect, so build it when the op's turn comes), or
// null for a plain choice. The server looks for a stale member only there.
function memberOf(v, i) {
  return v.prof.choices[i].group > 0 ? groupExpect(v, i) : null;
}

// hookCtxs gives each card's ctx what its ops need from the view, for
// caps.js and chips.js too: set() sends a choice.set (sendSet) and
// memberOf() builds an op's member_of.
function hookCtxs(v) {
  v.ctxs.forEach((ctx, i) => {
    ctx.set = (field, value, build) => sendSet(ctx, field, value, build);
    ctx.memberOf = () => memberOf(v, i);
  });
  return v;
}

// cardPos is a card's position column: its drag handle over its label.
// While selecting, the handle's place holds the checkbox, or stays empty on
// a group's member, which cannot be selected.
function cardPos(v, ctx, member) {
  const label = v.labels[ctx.index];
  const head = !v.lb ? h("span", { class: "p-handle", "aria-hidden": "true" }, dots())
    : member ? h("span", { class: "p-pick", "aria-hidden": "true" }) : lbCheck(ctx, label, repaint);
  return h("div", { class: "p-pos" }, head, h("span", { class: "mono p-n" }, label));
}

// card draws the choice at flat index i, labelled "3", or "2b" for a member
// of a load balancing group. While selecting, everything but the checkbox
// is inert and the card does not drag.
function card(v, i, member) {
  const ctx = v.ctxs[i];
  const label = v.labels[i];
  const pos = cardPos(v, ctx, member);
  const el = h("div", { class: ["p-card", v.red.has(choiceSubject(ctx.profile, label)) && "is-error",
    isPicked(i) && "is-picked"], tabindex: v.lb ? null : "0", role: "group", "aria-label": `Choice ${label}`,
  "data-key": `card-${i}`, "data-index": String(i) },
  pos,
  h("div", { class: "p-card-main", inert: Boolean(v.lb) }, selects(v.st, ctx),
    mount("p-caps", renderCaps, ctx), mount("p-chips", renderChips, ctx)),
  h("div", { class: "p-card-side", inert: Boolean(v.lb) },
    h("button", { type: "button", class: "btn btn-quiet", "aria-label": `Remove choice ${label}`,
      "data-key": `c${i}-remove`,
      onClick: () => sendOp("choice.remove",
        () => ({ profile: ctx.profile, index: i, expect: expectOf(ctx), member_of: memberOf(v, i) })) },
    "Remove")));
  if (!v.lb) dragHandle(el, pos.firstChild);
  return el;
}

// groupHead is a group's header: what it is, its algorithm (as text while
// there is one algorithm; spec §8 makes it a menu once there are two), and
// Stop load balancing, which dissolves it. Its handle, first, drags the
// whole group; while selecting an empty box keeps its place.
function groupHead(v, pos, n, expect) {
  const k = pos.end - pos.start;
  return h("div", { class: "p-group-head" },
    h("span", { class: v.lb ? "p-group-grip" : "p-handle", "aria-hidden": "true" }, v.lb ? null : dots()),
    h("span", { class: "p-group-title" }, `${n} · Load balancing ${k} models`),
    h("span", { class: "p-group-how" }, `· algorithm: ${pos.load_balance}`),
    h("button", { type: "button", class: "btn btn-quiet p-group-stop", "data-key": `group-${pos.start}-stop`,
      inert: Boolean(v.lb),
      onClick: () => sendOp("group.dissolve", () => ({ profile: v.prof.name, index: pos.start, expect: expect() })) },
    "Stop load balancing"));
}

// groupFrame draws the load balancing group at position n (pos from
// positions()): a frame named Choice n holding its member cards, with
// + Add Model at the bottom. Its ops' expect is groupExpect. Focused, the
// frame moves the whole group by ⌘↑/⌘↓, as its handle drags it.
function groupFrame(v, pos, n) {
  const members = [];
  for (let i = pos.start; i < pos.end; i++) members.push(i);
  const expect = () => groupExpect(v, pos.start);
  const head = groupHead(v, pos, n, expect);
  const frame = h("div", { class: ["p-group", v.red.has(choiceSubject(v.prof.name, String(n))) && "is-error"],
    tabindex: v.lb ? null : "0", role: "group", "aria-label": `Choice ${n}`, "data-key": `group-${pos.start}` },
  head,
  h("ol", { class: "p-members" }, members.map((i) => h("li", { class: "p-member" }, card(v, i, true)))),
  h("button", { type: "button", class: "btn btn-dashed p-group-add", "data-key": `group-${pos.start}-add`,
    inert: Boolean(v.lb),
    onClick: () => sendOp("choice.add", () => ({ profile: v.prof.name, group: pos.start, group_expect: expect() })) },
  "+ Add Model"));
  if (!v.lb) dragHandle(frame, head.firstChild);
  return frame;
}

// move sends choice.move; group is null (the card lands plain) or the flat
// index of a member of the group it lands in. Its expects are built when
// the op's turn comes, from the ctxs. Focus (and the selected style) stays
// on the moved card, whether it was dragged or moved with the keyboard,
// and also when the move dissolved its group; a refusal puts it back.
async function move(v, from, to, group) {
  focusKey = `card-${to}`;
  const r = await sendOp("choice.move", () => ({ profile: v.prof.name, from, to, expect: expectOf(v.ctxs[from]),
    member_of: memberOf(v, from), group, group_expect: group === null ? null : groupExpect(v, group) }));
  if (r.ok) return;
  focusKey = `card-${from}`;
  paint();
}

// moveGroup sends group.move for the group whose first member is index;
// focus stays on its frame, whose first member will be at start.
async function moveGroup(v, index, to, start) {
  focusKey = `group-${start}`;
  const r = await sendOp("group.move", () => ({ profile: v.prof.name, index, expect: groupExpect(v, index), to }));
  if (r.ok) return;
  focusKey = `group-${index}`;
  paint();
}

// cards lists the profile's positions: a plain choice's card, or a group's
// frame. Cards keep their flat index (data-index), which every op uses.
// Nothing moves while selecting.
function cards(v) {
  const ol = h("ol", { class: "p-cards" }, positions(v.prof.choices).map((pos, n) => (pos.group > 0
    ? h("li", { class: "p-item p-group-item" }, groupFrame(v, pos, n + 1))
    : h("li", { class: "p-item" }, card(v, pos.start, false)))));
  if (!v.lb) {
    wireReorder(ol, { card: (from, to, group) => move(v, from, to, group),
      group: (index, to, start) => moveGroup(v, index, to, start) });
  }
  return ol;
}

function profileMain(st, prof, red) {
  const v = hookCtxs({ st, prof, red, labels: choiceLabels(prof.choices), lb: selection(prof.name),
    ctxs: prof.choices.map((c, i) => ctxFor(prof.name, i, c)) });
  return h("div", { class: ["p-main", v.lb && "is-selecting"] },
    capRow(st, true),
    lbBar(prof.name, positions(prof.choices).filter((pos) => pos.group === 0), repaint),
    cols(),
    cards(v),
    h("button", { type: "button", class: "btn btn-lg btn-dashed p-add", "data-key": "add-choice", inert: Boolean(v.lb),
      onClick: () => sendOp("choice.add", { profile: prof.name }) }, "+ Add Model"));
}

// repaint redraws the tab, with focus on data-key key when one is given.
function repaint(key) {
  if (key !== undefined) focusKey = key;
  paint();
}

function paint() {
  if (!host || currentTab() !== "profiles") return;
  listen();
  const st = store.state;
  clear(host);
  if (!st) return;
  const red = errorSubjects();
  const prof = selected(st.profiles);
  if (!prof) endSelection();
  host.append(h("div", { class: "p-body" }, profileList(st.profiles, prof, red),
    prof ? profileMain(st, prof, red) : h("div", { class: "p-main" }, capRow(st, false))));
  restoreFocus();
}

export function render(root) {
  host = root;
  paint();
}
