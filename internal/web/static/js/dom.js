// Element builder. Text is always added as text nodes, never parsed as HTML,
// so names from the config cannot inject markup.

function setAttr(el, key, value) {
  if (value === null || value === undefined || value === false) return;
  if (key === "style") throw new Error("h: no style attribute (CSP); use a class or el.style");
  if (key.startsWith("on") && typeof value === "function") {
    el.addEventListener(key.slice(2).toLowerCase(), value);
    return;
  }
  if (key === "class") {
    el.className = Array.isArray(value) ? value.filter(Boolean).join(" ") : value;
    return;
  }
  el.setAttribute(key, value === true ? "" : String(value));
}

// h(tag, attrs, ...children): attrs takes class (string or list), aria-*,
// data-*, any plain attribute (true = present, false/null = absent) and
// event handlers as onClick, onKeyDown, …. Children are nodes or text;
// arrays are flattened and null/false/undefined skipped.
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) setAttr(el, k, v);
  for (const c of children.flat(Infinity)) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : String(c));
  }
  return el;
}

export function clear(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

const SVG = "http://www.w3.org/2000/svg";

// svg(w, view, attrs, ...parts) draws a w×w stroked icon; each part is
// [tag, attrs]. It is decorative (aria-hidden): the button carries the label.
export function svg(w, view, attrs, ...parts) {
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

// trashIcon is the delete glyph shared by the caps frame and the schedule list.
export function trashIcon() {
  return svg(16, "0 0 16 16", { "stroke-width": "1.5", "stroke-linecap": "round" },
    ["path", { d: "M2.5 4h11M6.5 4V2.5h3V4M4 4l.7 9.5h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5" }]);
}

// removeIcon is the x before the word on every Remove button.
export function removeIcon() {
  return svg(14, "0 0 16 16", { "stroke-width": "1.6", "stroke-linecap": "round" },
    ["path", { d: "M4 4l8 8M12 4l-8 8" }]);
}
