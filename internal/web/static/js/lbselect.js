// Creating a load balancing group (spec §8, "Creating"). ⇉ Load balance…
// at the top of the choice list starts a selection: each plain card shows
// a checkbox in place of its drag handle, and the bar offers Load balance
// N models (from 2 selected) and Cancel. Confirming sends group.create.
// Cancel, Esc, another profile or new state end the selection. profiles.js
// draws the cards and makes their other controls inert meanwhile.
import { h } from "./dom.js";
import { store, sendOp } from "./api.js";

// sel is the selection in progress, or null: the profile, the state the
// page was drawn from, and picked, each checked card's ctx by flat index.
let sel = null;

// selection returns the selection on profile, or null. One made on another
// profile, or before the state changed, ends here.
export function selection(profile) {
  if (sel && (sel.profile !== profile || sel.state !== store.state)) sel = null;
  return sel;
}

// endSelection ends the selection; it says whether there was one.
export function endSelection() {
  const was = sel !== null;
  sel = null;
  return was;
}

// confirm ends the selection and sends group.create. The args are built
// when the op's turn comes, from each card's ctx as its own edits left it
// (profiles.js setField), in file order.
async function confirm(repaint) {
  const s = sel;
  sel = null;
  repaint("lb-start");
  const ctxs = [...s.picked.values()].sort((a, b) => a.index - b.index);
  await sendOp("group.create",
    () => ({ profile: s.profile, indexes: ctxs.map((c) => c.index), expects: ctxs.map((c) => c.choice) }));
}

function selectingBar(repaint) {
  const n = sel.picked.size;
  return h("div", { class: "p-lb" },
    h("button", { type: "button", class: "btn btn-primary", "data-key": "lb-confirm", disabled: n < 2,
      onClick: () => confirm(repaint) }, `Load balance ${n} models`),
    h("button", { type: "button", class: "btn", "data-key": "lb-cancel",
      onClick: () => {
        sel = null;
        repaint("lb-start");
      } }, "Cancel"));
}

// lbBar is the row at the top of the choice list. repaint(key) redraws the
// tab with focus on data-key key; first is the first checkbox's data-key.
export function lbBar(profile, first, repaint) {
  if (selection(profile)) return selectingBar(repaint);
  return h("div", { class: "p-lb" },
    h("button", { type: "button", class: "btn", "data-key": "lb-start",
      onClick: () => {
        sel = { profile, state: store.state, picked: new Map() };
        repaint(first);
      } }, "⇉ Load balance…"));
}

export function pickKey(index) {
  return `c${index}-pick`;
}

// lbCheck is a plain card's checkbox, named after the card ("Select
// choice 3"). It stands in the handle's place, so it is as large.
export function lbCheck(ctx, label, repaint) {
  return h("span", { class: "p-pick" },
    h("input", { type: "checkbox", "aria-label": `Select choice ${label}`, "data-key": pickKey(ctx.index),
      checked: sel.picked.has(ctx.index),
      onChange: (e) => {
        if (e.target.checked) sel.picked.set(ctx.index, ctx);
        else sel.picked.delete(ctx.index);
        repaint();
      } }));
}

// isPicked says whether the card at index is checked.
export function isPicked(index) {
  return sel !== null && sel.picked.has(index);
}
