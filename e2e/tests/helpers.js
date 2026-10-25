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

// login visits the token URL, which sets the session cookie and redirects
// to the page.
// E2E_OP_DELAY_MS answers every /api/op that many ms late, for CI's second
// pass: a test that acts again before the page has redrawn after an op
// shows up there. scripts/check.sh runs with no delay.
const OP_DELAY_MS = Number(process.env.E2E_OP_DELAY_MS || 0);

async function login(page, url) {
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

// redrawn runs action, an edit that sends one op, and resolves once the
// page has been redrawn from it: the op's answer, then the state, usage and
// doctor reads of the reload after it, then two frames. A test that acts
// on what the edit redraws waits here, as an answer delayed by
// E2E_OP_DELAY_MS can redraw the page under its next action.
async function redrawn(page, action) {
  const done = [];
  const onDone = (req) => done.push(new URL(req.url()).pathname);
  page.on("requestfinished", onDone);
  try {
    await action();
    await expect.poll(() => {
      const op = done.indexOf("/api/op");
      return op >= 0 && ["/api/state", "/api/usage", "/api/doctor"].every((p) => done.indexOf(p, op) > op);
    }, { message: "the op answered and the page reloaded", timeout: 10000 }).toBe(true);
  } finally {
    page.off("requestfinished", onDone);
  }
  await page.evaluate(() => new Promise((go) => requestAnimationFrame(() => requestAnimationFrame(go))));
}

// stateGate holds back the page's /api/state answers, so the page is not
// redrawn after an op and the op queue waits on that reload: pass(n) lets
// the next n through, open() all from then on, held() counts those waiting.
async function stateGate(page) {
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

module.exports = { startServer, login, stateGate, holdState, redrawn };
