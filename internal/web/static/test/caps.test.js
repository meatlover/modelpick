// Cases for the pure functions of js/caps.js. In the browser,
// caps.test.html writes the verdict into <output id="result">; elsewhere
// run() returns it.
import { parseCap, capArg, writeCap, trashAction, positions, switchKind } from "../js/caps.js";

const json = (x) => JSON.stringify(x);
const DEF = 95;

// sent is what a typed box puts on the wire.
const sent = (text) => json(capArg(parseCap(text)));

export const cases = [
  ["parseCap 90", () => json(parseCap("90")), `{"kind":"fixed","n":90}`],
  ["parseCap 100%", () => json(parseCap("100%")), `{"kind":"fixed","n":100}`],
  ["parseCap ' Pace+10 '", () => json(parseCap(" Pace+10 ")), `{"kind":"pace","n":10}`],
  ["parseCap pace+10%", () => json(parseCap("pace+10%")), `{"kind":"pace","n":10}`],
  ["parseCap empty", () => json(parseCap("")), `{"kind":"empty"}`],
  ["parseCap spaces only", () => json(parseCap("   ")), `{"kind":"empty"}`],
  ["parseCap abc is bad", () => json(parseCap("abc")), `{"kind":"bad","raw":"abc"}`],
  ["parseCap pace+x is bad", () => json(parseCap("pace+x")), `{"kind":"bad","raw":"pace+x"}`],
  ["pace+0 is sent raw", () => sent("pace+0"), `"pace+0"`],
  ["120 is sent as typed", () => sent("120"), "120"],
  ["100% is sent as 100", () => sent("100%"), "100"],
  ["' Pace+10 ' is sent as pace+10", () => sent(" Pace+10 "), `"pace+10"`],
  ["bad text is sent as typed", () => sent("abc"), `"abc"`],
  ["empty box is trash", () => parseCap("").kind === "empty" && trashAction([90], 0), "default"],
  ["writeCap sets an explicit position", () => json(writeCap([90, "pace+10"], 1, 80, DEF)), "[90,80]"],
  ["writeCap default before explicit is the default number",
    () => json(writeCap([], 2, 90, DEF)), "[95,95,90]"],
  ["writeCap trailing defaults trimmed", () => json(writeCap([90, "pace+10"], 1, null, DEF)), "[90]"],
  ["writeCap middle back to default", () => json(writeCap([90, 80, 70], 1, null, DEF)), "[90,95,70]"],
  ["writeCap last explicit to default", () => json(writeCap([90], 0, null, DEF)), "[]"],
  ["writeCap leaves the input alone", () => {
    const cap = [90];
    writeCap(cap, 2, 70, DEF);
    return json(cap);
  }, "[90]"],
  ["trashAction explicit → default", () => trashAction([90, 80], 1), "default"],
  ["trashAction all default → uncap (cap: null)", () => trashAction([], 0), "uncap"],
  ["trashAction all default, later box → uncap", () => trashAction([], 2), "uncap"],
  ["trashAction default after an explicit → disabled", () => trashAction([90], 1), null],
  ["positions labelled", () => json(positions([90, "pace+10"], ["5h", "7d"], 0)),
    `[{"label":"5h","explicit":true},{"label":"7d","explicit":true}]`],
  ["positions labelled, all default", () => json(positions([], ["5h", "7d", "7d Fable"], 0)),
    `[{"label":"5h","explicit":false},{"label":"7d","explicit":false},{"label":"7d Fable","explicit":false}]`],
  ["positions labelled, longer cap numbered", () => json(positions([90, 80, 70], ["5h"], 0)),
    `[{"label":"5h","explicit":true},{"label":"2","explicit":true},{"label":"3","explicit":true}]`],
  ["positions no reading → one numbered", () => json(positions([], undefined, 0)),
    `[{"label":"1","explicit":false}]`],
  ["positions empty labels → numbered", () => json(positions([90], [], 0)),
    `[{"label":"1","explicit":true}]`],
  ["positions numbered plus added", () => json(positions([90], null, 3)),
    `[{"label":"1","explicit":true},{"label":"2","explicit":false},{"label":"3","explicit":false}]`],
  ["switchKind 90 → pace clamps to 75", () => json(switchKind({ kind: "fixed", n: 90 }, "pace")),
    `{"kind":"pace","n":75}`],
  ["switchKind 10 → pace+10", () => json(switchKind({ kind: "fixed", n: 10 }, "pace")),
    `{"kind":"pace","n":10}`],
  ["switchKind 0 → pace+1", () => json(switchKind({ kind: "fixed", n: 0 }, "pace")),
    `{"kind":"pace","n":1}`],
  ["switchKind 12.6 → pace+13", () => json(switchKind({ kind: "fixed", n: 12.6 }, "pace")),
    `{"kind":"pace","n":13}`],
  ["switchKind pace+40 → 40", () => json(switchKind({ kind: "pace", n: 40 }, "fixed")),
    `{"kind":"fixed","n":40}`],
  ["switchKind same kind unchanged", () => json(switchKind({ kind: "pace", n: 90 }, "pace")),
    `{"kind":"pace","n":90}`],
];

function check([name, fn, want]) {
  let got;
  try {
    got = fn();
  } catch (e) {
    got = "threw " + e.message;
  }
  return got === want ? null : `${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`;
}

// run returns "PASS n" or "FAIL <case>; <case>…".
export function run() {
  const failed = cases.map(check).filter(Boolean);
  return failed.length ? "FAIL " + failed.join("; ") : `PASS ${cases.length}`;
}

if (typeof document !== "undefined") {
  document.getElementById("result").textContent = run();
}
