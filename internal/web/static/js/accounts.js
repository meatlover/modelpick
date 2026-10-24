// Accounts tab: the Add account form above the account list. account.add and
// account.remove are single ops; a refusal toasts (api.js) and leaves the
// typed values and the list as they were.
import { h, clear } from "./dom.js";
import { store, sendOp } from "./api.js";
import { currentTab } from "./header.js";

export const uses = ["state", "doctor"];

const VENDORS = ["claude", "codex", "opencode-go"];
const PREFIX = { claude: "claude", codex: "codex", "opencode-go": "go" };
// Per vendor: the field keys in form order, with the label shown.
const FIELDS = {
  claude: [["config_dir", "Config dir"]],
  codex: [["codex_home", "Codex home"]],
  "opencode-go": [["key_file", "Key file"], ["key_line", "Key line"]],
};
const KEY_FILE = "~/.ssh/opencode-go-api";

let host = null;
// focusKey is the data-key of the element that had focus; paint puts focus
// back on it, since every repaint rebuilds the tab.
let focusKey = null;
let listening = false;
let busy = false;
// The form: the chosen vendor, and the text the user typed per key. A key
// without a draft shows its default, which follows the accounts as they change.
let vendor = "opencode-go";
let drafts = {};

function listen() {
  if (listening) return;
  listening = true;
  document.addEventListener("focusin", (e) => {
    if (currentTab() !== "accounts") return;
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

function errorSubjects() {
  const found = store.doctor ? store.doctor.findings || [] : [];
  return new Set(found.filter((f) => f.severity === "error").map((f) => f.subject));
}

// ---------- defaults ----------

function nextName(accounts, v) {
  const used = new Set(accounts.map((a) => a.name));
  let n = 1;
  while (used.has(`${PREFIX[v]}-${n}`)) n++;
  return `${PREFIX[v]}-${n}`;
}

// nextPath is base when no account uses it, else base-<n> for the first free
// n from 2.
function nextPath(accounts, v, key, base) {
  const used = new Set(accounts.filter((a) => a.vendor === v).map((a) => (a.fields || {})[key]));
  if (!used.has(base)) return base;
  let n = 2;
  while (used.has(`${base}-${n}`)) n++;
  return `${base}-${n}`;
}

function nextLine(accounts, file) {
  const lines = accounts.filter((a) => a.vendor === "opencode-go" && (a.fields || {}).key_file === file)
    .map((a) => Number(a.fields.key_line) || 0);
  return String(lines.length ? Math.max(...lines) + 1 : 1);
}

function value(accounts, key) {
  if (drafts[key] !== undefined) return drafts[key];
  switch (key) {
    case "name": return nextName(accounts, vendor);
    case "config_dir": return nextPath(accounts, "claude", "config_dir", "~/.claude");
    case "codex_home": return nextPath(accounts, "codex", "codex_home", "~/.codex");
    case "key_file": return KEY_FILE;
    default: return nextLine(accounts, value(accounts, "key_file"));
  }
}

// ---------- add ----------

function fieldArgs(accounts) {
  const fields = {};
  for (const [key] of FIELDS[vendor]) {
    const v = value(accounts, key);
    fields[key] = key === "key_line" ? (v.trim() === "" ? v : Number(v)) : v;
  }
  return fields;
}

async function add() {
  if (busy || !store.state) return;
  const accounts = store.state.accounts;
  busy = true;
  const r = await sendOp("account.add", { name: value(accounts, "name"), vendor, fields: fieldArgs(accounts) });
  busy = false;
  if (!r.ok) return; // the toast says why; the form keeps the values
  drafts = {};
  focusKey = "name";
  paint();
}

function labelled(label, control) {
  return h("label", { class: "a-field" }, label, control);
}

function textInput(accounts, key, cls) {
  return h("input", { type: "text", class: ["input", cls], "data-key": key, value: value(accounts, key),
    autocomplete: "off", spellcheck: "false", onInput: (e) => { drafts[key] = e.target.value; } });
}

function lineInput(accounts) {
  return h("input", { type: "number", class: "input mono a-line", "data-key": "key_line", min: "1",
    value: value(accounts, "key_line"), onInput: (e) => { drafts.key_line = e.target.value; } });
}

function vendorSelect() {
  return h("select", { "data-key": "vendor", onChange: (e) => {
    vendor = e.target.value;
    focusKey = "vendor";
    paint();
  } }, VENDORS.map((v) => h("option", { value: v, selected: v === vendor }, v)));
}

function vendorFields(accounts) {
  return FIELDS[vendor].map(([key, label]) => labelled(label,
    key === "key_line" ? lineInput(accounts) : textInput(accounts, key, "mono a-path")));
}

function addForm(accounts) {
  return h("section", { class: "panel a-add", "aria-label": "Add account" },
    h("h2", {}, "Add account"),
    h("form", { class: "a-form", onSubmit: (e) => {
      e.preventDefault();
      add();
    } },
    labelled("Name", textInput(accounts, "name", "a-name")),
    labelled("Vendor", vendorSelect()),
    vendorFields(accounts),
    h("button", { type: "submit", class: "btn btn-lg btn-outline", "data-key": "add" }, "Add")));
}

// ---------- list ----------

async function remove(accounts, i) {
  const next = accounts[i + 1];
  const r = await sendOp("account.remove", { name: accounts[i].name });
  if (!r.ok) return;
  focusKey = next ? `rm-${next.name}` : "name";
  paint();
}

function row(accounts, i, red) {
  const a = accounts[i];
  return h("li", { class: ["a-row", red.has(a.name) && "is-error"] },
    h("span", { class: "a-name-cell" }, a.name),
    h("span", { class: "mono a-vendor" }, a.vendor),
    h("div", { class: "a-where" },
      h("span", { class: "mono a-loc" }, a.location),
      h("span", { class: "a-src" }, a.source)),
    h("div", { class: "a-act" },
      h("button", { type: "button", class: "btn btn-quiet", "aria-label": `Remove account ${a.name}`,
        "data-key": `rm-${a.name}`, onClick: () => remove(accounts, i) }, "Remove")));
}

function list(accounts, red) {
  return h("div", { class: "panel a-list" },
    h("div", { class: "a-head", "aria-hidden": "true" },
      h("span", {}, "Account"), h("span", {}, "Vendor"), h("span", {}, "Login location"), h("span", {})),
    h("ul", { class: "a-rows", "aria-label": "Accounts" }, accounts.map((_, i) => row(accounts, i, red))));
}

function paint() {
  if (!host || currentTab() !== "accounts") return;
  listen();
  clear(host);
  const st = store.state;
  if (!st) return;
  host.append(addForm(st.accounts), list(st.accounts, errorSubjects()));
  restoreFocus();
}

export function render(root) {
  host = root;
  paint();
}
