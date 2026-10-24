// Cases for js/api.js against a stubbed fetch: reload ordering and the op
// queue. In the browser, api.test.html writes the verdict into
// <output id="result">; elsewhere run() resolves it.
import { store, reload, sendOp, doctorAll } from "../js/api.js";

// ---------- the stub server ----------

function res(status, body) {
  return { status, ok: status >= 200 && status < 300, json: async () => body };
}

function gate() {
  let open;
  const promise = new Promise((resolve) => { open = resolve; });
  return { promise, open };
}

let log = [];
let server = null;

function freshServer() {
  return {
    sha: "s1",
    stateDelays: [], // per /api/state call: a promise to wait for, or null
    stateFail: [], // per /api/state call: true makes the fetch reject
    op: () => res(200, { sha256: "s1", findings: [] }),
    stateCalls: 0,
    pingDown: false,
    doctorAll: () => Promise.resolve(res(200, { errors: 1, warnings: 0, findings: [] })),
  };
}

async function stateReply() {
  const n = server.stateCalls++;
  const sha = server.sha; // what the file was when the read started
  if (server.stateDelays[n]) await server.stateDelays[n];
  if (server.stateFail[n]) throw new TypeError("Failed to fetch");
  return res(200, { sha256: sha, read_only: "", accounts: [], profiles: [], schedules: [] });
}

function stubFetch(path, init = {}) {
  const body = init.body ? JSON.parse(init.body) : undefined;
  log.push({ path, method: init.method || "GET", body });
  if (path === "/api/state") return stateReply();
  if (path === "/api/usage") return Promise.resolve(res(200, []));
  if (path === "/api/doctor") return Promise.resolve(res(200, { errors: 0, warnings: 0, findings: [] }));
  if (path === "/api/doctor/all") return server.doctorAll();
  if (path === "/api/ping") {
    return server.pingDown ? Promise.reject(new TypeError("Failed to fetch")) : Promise.resolve(res(204));
  }
  if (path === "/api/op") return Promise.resolve(server.op(body));
  return Promise.resolve(res(404, {}));
}

const ops = () => log.filter((e) => e.path === "/api/op");
const tick = () => new Promise((r) => setTimeout(r, 0));

async function until(cond, what) {
  for (let i = 0; i < 200; i++) {
    if (cond()) return;
    await new Promise((r) => setTimeout(r, 5));
  }
  throw new Error("timed out waiting for " + what);
}

// answer stands in for the user: it waits for the conflict strip and
// presses "load" (its first button) or "apply" (its second).
async function answer(choice) {
  await until(() => document.getElementById("notice-conflict"), "the conflict dialog");
  const buttons = document.querySelectorAll("#notice-conflict button");
  buttons[choice === "load" ? 0 : 1].click();
}

function toastText() {
  const box = document.getElementById("toasts");
  return box ? box.textContent : "";
}

// ---------- cases: each resolves null on pass or a message ----------

async function olderReloadDropped() {
  const slow = gate();
  server.stateDelays = [slow.promise];
  const first = reload();
  server.sha = "s2";
  const second = reload();
  await second;
  slow.open();
  await first;
  return store.state.sha256 === "s2" ? null : `store sha ${store.state.sha256}, want s2`;
}

async function olderFailureNoBanner() {
  const slow = gate();
  server.stateDelays = [slow.promise];
  server.stateFail = [true];
  const first = reload();
  const second = reload();
  const ok = await second;
  server.pingDown = true; // the old reload's failure would now read as stopped
  slow.open();
  await first;
  if (!ok) return "newer reload reported failure";
  return document.getElementById("notice-stopped") ? "stopped banner from a superseded reload" : null;
}

async function postOpReloadWins() {
  const slow = gate();
  server.stateDelays = [slow.promise]; // a focus reload that reads s1
  const focus = reload();
  await tick();
  server.op = () => {
    server.sha = "s2";
    return res(200, { sha256: "s2", findings: [] });
  };
  const r = await sendOp("x.set", {});
  slow.open();
  await focus;
  if (!r.ok) return "op not ok";
  return store.state.sha256 === "s2" ? null : `store sha ${store.state.sha256} after the slow reload, want s2`;
}

async function opsRunSerially() {
  const hold = gate();
  let n = 0;
  server.op = (body) => {
    n++;
    if (body.op === "a") return hold.promise.then(() => res(200, { sha256: "s1", findings: [] }));
    return res(200, { sha256: "s1", findings: [] });
  };
  const a = sendOp("a", {});
  const b = sendOp("b", {});
  await until(() => n >= 1, "op a");
  await tick();
  await tick();
  if (n !== 1) return `${n} ops in flight while the first is unanswered`;
  hold.open();
  await Promise.all([a, b]);
  const order = ops().map((e) => e.body.op).join(",");
  return order === "a,b" ? null : `order ${order}`;
}

async function shaPatchedBeforeNextOp() {
  server.stateFail = [true]; // the reload after op a fails
  server.op = (body) => res(200, { sha256: body.op === "a" ? "p2" : "p3", findings: [] });
  await Promise.all([sendOp("a", {}), sendOp("b", {})]);
  const bases = ops().map((e) => e.body.base).join(",");
  return bases === "s1,p2" ? null : `bases ${bases}, want s1,p2`;
}

async function reloadOverridesPatch() {
  // The op answers p2 but the file reads s1 again (reverted by hand): the
  // reload after it must win over the patched base.
  server.op = () => res(200, { sha256: "p2", findings: [] });
  await sendOp("a", {});
  return store.state.sha256 === "s1" ? null : `store sha ${store.state.sha256}, want s1`;
}

async function conflictLoad() {
  server.op = () => res(409, { sha256: "c2" });
  const pending = sendOp("a", {});
  await answer("load");
  const r = await pending;
  if (r.ok || r.reason !== "config.json changed outside the UI") return `result ${JSON.stringify(r)}`;
  if (ops().length !== 1) return `${ops().length} ops sent, want 1`;
  return server.stateCalls > 0 ? null : "no reload after load";
}

async function conflictApply() {
  let n = 0;
  server.op = () => {
    n++;
    if (n === 1) {
      server.sha = "c2";
      return res(409, { sha256: "c2" });
    }
    return res(200, { sha256: "c3", findings: [] });
  };
  const pending = sendOp("a", {});
  await answer("apply");
  const r = await pending;
  const bases = ops().map((e) => e.body.base).join(",");
  if (!r.ok) return `result ${JSON.stringify(r)}`;
  return bases === "s1,c2" ? null : `bases ${bases}, want s1,c2`;
}

async function refusalQuiet() {
  server.op = () => res(422, { reason: "schedule p is used by 4 choice(s)", uses: 4 });
  const before = toastText();
  const r = await sendOp("schedule.remove", { name: "p" }, { quiet: true });
  if (r.ok || !r.refused || r.uses !== 4 || r.reason !== "schedule p is used by 4 choice(s)") {
    return `quiet result ${JSON.stringify(r)}`;
  }
  if (toastText() !== before) return "quiet refusal toasted";
  const loud = await sendOp("schedule.remove", { name: "p" });
  if (loud.ok || !loud.refused) return `loud result ${JSON.stringify(loud)}`;
  const t = toastText();
  return t.includes("Not applied.") && t.includes("schedule p is used by 4 choice(s)") ? null : `toast ${t}`;
}

async function thrownOpKeepsQueue() {
  // A 200 whose body is null makes the op throw reading its sha256.
  server.op = (body) => (body.op === "bad" ? res(200, null) : res(200, { sha256: "s1", findings: [] }));
  const bad = sendOp("bad", {});
  const good = sendOp("good", {});
  const [rb, rg] = await Promise.all([bad, good]);
  if (rb.ok !== false) return `thrown op result ${JSON.stringify(rb)}`;
  return rg.ok ? null : `next op result ${JSON.stringify(rg)}`;
}

async function doctorAllOK() {
  const body = { errors: 1, warnings: 0, findings: [{ severity: "error", subject: "a", message: "m" }] };
  server.doctorAll = () => Promise.resolve(res(200, body));
  server.stateFail = [true]; // the reload fails, so only the 200's body can be in store.doctor
  const r = await doctorAll();
  if (!r.ok) return `result ${JSON.stringify(r)}`;
  if (JSON.stringify(store.doctor) !== JSON.stringify(body)) return `store.doctor ${JSON.stringify(store.doctor)}`;
  const posts = log.filter((e) => e.path === "/api/doctor/all" && e.method === "POST").length;
  const usage = log.filter((e) => e.path === "/api/usage").length;
  return posts === 1 && usage >= 1 ? null : `posts ${posts}, usage fetches ${usage}`;
}

async function doctorAllRefused() {
  server.doctorAll = () => Promise.resolve(res(500, { reason: "x" }));
  const r = await doctorAll();
  if (r.ok !== false) return `result ${JSON.stringify(r)}`;
  const t = toastText();
  return t.includes("Not checked.") && t.includes("x") ? null : `toast ${t}`;
}

async function doctorAllDown() {
  server.doctorAll = () => Promise.reject(new TypeError("Failed to fetch"));
  const r = await doctorAll();
  if (r.ok !== false) return `result ${JSON.stringify(r)}`;
  return document.getElementById("notice-stopped") ? null : "no stopped banner";
}

export const cases = [
  ["an older reload's result is dropped", olderReloadDropped],
  ["a superseded reload's failure shows no banner", olderFailureNoBanner],
  ["the post-op reload beats a slower earlier reload", postOpReloadWins],
  ["ops run serially", opsRunSerially],
  ["a 200 patches the sha before the next op", shaPatchedBeforeNextOp],
  ["the reload after a 200 overrides the patched sha", reloadOverridesPatch],
  ["409 then load reloads and does not resend", conflictLoad],
  ["409 then apply resends on the new base", conflictApply],
  ["422 under quiet returns uses without a toast", refusalQuiet],
  ["a thrown op does not stall the queue", thrownOpKeepsQueue],
  ["doctorAll on 200 sets the doctor and reloads usage", doctorAllOK],
  ["doctorAll on 500 toasts the reason", doctorAllRefused],
  ["doctorAll with the server gone shows the stopped banner", doctorAllDown],
];

async function check([name, fn]) {
  log = [];
  server = freshServer();
  const banner = document.getElementById("notice-stopped");
  if (banner) banner.remove();
  await reload(); // a known store: sha s1
  log = [];
  server.stateCalls = 0;
  server.stateDelays = [];
  server.stateFail = [];
  let msg;
  try {
    msg = await Promise.race([fn(), new Promise((r) => setTimeout(() => r("timed out"), 3000))]);
  } catch (e) {
    msg = "threw " + e.message;
  }
  return msg ? `${name}: ${msg}` : null;
}

// run resolves "PASS n" or "FAIL <case>; <case>…". Cases run one at a time:
// they share the module's store and queue.
export async function run() {
  const real = window.fetch;
  window.fetch = stubFetch;
  const failed = [];
  try {
    for (const c of cases) {
      const m = await check(c);
      if (m) failed.push(m);
    }
  } finally {
    window.fetch = real;
  }
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${cases.length}`;
}

if (typeof document !== "undefined") {
  run().then((v) => { document.getElementById("result").textContent = v; });
}
