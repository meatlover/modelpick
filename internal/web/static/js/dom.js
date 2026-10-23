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
