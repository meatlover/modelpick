// Cases for the pure functions of js/caps.js. In the browser,
// caps.test.html writes the verdict into <output id="result">; elsewhere
// run() returns it.
import { parseCap, capArg, writeCap, trashAction, positions, switchKind, barGeometry, scaleText }
  from "../js/caps.js";

const json = (x) => JSON.stringify(x);

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
  // Owner ruling 2026-10-23: the cap array runs from the shortest window
  // up; a default position before an explicit one never happens, so
  // writeCap never writes a default number.
  ["writeCap sets an explicit position", () => json(writeCap([90, "pace+10"], 1, 80)), "[90,80]"],
  ["writeCap sets the first position", () => json(writeCap([90, 80], 0, "pace+5")), `["pace+5",80]`],
  ["writeCap appends on the first default", () => json(writeCap([90], 1, 80)), "[90,80]"],
  ["writeCap appends to an empty cap", () => json(writeCap([], 0, 90)), "[90]"],
  ["writeCap past the first default → null", () => writeCap([], 2, 90), null],
  ["writeCap two past the end → null", () => writeCap([90], 2, 70), null],
  ["writeCap negative position → null", () => writeCap([90], -1, 70), null],
  ["writeCap null on the last explicit → shorter", () => json(writeCap([90, "pace+10"], 1, null)), "[90]"],
  ["writeCap null on the only explicit → []", () => json(writeCap([90], 0, null)), "[]"],
  ["writeCap null on a middle position → null", () => writeCap([90, 80, 70], 1, null), null],
  ["writeCap null on the first default → null", () => writeCap([90], 1, null), null],
  ["writeCap null on an empty cap → null", () => writeCap([], 0, null), null],
  ["writeCap leaves the input alone", () => {
    const cap = [90];
    writeCap(cap, 1, 70);
    writeCap(cap, 0, null);
    return json(cap);
  }, "[90]"],
  ["trashAction last explicit → default", () => trashAction([90, 80], 1), "default"],
  ["trashAction only explicit → default", () => trashAction([90], 0), "default"],
  ["trashAction middle explicit → disabled", () => trashAction([90, 80, 70], 1), null],
  ["trashAction first of two explicit → disabled", () => trashAction([90, 80], 0), null],
  ["trashAction empty cap, first box → uncap (cap: null)", () => trashAction([], 0), "uncap"],
  ["trashAction empty cap, later box → disabled", () => trashAction([], 2), null],
  ["trashAction default after an explicit → disabled", () => trashAction([90], 1), null],
  ["positions labelled", () => json(positions([90, "pace+10"], ["5h", "7d"], 0)),
    `[{"label":"5h","explicit":true,"editable":true},{"label":"7d","explicit":true,"editable":true}]`],
  ["positions labelled, all default: only the first editable",
    () => json(positions([], ["5h", "7d", "7d Fable"], 0)),
    `[{"label":"5h","explicit":false,"editable":true},{"label":"7d","explicit":false,"editable":false},` +
    `{"label":"7d Fable","explicit":false,"editable":false}]`],
  ["positions labelled, one explicit: the next editable",
    () => json(positions([90], ["5h", "7d", "7d Fable"], 0)),
    `[{"label":"5h","explicit":true,"editable":true},{"label":"7d","explicit":false,"editable":true},` +
    `{"label":"7d Fable","explicit":false,"editable":false}]`],
  ["positions labelled, longer cap numbered", () => json(positions([90, 80, 70], ["5h"], 0)),
    `[{"label":"5h","explicit":true,"editable":true},{"label":"2","explicit":true,"editable":true},` +
    `{"label":"3","explicit":true,"editable":true}]`],
  ["positions no reading → one numbered", () => json(positions([], undefined, 0)),
    `[{"label":"1","explicit":false,"editable":true}]`],
  ["positions empty labels → numbered", () => json(positions([90], [], 0)),
    `[{"label":"1","explicit":true,"editable":true}]`],
  ["positions numbered plus added → one default box", () => json(positions([90], null, 2)),
    `[{"label":"1","explicit":true,"editable":true},{"label":"2","explicit":false,"editable":true}]`],
  ["positions numbered, stale added count → still one default box", () => json(positions([90], null, 3)),
    `[{"label":"1","explicit":true,"editable":true},{"label":"2","explicit":false,"editable":true}]`],
  ["positions numbered, empty cap with added → one box", () => json(positions([], null, 3)),
    `[{"label":"1","explicit":false,"editable":true}]`],
  // Owner ruling 2026-10-23 (round 2): the pace bar's fill stands for time
  // elapsed at a fixed 50% (was 25%), so pace N runs 1–50 (was 1–75).
  ["switchKind 90 → pace clamps to 50", () => json(switchKind({ kind: "fixed", n: 90 }, "pace")),
    `{"kind":"pace","n":50}`],
  ["switchKind 50 → pace+50", () => json(switchKind({ kind: "fixed", n: 50 }, "pace")),
    `{"kind":"pace","n":50}`],
  ["switchKind 51 → pace clamps to 50", () => json(switchKind({ kind: "fixed", n: 51 }, "pace")),
    `{"kind":"pace","n":50}`],
  ["switchKind pace+60 stays pace+60", () => json(switchKind({ kind: "pace", n: 60 }, "pace")),
    `{"kind":"pace","n":60}`],
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
  // The bar is 240 px; the range's thumb is 14 px, so the range sits 7 px
  // out on each side and its thumb centre meets x at every value.
  ["barGeometry fixed 90", () => json(barGeometry({ kind: "fixed", n: 90 })),
    `{"x":216,"left":-7,"width":254,"range":{"min":0,"max":100,"value":90}}`],
  ["barGeometry fixed 0", () => json(barGeometry({ kind: "fixed", n: 0 })),
    `{"x":0,"left":-7,"width":254,"range":{"min":0,"max":100,"value":0}}`],
  ["barGeometry fixed 120 clamps to the end", () => json(barGeometry({ kind: "fixed", n: 120 })),
    `{"x":240,"left":-7,"width":254,"range":{"min":0,"max":100,"value":100}}`],
  // Pace: the range starts at elapsed + 1 (51% = 122.4 px) and ends at 100%.
  ["barGeometry pace+10 at 60%", () => json(barGeometry({ kind: "pace", n: 10 })),
    `{"x":144,"left":115.4,"width":131.6,"range":{"min":1,"max":50,"value":10}}`],
  ["barGeometry pace+1 at 51%", () => barGeometry({ kind: "pace", n: 1 }).x, 122.4],
  ["barGeometry pace+50 at the end", () => barGeometry({ kind: "pace", n: 50 }).x, 240],
  ["barGeometry hand-written pace+60 drawn at the end, range at 50",
    () => json([barGeometry({ kind: "pace", n: 60 }).x, barGeometry({ kind: "pace", n: 60 }).range.value]),
    "[240,50]"],
  ["barGeometry pace thumb centre meets x at every N", () => {
    const off = [];
    for (let n = 1; n <= 50; n++) {
      const g = barGeometry({ kind: "pace", n });
      const c = g.left + 7 + ((n - 1) / 49) * (g.width - 14);
      if (Math.abs(c - g.x) > 1e-9) off.push(n);
    }
    return json(off);
  }, "[]"],
  ["scaleText fixed", () => scaleText("fixed"), "Token usage: 33%"],
  ["scaleText pace", () => scaleText("pace"), "Time elapsed: 50%"],
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
