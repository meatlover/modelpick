// Profiles tab: the profile list, and the selected profile's choices as
// cards (#profiles/<name>; the first profile when the name is missing or
// unknown), under one row holding the Global default cap and the cap
// Example. Every edit is one op; a refusal re-renders from the store, which
// reverts the control.
import { h, clear, trashIcon } from "./dom.js";
import { store, sendOp } from "./api.js";
import { confirmDialog } from "./notice.js";
import { currentTab } from "./header.js";
import { renderCaps } from "./caps.js";
import { renderChips } from "./chips.js";
import { wireReorder, dragHandle } from "./reorder.js";
import { choiceSubject, profileHasError } from "./subjects.js";

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

// ctx.choice is the choice as this card's own edits left it. A second edit
// made before the first one's redraw is built when its turn comes, from
// what the first left, not from the choice the card was drawn with.
async function setField(ctx, field, value) {
  const r = await sendOp("choice.set",
    () => ({ profile: ctx.profile, index: ctx.index, expect: ctx.choice, field, value }),
    { done: (res) => advance(ctx, res) });
  if (!r.ok) paint();
}

// advance moves ctx.choice to what the op left: choice.set keeps the index,
// and no other op runs between the op and done.
function advance(ctx, res) {
  if (!res.ok || !store.state) return;
  const prof = store.state.profiles.find((p) => p.name === ctx.profile);
  if (prof && prof.choices[ctx.index]) ctx.choice = prof.choices[ctx.index];
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
    return h("select", { "data-key": key, disabled: true }, h("option", { value: "" }, "—"));
  }
  return select(key, null, (v) => setField(ctx, "thinking", v === "" ? null : v),
    options(levels, c.thinking, (v) => (v === null ? "—" : v)));
}

function field(label, control) {
  return h("label", { class: "p-field" }, label, control);
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

function card(st, profile, c, i, red) {
  const ctx = { profile, index: i, choice: c };
  const handle = h("span", { class: "p-handle", "aria-hidden": "true" }, dots());
  const el = h("div", { class: ["p-card", red.has(choiceSubject(profile, i)) && "is-error"],
    tabindex: "0", role: "group", "aria-label": `Choice ${i + 1}`, "data-key": `card-${i}`, "data-index": String(i) },
  h("div", { class: "p-pos" }, handle, h("span", { class: "mono p-n" }, String(i + 1))),
  h("div", { class: "p-card-main" }, selects(st, ctx),
    mount("p-caps", renderCaps, ctx), mount("p-chips", renderChips, ctx)),
  h("div", { class: "p-card-side" },
    h("button", { type: "button", class: "btn btn-quiet", "aria-label": `Remove choice ${i + 1}`,
      "data-key": `c${i}-remove`,
      onClick: () => sendOp("choice.remove", () => ({ profile, index: i, expect: ctx.choice })) }, "Remove")));
  dragHandle(el, handle);
  return el;
}

// move keeps focus (and the selected style) on the moved card, whether it
// was dragged or moved with the keyboard; a refusal puts it back.
async function move(prof, from, to) {
  focusKey = `card-${to}`;
  const r = await sendOp("choice.move", { profile: prof.name, from, to, expect: prof.choices[from] });
  if (r.ok) return;
  focusKey = `card-${from}`;
  paint();
}

function cards(st, prof, red) {
  const ol = h("ol", { class: "p-cards" },
    prof.choices.map((c, i) => h("li", { class: "p-item" }, card(st, prof.name, c, i, red))));
  wireReorder(ol, (from, to) => move(prof, from, to));
  return ol;
}

function profileMain(st, prof, red) {
  return h("div", { class: "p-main" },
    capRow(st, true),
    cards(st, prof, red),
    h("button", { type: "button", class: "btn btn-lg btn-dashed p-add", "data-key": "add-choice",
      onClick: () => sendOp("choice.add", { profile: prof.name }) }, "+ Add Model"));
}

function paint() {
  if (!host || currentTab() !== "profiles") return;
  listen();
  const st = store.state;
  clear(host);
  if (!st) return;
  const red = errorSubjects();
  const prof = selected(st.profiles);
  host.append(h("div", { class: "p-body" }, profileList(st.profiles, prof, red),
    prof ? profileMain(st, prof, red) : h("div", { class: "p-main" }, capRow(st, false))));
  restoreFocus();
}

export function render(root) {
  host = root;
  paint();
}
