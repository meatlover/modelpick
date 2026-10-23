// Cases for the pure functions of js/caps.js. In the browser,
// caps.test.html writes the verdict into <output id="result">; elsewhere
// run() returns it.
import { parseCap, capArg, writeCap, trashAction, positions, switchKind, barGeometry, scaleText, elementOf,
  boundNumber, previewOf, capLine, lineParts, isSet, plusShown } from "../js/caps.js";

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
  // Owner ruling 2026-10-23 (round 3): every typed number is bounded:
  // fixed 0–100, pace 0–50; out of range clamps.
  ["pace+0 is sent as pace+0", () => sent("pace+0"), `"pace+0"`],
  ["120 clamps to 100", () => sent("120"), "100"],
  ["-5 clamps to 0", () => sent("-5"), "0"],
  ["pace+70 clamps to pace+50", () => sent("pace+70"), `"pace+50"`],
  ["pace+50 is sent as pace+50", () => sent("pace+50"), `"pace+50"`],
  ["100% is sent as 100", () => sent("100%"), "100"],
  ["' Pace+10 ' is sent as pace+10", () => sent(" Pace+10 "), `"pace+10"`],
  ["bad text is sent as typed", () => sent("abc"), `"abc"`],
  // Owner ruling 2026-10-23 (round 5): a cap element is a number, "pace+N"
  // or "default" (use the Global default cap); the array runs from the
  // shortest window up; trailing "default" elements are dropped on write.
  ["isSet number", () => isSet(90), true],
  ["isSet pace", () => isSet("pace+5"), true],
  ["isSet default → false", () => isSet("default"), false],
  ["isSet past the end → false", () => isSet(undefined), false],
  ["writeCap sets an explicit position", () => json(writeCap([90, "pace+10"], 1, 80)), "[90,80]"],
  ["writeCap sets the first position", () => json(writeCap([90, 80], 0, "pace+5")), `["pace+5",80]`],
  ["writeCap appends on the first default", () => json(writeCap([90], 1, 80)), "[90,80]"],
  ["writeCap appends to an empty cap", () => json(writeCap([], 0, 90)), "[90]"],
  ["writeCap past the end fills the gap with default", () => json(writeCap([], 2, 80)),
    `["default","default",80]`],
  ["writeCap two past the end fills one default", () => json(writeCap([90], 2, 70)), `[90,"default",70]`],
  ["writeCap sets a default element", () => json(writeCap([90, "default", 95], 1, 80)), "[90,80,95]"],
  ["writeCap drops a stored trailing default", () => json(writeCap([90, "default"], 0, 80)), "[80]"],
  ["writeCap negative position → null", () => writeCap([90], -1, 70), null],
  ["writeCap null on the last explicit → shorter", () => json(writeCap([90, "pace+10"], 1, null)), "[90]"],
  ["writeCap null on the only explicit → []", () => json(writeCap([90], 0, null)), "[]"],
  ["writeCap null on a middle position → default", () => json(writeCap([90, 98, 95], 1, null)),
    `[90,"default",95]`],
  ["writeCap null on the first → default", () => json(writeCap([90, "default", 95], 0, null)),
    `["default","default",95]`],
  ["writeCap null on the last trims every trailing default",
    () => json(writeCap(["default", "default", 95], 2, null)), "[]"],
  ["writeCap null on the last trims back to the explicit one",
    () => json(writeCap([90, "default", 95], 2, null)), "[90]"],
  ["writeCap null on a default element → null", () => writeCap([90, "default", 95], 1, null), null],
  ["writeCap null on the first default → null", () => writeCap([90], 1, null), null],
  ["writeCap null on an empty cap → null", () => writeCap([], 0, null), null],
  ["writeCap leaves the input alone", () => {
    const cap = [90, 98];
    writeCap(cap, 3, 70);
    writeCap(cap, 0, null);
    return json(cap);
  }, "[90,98]"],
  ["trashAction last explicit → default", () => trashAction([90, 80], 1), "default"],
  ["trashAction only explicit → default", () => trashAction([90], 0), "default"],
  ["trashAction middle explicit → default", () => trashAction([90, 98, 95], 1), "default"],
  ["trashAction first of two explicit → default", () => trashAction([90, 80], 0), "default"],
  ["trashAction explicit after a default element → default", () => trashAction(["default", 80], 1), "default"],
  ["trashAction default element → disabled", () => trashAction([90, "default", 95], 1), null],
  ["trashAction empty cap, first box → uncap (cap: null)", () => trashAction([], 0), "uncap"],
  ["trashAction only default elements, first box → uncap", () => trashAction(["default"], 0), "uncap"],
  ["trashAction empty cap, later box → disabled", () => trashAction([], 2), null],
  ["trashAction first box default, a later one set → disabled", () => trashAction(["default", "default", 95], 0),
    null],
  ["trashAction default after an explicit → disabled", () => trashAction([90], 1), null],
  // Task 23: a value typed (or sent, not yet answered) into a default box;
  // trash puts the default back. Round 5: on any default box.
  ["trashAction typed first default → discard", () => trashAction([90], 1, true), "discard"],
  ["trashAction typed second default → discard", () => trashAction([90], 2, true), "discard"],
  ["trashAction typed default element → discard", () => trashAction([90, "default", 95], 1, true), "discard"],
  ["trashAction typed middle explicit → default", () => trashAction([90, 80, 70], 1, true), "default"],
  ["trashAction typed last explicit → default", () => trashAction([90, 80], 1, true), "default"],
  ["trashAction typed box 1, none explicit → uncap", () => trashAction([], 0, true), "uncap"],
  ["positions labelled", () => json(positions([90, "pace+10"], ["5h", "7d"], 0)),
    `[{"label":"5h","explicit":true},{"label":"7d","explicit":true}]`],
  ["positions labelled, all default", () => json(positions([], ["5h", "7d", "7d Fable"], 0)),
    `[{"label":"5h","explicit":false},{"label":"7d","explicit":false},{"label":"7d Fable","explicit":false}]`],
  ["positions labelled, a default element", () => json(positions([90, "default", 95], ["5h", "7d", "7d Fable"], 0)),
    `[{"label":"5h","explicit":true},{"label":"7d","explicit":false},{"label":"7d Fable","explicit":true}]`],
  ["positions labelled, longer cap numbered", () => json(positions([90, 80, 70], ["5h"], 0)),
    `[{"label":"5h","explicit":true},{"label":"2","explicit":true},{"label":"3","explicit":true}]`],
  ["positions no reading → one numbered", () => json(positions([], undefined, 0)),
    `[{"label":"1","explicit":false}]`],
  ["positions empty labels → numbered", () => json(positions([90], [], 0)),
    `[{"label":"1","explicit":true}]`],
  ["positions numbered plus added → one default box", () => json(positions([90], null, 2)),
    `[{"label":"1","explicit":true},{"label":"2","explicit":false}]`],
  ["positions numbered, stale added count → still one default box", () => json(positions([90], null, 3)),
    `[{"label":"1","explicit":true},{"label":"2","explicit":false}]`],
  ["positions numbered, empty cap with added → one box", () => json(positions([], null, 3)),
    `[{"label":"1","explicit":false}]`],
  ["positions numbered, default elements", () => json(positions(["default", "default", 80], null, 0)),
    `[{"label":"1","explicit":false},{"label":"2","explicit":false},{"label":"3","explicit":true}]`],
  // + (numbered boxes only) shows when the last box is set.
  ["plusShown last set", () => plusShown(positions(["default", 80], null, 0), false), true],
  ["plusShown last default", () => plusShown(positions([80, "default"], null, 0), false), false],
  ["plusShown one default box", () => plusShown(positions([], null, 0), false), false],
  ["plusShown labelled → never", () => plusShown(positions([90, 80], ["5h", "7d"], 0), true), false],
  // elementOf shows a stored element as it is, in range or not.
  ["elementOf hand-written pace+60 stays 60", () => json(elementOf("pace+60")), `{"kind":"pace","n":60}`],
  ["elementOf pace+0", () => json(elementOf("pace+0")), `{"kind":"pace","n":0}`],
  ["elementOf 120 stays 120", () => json(elementOf(120)), `{"kind":"fixed","n":120}`],
  // boundNumber reads the frame's number box: null for garbage (the box
  // reverts), else the number clamped to the kind's range.
  ["boundNumber 90 fixed", () => boundNumber("90", "fixed"), 90],
  ["boundNumber ' 90% ' fixed", () => boundNumber(" 90% ", "fixed"), 90],
  ["boundNumber 12.5 fixed keeps the decimal", () => boundNumber("12.5", "fixed"), 12.5],
  ["boundNumber 120 fixed → 100", () => boundNumber("120", "fixed"), 100],
  ["boundNumber -3 fixed → 0", () => boundNumber("-3", "fixed"), 0],
  ["boundNumber 70 pace → 50", () => boundNumber("70", "pace"), 50],
  ["boundNumber +10 pace", () => boundNumber("+10", "pace"), 10],
  ["boundNumber 0 pace", () => boundNumber("0", "pace"), 0],
  ["boundNumber -2 pace → 0", () => boundNumber("-2", "pace"), 0],
  ["boundNumber 12.6 pace rounds to 13", () => boundNumber("12.6", "pace"), 13],
  ["boundNumber abc → null", () => boundNumber("abc", "fixed"), null],
  ["boundNumber 9x → null", () => boundNumber("9x", "pace"), null],
  ["boundNumber pace+10 typed in the number → null", () => boundNumber("pace+10", "pace"), null],
  ["boundNumber empty → null", () => boundNumber("  ", "fixed"), null],
  ["boundNumber 1e3 → null", () => boundNumber("1e3", "fixed"), null],
  // Owner ruling 2026-10-23 (round 3): switching kind shows 90 (fixed) or
  // +5 (pace); switching back to the stored kind shows the stored value.
  ["switchKind pace+10 → % shows 90", () => json(switchKind("fixed", { kind: "pace", n: 10 })),
    `{"kind":"fixed","n":90}`],
  ["switchKind 80 → pace shows +5", () => json(switchKind("pace", { kind: "fixed", n: 80 })),
    `{"kind":"pace","n":5}`],
  ["switchKind back to stored pace shows +10", () => json(switchKind("pace", { kind: "pace", n: 10 })),
    `{"kind":"pace","n":10}`],
  ["switchKind back to stored 80 shows 80", () => json(switchKind("fixed", { kind: "fixed", n: 80 })),
    `{"kind":"fixed","n":80}`],
  ["switchKind sequence pace+10 → % → pace", () => {
    const stored = { kind: "pace", n: 10 };
    return [switchKind("fixed", stored), switchKind("pace", stored)].map((v) => capLine(v)).join(" / ");
  }, "Cap at 90% / Cap at time elapsed + 10%"],
  // previewOf is what the bar shows while the box holds text: the typed
  // number clamped, else the kind's value from switchKind.
  ["previewOf 70 in pace → pace+50", () => json(previewOf("pace", "70", { kind: "pace", n: 10 })),
    `{"kind":"pace","n":50}`],
  ["previewOf garbage in the stored kind → stored", () => json(previewOf("pace", "x", { kind: "pace", n: 10 })),
    `{"kind":"pace","n":10}`],
  ["previewOf garbage in the other kind → its default", () => json(previewOf("fixed", "x", { kind: "pace", n: 10 })),
    `{"kind":"fixed","n":90}`],
  ["previewOf 150 fixed → 100", () => json(previewOf("fixed", "150", { kind: "fixed", n: 80 })),
    `{"kind":"fixed","n":100}`],
  ["previewOf stored pace+60 untouched → drawn at 50", () => json(previewOf("pace", "60", { kind: "pace", n: 60 })),
    `{"kind":"pace","n":50}`],
  // The frame's line 2: one sentence, only the number editable.
  ["lineParts fixed", () => json(lineParts("fixed")), `["Cap at ","%"]`],
  ["lineParts pace", () => json(lineParts("pace")), `["Cap at time elapsed + ","%"]`],
  ["capLine fixed 90", () => capLine({ kind: "fixed", n: 90 }), "Cap at 90%"],
  ["capLine pace 10", () => capLine({ kind: "pace", n: 10 }), "Cap at time elapsed + 10%"],
  // The bar is 240 px; the range's thumb is 14 px, so the range sits 7 px
  // out on each side and its thumb centre meets x at every value.
  ["barGeometry fixed 90", () => json(barGeometry({ kind: "fixed", n: 90 })),
    `{"x":216,"left":-7,"width":254,"range":{"min":0,"max":100,"value":90}}`],
  ["barGeometry fixed 0", () => json(barGeometry({ kind: "fixed", n: 0 })),
    `{"x":0,"left":-7,"width":254,"range":{"min":0,"max":100,"value":0}}`],
  ["barGeometry fixed 120 clamps to the end", () => json(barGeometry({ kind: "fixed", n: 120 })),
    `{"x":240,"left":-7,"width":254,"range":{"min":0,"max":100,"value":100}}`],
  // Pace (owner, round 3): N runs 0–50; the range starts at elapsed (50% =
  // 120 px) for pace+0 and ends at 100% for pace+50.
  ["barGeometry pace+10 at 60%", () => json(barGeometry({ kind: "pace", n: 10 })),
    `{"x":144,"left":113,"width":134,"range":{"min":0,"max":50,"value":10}}`],
  ["barGeometry pace+0 at the elapsed mark", () => barGeometry({ kind: "pace", n: 0 }).x, 120],
  ["barGeometry pace+50 at the end", () => barGeometry({ kind: "pace", n: 50 }).x, 240],
  ["barGeometry hand-written pace+60 drawn at the end, range at 50",
    () => json([barGeometry({ kind: "pace", n: 60 }).x, barGeometry({ kind: "pace", n: 60 }).range.value]),
    "[240,50]"],
  ["barGeometry pace thumb centre meets x at every N", () => {
    const off = [];
    for (let n = 0; n <= 50; n++) {
      const g = barGeometry({ kind: "pace", n });
      const c = g.left + 7 + (n / 50) * (g.width - 14);
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
