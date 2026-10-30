// Starting and driving the servers the browser tests run against:
// "live" is cmd/modelpick-e2e-server (the live back end over fakes, in a
// fresh temp dir), "demo" is cmd/modelpick-ui-demo. scripts/e2e.sh builds
// both and sets E2E_SERVER and E2E_DEMO.
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline");
const { expect } = require("@playwright/test");

const START_MS = 10000;
const ANSWER_MS = 5000;
const STOP_MS = 3000;

function binary(name) {
  const p = process.env[name];
  if (!p) {
    throw new Error(`${name} is not set: run the browser tests with bash scripts/e2e.sh`);
  }
  return p;
}

// spawnServer starts kind's binary with HOME and, for live, -dir in dir.
function spawnServer(kind, dir) {
  const env = { ...process.env, HOME: dir };
  if (kind === "live") {
    return spawn(binary("E2E_SERVER"), ["-dir", dir], { env, stdio: ["pipe", "pipe", "pipe"] });
  }
  if (kind === "demo") {
    return spawn(binary("E2E_DEMO"), ["-no-open", "-port", "0"], { env, stdio: ["pipe", "pipe", "pipe"] });
  }
  throw new Error(`startServer: unknown kind ${JSON.stringify(kind)} (want "live" or "demo")`);
}

// parseURLLine returns {url, configPath} from the server's first line, or
// null when line is not it.
function parseURLLine(kind, line) {
  if (kind === "demo") {
    const m = /^modelpick-ui-demo: (http:\/\/127\.0\.0\.1:\d+\/\?t=\S+)$/.exec(line);
    return m ? { url: m[1], configPath: null } : null;
  }
  try {
    const o = JSON.parse(line);
    return typeof o.url === "string" ? { url: o.url, configPath: o.config } : null;
  } catch {
    return null;
  }
}

// lineRouter feeds the server's stdout lines: the URL line first, then
// answers to send. The demo also prints a usage hint, which is no answer.
function lineRouter(kind, onURL, answers) {
  let started = false;
  return (line) => {
    if (!started) {
      const u = parseURLLine(kind, line);
      if (u) {
        started = true;
        onURL(u);
      }
      return;
    }
    const waiter = answers[0];
    if (!waiter) return;
    if (line === "ok") {
      answers.shift();
      waiter.resolve();
    } else if (line.startsWith("error") || line.startsWith("demo: unknown command")) {
      answers.shift();
      waiter.reject(new Error(`${kind} server answered ${JSON.stringify(waiter.cmd)} with: ${line}`));
    }
  };
}

// stopper kills the child (SIGTERM, then SIGKILL after STOP_MS) and removes
// dir; calling it again waits for the same stop.
function stopper(child, dir) {
  let stopping = null;
  return () => {
    if (!stopping) {
      stopping = new Promise((resolve) => {
        const done = () => {
          fs.rmSync(dir, { recursive: true, force: true });
          resolve();
        };
        const gone = child.pid === undefined || child.exitCode !== null || child.signalCode !== null;
        if (gone) return done();
        const kill = setTimeout(() => child.kill("SIGKILL"), STOP_MS);
        child.once("exit", () => {
          clearTimeout(kill);
          done();
        });
        child.kill("SIGTERM");
      });
    }
    return stopping;
  };
}

// sender writes one command at a time and resolves on its "ok"; it rejects
// on an error answer, on no answer within ANSWER_MS, or when the server is
// gone.
function sender(kind, child, answers) {
  let chain = Promise.resolve();
  const one = (cmd) =>
    new Promise((resolve, reject) => {
      if (child.exitCode !== null || child.signalCode !== null) {
        return reject(new Error(`${kind} server has exited; cannot send ${JSON.stringify(cmd)}`));
      }
      const timer = setTimeout(() => {
        answers.splice(answers.indexOf(waiter), 1);
        reject(new Error(`${kind} server did not answer ${JSON.stringify(cmd)} within ${ANSWER_MS} ms`));
      }, ANSWER_MS);
      const waiter = {
        cmd,
        resolve: () => (clearTimeout(timer), resolve()),
        reject: (e) => (clearTimeout(timer), reject(e)),
      };
      answers.push(waiter);
      child.stdin.write(cmd + "\n");
    });
  return (cmd) => {
    const p = chain.then(() => one(cmd));
    chain = p.catch(() => {});
    return p;
  };
}

// startServer starts a server of kind and resolves once it printed its URL:
// {url, configPath, send(cmd), stop()}. configPath is null for the demo,
// which keeps its config in memory.
function startServer(kind) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), `modelpick-e2e-${kind}-`));
  let child;
  try {
    child = spawnServer(kind, dir);
  } catch (e) {
    fs.rmSync(dir, { recursive: true, force: true });
    throw e;
  }
  const stop = stopper(child, dir);
  const answers = [];
  let stderr = "";
  child.stderr.on("data", (b) => (stderr += b));
  child.stdin.on("error", () => {}); // a write after exit: send reports it
  return new Promise((resolve, reject) => {
    const fail = (why) => stop().then(() => reject(new Error(`${kind} server ${why}\n${stderr}`)));
    const timer = setTimeout(() => fail(`printed no URL within ${START_MS} ms`), START_MS);
    const onURL = ({ url, configPath }) => {
      clearTimeout(timer);
      resolve({ url, configPath, send: sender(kind, child, answers), stop });
    };
    readline.createInterface({ input: child.stdout }).on("line", lineRouter(kind, onURL, answers));
    child.on("error", (e) => (clearTimeout(timer), fail(`did not start: ${e.message}`)));
    child.on("exit", (code, sig) => {
      clearTimeout(timer);
      for (const w of answers.splice(0)) w.reject(new Error(`${kind} server exited before answering ${JSON.stringify(w.cmd)}`));
      fail(`exited (${sig || code})`);
    });
  });
}

// E2E_OP_DELAY_MS answers every /api/op that many ms late, for CI's second
// pass: a test that acts again before the page has settled after an op
// shows up there. scripts/check.sh runs with no delay.
const OP_DELAY_MS = Number(process.env.E2E_OP_DELAY_MS || 0);
const SETTLE_MS = 10000;

// login visits the token URL, which sets the session cookie and redirects
// to the page, and guards the page (see guard below).
async function login(page, url) {
  guard(page);
  if (OP_DELAY_MS > 0) {
    await page.route("**/api/op", async (route) => {
      const resp = await route.fetch();
      await new Promise((ok) => setTimeout(ok, OP_DELAY_MS));
      await route.fulfill({ response: resp });
    });
  }
  const resp = await page.goto(url);
  if (!resp || !resp.ok()) {
    throw new Error(`login: ${url} answered ${resp ? resp.status() : "nothing"}`);
  }
}

// ---------- waiting for the page to settle ----------
//
// The page publishes on <body> (internal/web/static/js/settle.js):
// data-settled "true" once no write (an op, a Refresh, a Diagnose) and no
// reload is in flight; data-gen, how many times it became settled;
// data-writes, how many writes it sent. A test waits on those, never on a
// guess about the network or the clock.

// readBody returns the page's signal and its document's start time (a new
// document starts its counts from 0).
function readBody(page) {
  return page.evaluate(() => ({
    settled: document.body.dataset.settled === "true",
    gen: Number(document.body.dataset.gen || 0),
    writes: Number(document.body.dataset.writes || 0),
    doc: performance.timeOrigin,
  }));
}

// untilSettled resolves, in the page, once it is settled with a generation
// above before, then two frames later (so layout and focus are done), with
// the signal as readBody gives it.
function untilSettled(page, before) {
  return page.evaluate((after) => new Promise((done) => {
    const d = document.body.dataset;
    const read = () => ({ settled: true, gen: Number(d.gen), writes: Number(d.writes || 0), doc: performance.timeOrigin });
    const check = () => {
      if (d.settled !== "true" || !(Number(d.gen) > after)) return false;
      const got = read();
      requestAnimationFrame(() => requestAnimationFrame(() => done(got)));
      return true;
    };
    if (check()) return;
    const watch = new MutationObserver(() => check() && watch.disconnect());
    watch.observe(document.body, { attributes: true, attributeFilter: ["data-settled", "data-gen"] });
  }), before);
}

// settled waits until the page is settled with a generation above before,
// the data-gen read before the action, and two frames more. Every write it
// sent so far is then acknowledged: the guard lets the next action through.
async function settled(page, before) {
  let timer;
  const late = new Promise((_, fail) => {
    timer = setTimeout(async () => {
      const now = await readBody(page).catch((e) => ({ error: e.message }));
      fail(new Error(`the page did not settle within ${SETTLE_MS} ms after the action (data-gen was ${before}, ` +
        `now ${JSON.stringify(now)}): did the action send an op, a Refresh or a Diagnose?`));
    }, SETTLE_MS);
  });
  try {
    const got = await Promise.race([untilSettled(page, before), late]);
    const g = guards.get(page);
    if (g) Object.assign(g, { doc: got.doc, seen: got.writes });
  } finally {
    clearTimeout(timer);
  }
}

// act runs action, a step that sends an op (or a Refresh or a Diagnose), and
// resolves once the page has settled after it: the op answered, its reload
// drawn. Every such step that the test follows with another action goes
// through act; the guard fails a test that does not.
async function act(page, action) {
  const { gen } = await readBody(page);
  await action();
  await settled(page, gen);
}

// answer is act for the one action that must be taken while a write is
// still in flight, because that write waits for it: the conflict strip's
// buttons. The guard lets that action through.
async function answer(page, action) {
  const g = guards.get(page);
  const { gen } = await readBody(page);
  if (g) g.off += 1;
  try {
    await action();
  } finally {
    if (g) g.off -= 1;
  }
  await settled(page, gen);
}

// ---------- the guard ----------
//
// Every user action (a locator's click, fill, press, selectOption, dragTo,
// focus …; the page's mouse and keyboard) first asks the page how many
// writes it sent. More than the test has waited for through act or settled
// fails the action: it would act on a view the reload is about to replace.
// The count is the page's own, made when the action sends the write, so the
// guard fails the same way whatever the timing. stateGate turns it off for
// its page: a test that holds the reloads back acts before them on purpose.

const guards = new WeakMap(); // page → { doc, seen, off }
const LOCATOR_ACTIONS = ["blur", "check", "clear", "click", "dblclick", "dispatchEvent", "dragTo", "fill", "focus",
  "hover", "press", "pressSequentially", "selectOption", "selectText", "setChecked", "setInputFiles", "tap", "type",
  "uncheck"];
const PAGE_ACTIONS = ["check", "click", "dblclick", "dispatchEvent", "dragAndDrop", "fill", "focus", "hover", "press",
  "selectOption", "setChecked", "setInputFiles", "tap", "type", "uncheck"];
const DEVICE_ACTIONS = {
  mouse: ["click", "dblclick", "down", "move", "up", "wheel"],
  keyboard: ["down", "insertText", "press", "type", "up"],
};

// unsettledBy returns why acting on page now is wrong, or "" when it is not.
async function unsettledBy(page) {
  const g = guards.get(page);
  if (!g || g.off > 0) return "";
  const now = await readBody(page);
  if (now.doc !== g.doc) Object.assign(g, { doc: now.doc, seen: 0 }); // a reload of the page starts at 0
  return now.writes > g.seen ? `${now.writes - g.seen} op(s) the test did not wait for` : "";
}

// wrap makes obj[name] check first. The error carries the stack of the
// call (taken before any await), so it points at the test's line.
function wrap(obj, name, page, what) {
  const orig = obj[name];
  obj[name] = async function guarded(...args) {
    const at = {};
    Error.captureStackTrace(at, guarded);
    const why = await unsettledBy(page || this.page());
    if (why) {
      const step = what ? `${what}(${args.length ? JSON.stringify(args[0]) : ""})` : `${name} on ${this}`;
      const e = new Error(`acted on an unsettled page: ${step} came after ${why}. ` +
        "Send the op through act(page, () => …) so that the next action waits for the page to settle.");
      e.stack = e.stack.split("\n")[0] + "\n" + at.stack.split("\n").slice(1).join("\n");
      throw e;
    }
    return orig.apply(this, args);
  };
}

let locatorWrapped = false;

// guard installs the check on page (once) and on every locator (once per
// run: they share one prototype).
function guard(page) {
  if (guards.has(page)) return;
  guards.set(page, { doc: null, seen: 0, off: 0 });
  for (const name of PAGE_ACTIONS) wrap(page, name, page, `page.${name}`);
  for (const [dev, names] of Object.entries(DEVICE_ACTIONS)) {
    for (const name of names) wrap(page[dev], name, page, `${dev}.${name}`);
  }
  if (!locatorWrapped) {
    const proto = Object.getPrototypeOf(page.locator("body"));
    for (const name of LOCATOR_ACTIONS) wrap(proto, name, null, null);
    locatorWrapped = true;
  }
}

// unguarded turns the guard off for page from now on.
function unguarded(page) {
  const g = guards.get(page);
  if (g) g.off += 1;
}

// stateGate holds back the page's /api/state answers, so the page is not
// redrawn after an op and the op queue waits on that reload: pass(n) lets
// the next n through, open() all from then on, held() counts those waiting.
// It turns the guard off: every action after it is before a reload.
async function stateGate(page) {
  unguarded(page);
  const waiting = [];
  let credit = 0;
  let opened = false;
  await page.route("**/api/state", async (route) => {
    if (!opened && credit === 0) await new Promise((go) => waiting.push(go));
    else if (!opened) credit -= 1;
    await route.continue();
  });
  return {
    held: () => waiting.length,
    pass(n = 1) {
      for (let i = 0; i < n; i++) {
        const go = waiting.shift();
        if (go) go();
        else credit += 1;
      }
    },
    open() {
      opened = true;
      waiting.splice(0).forEach((go) => go());
    },
  };
}

// holdState holds back every /api/state answer until the returned release()
// is called.
async function holdState(page) {
  const gate = await stateGate(page);
  return () => gate.open();
}

module.exports = { startServer, login, stateGate, holdState, act, answer, settled };
