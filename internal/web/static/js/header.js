// The shared header: name, config path, tabs, UTC/Local switch, doctor status.
import { h, clear } from "./dom.js";
import { store } from "./api.js";
import { findingTarget, goTo } from "./goto.js";
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

// findingItem is one finding with its Go to, which closes the panel and
// shows the finding's item (goto.js); a finding about the whole file has
// none.
function findingItem(f, close) {
  const err = f.severity === "error";
  const go = findingTarget(f) && h("button", { type: "button", class: "btn btn-md btn-quiet pop-go",
    "aria-label": `Go to ${f.subject}`, onClick: () => {
      close();
      goTo(f);
    } }, "Go to");
  return h("li", {},
    h("span", { class: "pop-text" },
      h("strong", { class: err ? "sev sev-error" : "sev sev-warn" }, err ? "ERROR" : "WARNING"),
      ` ${f.subject}: ${f.message}`),
    go);
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
  parts.list.append(...parts.findings.map((f) => findingItem(f, parts.close)));
  parts.none.hidden = parts.findings.length > 0;
  parts.sync();
}

// wirePopover: hovering the status, or keyboard focus anywhere in it (the
// button or a finding's Go to), shows the panel while there are
// findings. A click pins it open even with none (it then says so), or
// unpins it: it then stays only while hovered. Escape or a click outside
// unpins and hides it; Escape from inside the panel puts focus back on the
// button. A pointer press also focuses what it presses, so that focus is
// not counted, or unpinning by click would leave the panel open until blur.
function wirePopover(parts) {
  const on = { hover: false, focus: false, pinned: false };
  let pressing = false;
  parts.sync = () => {
    const preview = (on.hover || on.focus) && parts.findings.length > 0;
    parts.pop.hidden = !(on.pinned || preview);
    parts.btn.setAttribute("aria-expanded", String(!parts.pop.hidden));
  };
  const set = (key, value) => { on[key] = value; parts.sync(); };
  const close = () => {
    on.hover = false;
    on.focus = false;
    set("pinned", false);
  };
  parts.close = close;
  parts.wrap.addEventListener("pointerdown", () => { pressing = true; });
  for (const type of ["pointerup", "pointercancel"]) {
    document.addEventListener(type, () => { pressing = false; }, true);
  }
  parts.btn.addEventListener("click", () => set("pinned", !on.pinned));
  parts.wrap.addEventListener("mouseenter", () => set("hover", true));
  parts.wrap.addEventListener("mouseleave", () => set("hover", false));
  parts.wrap.addEventListener("focusin", () => { if (!pressing) set("focus", true); });
  parts.wrap.addEventListener("focusout", (e) => { if (!parts.wrap.contains(e.relatedTarget)) set("focus", false); });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (parts.pop.contains(document.activeElement)) parts.btn.focus();
    close();
  });
  document.addEventListener("click", (e) => { if (!parts.wrap.contains(e.target)) close(); });
}

function statusButton() {
  const parts = { label: h("span"), list: h("ul", { class: "pop-list" }), findings: [] };
  parts.none = h("p", { class: "pop-none" }, "No problems found");
  parts.btn = h("button", { type: "button", class: "status", "aria-controls": "doctor-pop" },
    h("span", { class: "dot", "aria-hidden": "true" }), parts.label);
  parts.pop = h("div", { id: "doctor-pop", class: "pop", role: "region", "aria-label": "Doctor findings", hidden: true },
    parts.none, parts.list);
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
