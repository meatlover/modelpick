// Starting and driving the servers the browser tests run against:
// "live" is cmd/modelpick-e2e-server (the live back end over fakes, in a
// fresh temp dir), "demo" is cmd/modelpick-ui-demo. scripts/e2e.sh builds
// both and sets E2E_SERVER and E2E_DEMO.
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline");

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
async function login(page, url) {
  const resp = await page.goto(url);
  if (!resp || !resp.ok()) {
    throw new Error(`login: ${url} answered ${resp ? resp.status() : "nothing"}`);
  }
}

module.exports = { startServer, login };
