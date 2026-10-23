// The shared header: name, config path, tabs, UTC/Local switch, doctor status.
import { h, clear } from "./dom.js";
import { store } from "./api.js";
import { zone, setZone, onZone, localZoneName } from "./time.js";

export const TABS = [["usage", "Usage"], ["profiles", "Profiles"], ["schedules", "Schedules"], ["accounts", "Accounts"]];

// currentTab is the tab the hash names, "usage" for anything else. A tab
// may carry a sub-path after a slash (#profiles/<name>).
export function currentTab() {
  const id = location.hash.slice(1).split("/")[0];
  return TABS.some(([t]) => t === id) ? id : "usage";
}

function markTab(nav) {
  const cur = currentTab();
  for (const a of nav.querySelectorAll("a")) {
    if (a.dataset.tab === cur) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  }
}

function zoneSwitch() {
  const utc = h("button", { type: "button", onClick: () => setZone("utc") }, "UTC");
  const local = h("button", { type: "button", onClick: () => setZone("local") }, `Local (${localZoneName()})`);
  const mark = (z) => {
    utc.setAttribute("aria-pressed", String(z === "utc"));
    local.setAttribute("aria-pressed", String(z === "local"));
  };
  onZone(mark);
  mark(zone());
  return h("div", { class: "seg", role: "group", "aria-label": "Time zone" }, utc, local);
}

function plural(n, word) {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

function level(doctor) {
  if (doctor.errors > 0) return ["error", plural(doctor.errors, "error")];
  if (doctor.warnings > 0) return ["warn", plural(doctor.warnings, "warning")];
  return ["ok", "config ok"];
}

function findingItem(f) {
  const err = f.severity === "error";
  return h("li", {},
    h("strong", { class: err ? "sev sev-error" : "sev sev-warn" }, err ? "ERROR" : "WARNING"),
    ` ${f.subject}: ${f.message}`);
}

function fillStatus(parts, doctor) {
  parts.wrap.hidden = !doctor;
  if (!doctor) return;
  const [kind, text] = level(doctor);
  parts.btn.className = `status status-${kind}`;
  parts.label.textContent = text;
  const all = doctor.findings || [];
  parts.findings = [...all.filter((f) => f.severity === "error"), ...all.filter((f) => f.severity !== "error")];
  clear(parts.list);
  parts.list.append(...parts.findings.map(findingItem));
  parts.sync();
}

// wirePopover: hover and keyboard focus show the findings while they last;
// a click pins them open (or unpins: they then stay until the pointer
// leaves); Escape or a click outside unpins and hides. A pointer press also
// focuses the button, so that focus is not counted, or unpinning by click
// would leave them open until blur.
function wirePopover(parts) {
  const on = { hover: false, focus: false, pinned: false };
  let pressing = false;
  parts.sync = () => {
    parts.pop.hidden = !parts.findings.length || !(on.hover || on.focus || on.pinned);
  };
  const set = (key, value) => { on[key] = value; parts.sync(); };
  const close = () => {
    on.hover = false;
    on.focus = false;
    set("pinned", false);
  };
  parts.btn.addEventListener("pointerdown", () => { pressing = true; });
  for (const type of ["pointerup", "pointercancel"]) {
    document.addEventListener(type, () => { pressing = false; }, true);
  }
  parts.btn.addEventListener("click", () => set("pinned", !on.pinned));
  parts.wrap.addEventListener("mouseenter", () => set("hover", true));
  parts.wrap.addEventListener("mouseleave", () => set("hover", false));
  parts.btn.addEventListener("focus", () => { if (!pressing) set("focus", true); });
  parts.btn.addEventListener("blur", () => set("focus", false));
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") close(); });
  document.addEventListener("click", (e) => { if (!parts.wrap.contains(e.target)) close(); });
}

function statusButton() {
  const parts = { label: h("span"), list: h("ul", { class: "pop-list" }), findings: [] };
  parts.btn = h("button", { type: "button", class: "status", "aria-describedby": "doctor-pop" },
    h("span", { class: "dot", "aria-hidden": "true" }), parts.label);
  parts.pop = h("div", { id: "doctor-pop", class: "pop", role: "tooltip", hidden: true }, parts.list);
  parts.wrap = h("div", { class: "status-wrap", hidden: true }, parts.btn, parts.pop);
  wirePopover(parts);
  return parts;
}

export function renderHeader(root) {
  const path = h("span", { class: "hdr-path" });
  const nav = h("nav", { class: "tabs", "aria-label": "Sections" },
    TABS.map(([id, label]) => h("a", { href: `#${id}`, "data-tab": id }, label)));
  const status = statusButton();
  clear(root);
  root.classList.add("hdr");
  root.append(
    h("div", { class: "hdr-brand" }, h("span", { class: "hdr-name" }, "modelpick"), path),
    nav,
    h("div", { class: "hdr-right" }, zoneSwitch(), status.wrap));
  const update = () => {
    path.textContent = store.state ? store.state.path : "";
    fillStatus(status, store.doctor);
  };
  store.subscribe(update);
  update();
  window.addEventListener("hashchange", () => markTab(nav));
  markTab(nav);
}
