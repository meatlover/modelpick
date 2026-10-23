// The shared header: name, config path, tabs, UTC/Local switch, doctor status.
import { h, clear } from "./dom.js";
import { store } from "./api.js";
import { zone, setZone, onZone, localZoneName } from "./time.js";

export const TABS = [["usage", "Usage"], ["profiles", "Profiles"], ["schedules", "Schedules"], ["accounts", "Accounts"]];

// currentTab is the tab the hash names, "usage" for anything else.
export function currentTab() {
  const id = location.hash.slice(1);
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
    h("strong", { class: err ? "sev sev-error" : "sev sev-warn" }, err ? "ERROR" : "WARN"),
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
  if (!parts.findings.length) parts.pop.hidden = true;
}

function statusButton() {
  const parts = { label: h("span"), list: h("ul", { class: "pop-list" }), findings: [] };
  parts.btn = h("button", { type: "button", class: "status", "aria-describedby": "doctor-pop" },
    h("span", { class: "dot", "aria-hidden": "true" }), parts.label);
  parts.pop = h("div", { id: "doctor-pop", class: "pop", role: "tooltip", hidden: true }, parts.list);
  parts.wrap = h("div", { class: "status-wrap", hidden: true }, parts.btn, parts.pop);
  const show = () => { parts.pop.hidden = parts.findings.length === 0; };
  const hide = () => { parts.pop.hidden = true; };
  // A pointer press focuses the button too; its click, not that focus,
  // decides, so a tap opens and a second tap closes.
  let pressing = false;
  parts.btn.addEventListener("pointerdown", () => { pressing = true; });
  parts.btn.addEventListener("click", () => {
    pressing = false;
    if (parts.pop.hidden) show();
    else hide();
  });
  parts.wrap.addEventListener("mouseenter", show);
  parts.wrap.addEventListener("mouseleave", hide);
  parts.btn.addEventListener("focus", () => { if (!pressing) show(); });
  parts.btn.addEventListener("blur", hide);
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") hide(); });
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
