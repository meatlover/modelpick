// Entry point: header, router, reload loop, stopped and read-only notices.
import { clear } from "./dom.js";
import { reload, ping, store } from "./api.js";
import { renderHeader, currentTab } from "./header.js";
import { stopped, readOnly, newerBinary } from "./notice.js";
import { onZone, tick } from "./time.js";
import * as usage from "./usage.js";
import * as profiles from "./profiles.js";
import * as schedules from "./schedules.js";
import * as accounts from "./accounts.js";

// A tab module exports render(root) and may export uses, the store keys it
// shows; it then re-renders only when one of those changed.
const MODULES = { usage, profiles, schedules, accounts };
const RELOAD_MS = 60000;
const TICK_MS = 30000;
const PING_MS = 10000;

function mainEl() {
  return document.getElementById("main");
}

function renderTab() {
  const id = currentTab();
  const hash = location.hash;
  if (hash !== `#${id}` && !hash.startsWith(`#${id}/`)) history.replaceState(null, "", `#${id}`);
  clear(mainEl());
  MODULES[id].render(mainEl());
  tick(mainEl());
}

// While the file is read-only every control in main is inert, including
// those a tab adds later; app.css dims them. A container holding a .ro-ok
// control (Usage refresh) is left alone.
const LOCKABLE = 'button, input, select, textarea, [role="button"], [draggable="true"], [tabindex]';
const locker = new MutationObserver(() => lock(true));

function lock(on) {
  if (!on) {
    for (const el of mainEl().querySelectorAll("[data-ro-lock]")) {
      el.inert = false;
      delete el.dataset.roLock;
    }
    return;
  }
  for (const el of mainEl().querySelectorAll(LOCKABLE)) {
    if (el.inert || el.classList.contains("ro-ok") || el.querySelector(".ro-ok")) continue;
    el.inert = true;
    el.dataset.roLock = "1";
  }
}

function applyReadOnly() {
  const text = store.state ? store.state.read_only : "";
  readOnly(text);
  newerBinary(store.state && store.state.binary_replaced);
  if (text) document.body.dataset.readonly = "1";
  else delete document.body.dataset.readonly;
  locker.disconnect();
  lock(Boolean(text));
  if (text) locker.observe(mainEl(), { childList: true, subtree: true });
}

function onStore(changed) {
  applyReadOnly();
  const uses = MODULES[currentTab()].uses;
  if (!uses || uses.some((k) => changed.includes(k))) renderTab();
}

async function checkPing() {
  const ok = await ping();
  if (stopped(!ok) && ok) reload();
}

function start() {
  renderHeader(document.getElementById("header"));
  store.subscribe(onStore);
  window.addEventListener("hashchange", renderTab);
  window.addEventListener("focus", reload);
  onZone(renderTab);
  setInterval(reload, RELOAD_MS);
  setInterval(() => tick(mainEl()), TICK_MS);
  setInterval(checkPing, PING_MS);
  renderTab();
  reload();
}

start();
