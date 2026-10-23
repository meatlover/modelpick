// Entry point: header, router, reload loop, stopped and read-only notices.
import { clear } from "./dom.js";
import { load, ping, store } from "./api.js";
import { renderHeader, currentTab } from "./header.js";
import { stopped, readOnly } from "./notice.js";
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
  if (location.hash !== `#${id}`) history.replaceState(null, "", `#${id}`);
  clear(mainEl());
  MODULES[id].render(mainEl());
  tick(mainEl());
}

function applyReadOnly() {
  const text = store.state ? store.state.read_only : "";
  readOnly(text);
  if (text) document.body.dataset.readonly = "1";
  else delete document.body.dataset.readonly;
}

function onStore(changed) {
  applyReadOnly();
  const uses = MODULES[currentTab()].uses;
  if (!uses || uses.some((k) => changed.includes(k))) renderTab();
}

async function reload() {
  try {
    await load();
    stopped(false);
  } catch {
    stopped(!(await ping()));
  }
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
