// The page's only path to the server, and the store every tab reads.
import { toast, conflictDialog, stopped } from "./notice.js";

const listeners = [];
let seen = { state: "", usage: "", doctor: "" };

// store holds the last state, usage and doctor. subscribe(fn) calls
// fn(changed) after a load, changed being the keys whose JSON differs.
export const store = {
  state: null,
  usage: [],
  doctor: null,
  subscribe(fn) {
    listeners.push(fn);
  },
  set(next) {
    const changed = Object.keys(next).filter((k) => JSON.stringify(next[k]) !== seen[k]);
    for (const k of changed) {
      store[k] = next[k];
      seen[k] = JSON.stringify(next[k]);
    }
    if (changed.length) for (const fn of listeners) fn(changed);
  },
};

async function getJSON(path) {
  const r = await fetch(path, { headers: { Accept: "application/json" } });
  if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`);
  return r.json();
}

async function postJSON(path, body) {
  const r = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let data = {};
  try {
    data = await r.json();
  } catch {
    // an empty or non-JSON body: the status alone says what happened
  }
  return { status: r.status, data };
}

// ping resolves true when the server answers.
export async function ping() {
  try {
    return (await fetch("/api/ping")).status === 204;
  } catch {
    return false;
  }
}

export async function load() {
  const [state, usage, doctor] = await Promise.all(
    [getJSON("/api/state"), getJSON("/api/usage"), getJSON("/api/doctor")]);
  store.set({ state, usage, doctor });
  return { state, usage, doctor };
}

// reload is load that never rejects: on failure it shows the stopped banner
// when the server is gone. It resolves true when the store was reloaded.
export async function reload() {
  try {
    await load();
    stopped(false);
    return true;
  } catch {
    stopped(!(await ping()));
    return false;
  }
}

function notApplied(reason) {
  toast(`${String(reason).replace(new RegExp("[.\\s]+$"), "")}. config.json is unchanged.`, "Not applied.");
}

async function attempt(op, args, retry, opts) {
  const base = store.state ? store.state.sha256 : "";
  let r;
  try {
    r = await postJSON("/api/op", { base, op, args });
  } catch {
    stopped(true);
    return { ok: false, reason: "modelpick ui has stopped" };
  }
  if (r.status === 200) {
    // The new base is known now, even if the reload below fails.
    if (store.state && r.data.sha256) store.state.sha256 = r.data.sha256;
    await reload();
    return { ok: true, findings: r.data.findings || [] };
  }
  if (r.status === 409) return conflict(op, args, retry, opts);
  const reason = r.data.reason || `HTTP ${r.status}`;
  if (!opts.quiet) notApplied(reason);
  return { ok: false, reason, uses: r.data.uses };
}

async function conflict(op, args, retry, opts) {
  const reason = "config.json changed outside the UI";
  if (!retry) {
    await reload();
    notApplied(reason);
    return { ok: false, reason };
  }
  const choice = await conflictDialog();
  await reload();
  if (choice === "load") return { ok: false, reason };
  return attempt(op, args, false, opts);
}

// Every op starts after the previous one, reload included, so consecutive
// edits never carry a stale base.
let queue = Promise.resolve();

// sendOp applies one operation. {ok: true} after a 200 (the reload is
// attempted; its failure does not reject); on a refusal {ok: false, reason,
// uses?} after the toast (opts.quiet skips the toast, for a caller that asks
// the user instead, e.g. schedule.remove's uses). A 409 shows the conflict
// dialog: "load" reloads, "apply" resends once on the new base.
export function sendOp(op, args, opts = {}) {
  const run = queue.then(() => attempt(op, args, true, opts))
    .catch((e) => ({ ok: false, reason: String(e && e.message ? e.message : e) }));
  queue = run;
  return run;
}

// refresh calls the vendor for one account and replaces its usage row.
export async function refresh(account) {
  let r;
  try {
    r = await postJSON(`/api/usage/${encodeURIComponent(account)}/refresh`);
  } catch {
    stopped(true);
    return { ok: false, reason: "modelpick ui has stopped" };
  }
  if (r.status !== 200) {
    const reason = r.data.reason || `HTTP ${r.status}`;
    toast(reason, "Not refreshed."); // the reason names the account
    return { ok: false, reason };
  }
  store.set({ usage: store.usage.map((row) => (row.account === account ? r.data : row)) });
  return { ok: true, row: r.data };
}
