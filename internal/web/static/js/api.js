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

async function fetchAll() {
  const [state, usage, doctor] = await Promise.all(
    [getJSON("/api/state"), getJSON("/api/usage"), getJSON("/api/doctor")]);
  return { state, usage, doctor };
}

// Reloads overlap (focus, the 60 s timer, after each op); generation counts
// them so that only the newest one started may write the store. An older
// one, whenever it finishes, is dropped and resolves as the newest does.
let generation = 0;
let newest = Promise.resolve(true);

async function reloadAs(mine) {
  let data;
  try {
    data = await fetchAll();
  } catch {
    const up = await ping(); // a newer reload may start meanwhile
    if (mine !== generation) return newest;
    stopped(!up);
    return false;
  }
  if (mine !== generation) return newest;
  store.set(data);
  stopped(false);
  return true;
}

// reload never rejects: on failure it shows the stopped banner when the
// server is gone. It resolves true when the store was reloaded.
export function reload() {
  generation += 1;
  newest = reloadAs(generation);
  return newest;
}

// notApplied shows the refusal toast; a caller that sent with opts.quiet
// uses it for a refusal it does not handle itself.
export function notApplied(reason) {
  toast(`${String(reason).replace(new RegExp("[.\\s]+$"), "")}. config.json is unchanged.`, "Not applied.");
}

// patchSHA records a new base in the store; seen follows, so a reload that
// brings back the state as it was before the op still counts as a change.
function patchSHA(sha) {
  store.state.sha256 = sha;
  seen.state = JSON.stringify(store.state);
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
    if (store.state && r.data.sha256) patchSHA(r.data.sha256);
    await reload();
    return { ok: true, findings: r.data.findings || [] };
  }
  if (r.status === 409) return conflict(op, args, retry, opts);
  const reason = r.data.reason || `HTTP ${r.status}`;
  if (!opts.quiet) notApplied(reason);
  return { ok: false, reason, uses: r.data.uses, refused: true };
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
// attempted; its failure does not reject); otherwise {ok: false, reason}.
// Any other answer (a refusal, 422) also carries refused: true and uses?, and
// is toasted unless opts.quiet: that is for a caller that asks the user
// instead (schedule.remove's uses) and calls notApplied for the rest. A 409
// shows the conflict dialog: "load" reloads, "apply" resends once on the
// new base.
export function sendOp(op, args, opts = {}) {
  const run = queue.then(() => attempt(op, args, true, opts))
    .catch((e) => ({ ok: false, reason: String(e && e.message ? e.message : e) }));
  queue = run;
  return run;
}

// refresh calls the vendor for one account and replaces its usage row.
// Then it reloads, which re-reads the doctor: the call, failed or not
// (a refusal may still have recorded the attempt), changes the status.
// opts.reload === false skips that, for a caller that refreshes several
// accounts and reloads once after them (each doctor read re-runs every
// account's Check). A server that is gone reloads nothing.
export async function refresh(account, opts = {}) {
  let r;
  try {
    r = await postJSON(`/api/usage/${encodeURIComponent(account)}/refresh`);
  } catch {
    stopped(true);
    return { ok: false, reason: "modelpick ui has stopped" };
  }
  let out;
  if (r.status !== 200) {
    out = { ok: false, reason: r.data.reason || `HTTP ${r.status}` };
    toast(out.reason, "Not refreshed."); // the reason names the account
  } else {
    store.set({ usage: store.usage.map((row) => (row.account === account ? r.data : row)) });
    out = { ok: true, row: r.data };
  }
  if (opts.reload !== false) await reload();
  return out;
}

// doctorAll runs `doctor all` on the server. The page reloads (the calls
// changed usage rows), then the POST's own result replaces the doctor:
// it holds findings plain doctor does not make (cannot update stored usage,
// cannot read stored usage for a file the call repaired). The next focus or
// 60 s reload shows plain doctor again.
export async function doctorAll() {
  let r;
  try {
    r = await postJSON("/api/doctor/all");
  } catch {
    stopped(true);
    return { ok: false, reason: "modelpick ui has stopped" };
  }
  if (r.status !== 200) {
    const reason = r.data.reason || "Check all failed";
    toast(reason, "Not checked.");
    return { ok: false, reason };
  }
  await reload();
  store.set({ doctor: r.data });
  return { ok: true };
}
