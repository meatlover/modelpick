// Cases for the pure functions of js/caps.js. In the browser,
// caps.test.html writes the verdict into <output id="result">; elsewhere
// run() returns it.
import { parseCap, capArg, capText, writeCap, trashAction, positions, switchKind, barGeometry, scaleText, elementOf,
  boundNumber, previewOf, capLine, lineParts, isSet } from "../js/caps.js";

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
  ["parseCap pace-x is bad", () => json(parseCap("pace-x")), `{"kind":"bad","raw":"pace-x"}`],
  ["parseCap pace10 (no sign) is bad", () => json(parseCap("pace10")), `{"kind":"bad","raw":"pace10"}`],
  ["parseCap pace+-10 is bad", () => json(parseCap("pace+-10")), `{"kind":"bad","raw":"pace+-10"}`],
  ["parseCap ' PACE-10% ' is signed", () => json(parseCap(" PACE-10% ")), `{"kind":"pace","n":-10}`],
  ["parseCap pace\u221210 (U+2212) is signed", () => json(parseCap("pace\u221210")), `{"kind":"pace","n":-10}`],
  ["parseCap pace-0 is 0, not -0", () => Object.is(parseCap("pace-0").n, 0), true],
  // Owner ruling 2026-10-23 (round 3): every typed number is bounded, out
  // of range clamps: fixed 0–100; pace −50…+50, continuous, including 0
  // (owner, 2026-11-01).
  ["pace+2 is sent as pace+2", () => sent("pace+2"), `"pace+2"`],
  ["pace+0 is sent as pace+0", () => sent("pace+0"), `"pace+0"`],
  ["pace+5 is sent as pace+5", () => sent("pace+5"), `"pace+5"`],
  ["pace-10 is sent as pace-10", () => sent("pace-10"), `"pace-10"`],
  ["pace\u221210 (U+2212) is sent as pace-10", () => sent("pace\u221210"), `"pace-10"`],
  ["pace-0 is sent as pace+0", () => sent("pace-0"), `"pace+0"`],
  ["pace-70 clamps to pace-50", () => sent("pace-70"), `"pace-50"`],
  ["pace-50 is sent as pace-50", () => sent("pace-50"), `"pace-50"`],
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
  ["isSet negative pace", () => isSet("pace-5"), true],
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
  ["trashAction default element → none", () => trashAction([90, "default", 95], 1), null],
  ["trashAction empty cap, first box → none (Remove all caps uncaps)", () => trashAction([], 0), null],
  ["trashAction only default elements, first box → none", () => trashAction(["default"], 0), null],
  ["trashAction empty cap, later box → none", () => trashAction([], 2), null],
  ["trashAction first box default, a later one set → none", () => trashAction(["default", "default", 95], 0),
    null],
  ["trashAction default after an explicit → none", () => trashAction([90], 1), null],
  // Task 23: a value typed (or sent, not yet answered) into a default box;
  // trash puts the default back. Round 5: on any default box.
  ["trashAction typed first default → discard", () => trashAction([90], 1, true), "discard"],
  ["trashAction typed second default → discard", () => trashAction([90], 2, true), "discard"],
  ["trashAction typed default element → discard", () => trashAction([90, "default", 95], 1, true), "discard"],
  ["trashAction typed middle explicit → default", () => trashAction([90, 80, 70], 1, true), "default"],
  ["trashAction typed last explicit → default", () => trashAction([90, 80], 1, true), "default"],
  ["trashAction typed box 1, none explicit → discard", () => trashAction([], 0, true), "discard"],
  ["positions labelled", () => json(positions([90, "pace+10"], ["5h", "7d"])),
    `[{"label":"5h","explicit":true},{"label":"7d","explicit":true}]`],
  ["positions labelled, all default", () => json(positions([], ["5h", "7d", "7d Fable"])),
    `[{"label":"5h","explicit":false},{"label":"7d","explicit":false},{"label":"7d Fable","explicit":false}]`],
  ["positions labelled, a default element", () => json(positions([90, "default", 95], ["5h", "7d", "7d Fable"])),
    `[{"label":"5h","explicit":true},{"label":"7d","explicit":false},{"label":"7d Fable","explicit":true}]`],
  ["positions labelled, longer cap numbered", () => json(positions([90, 80, 70], ["5h"])),
    `[{"label":"5h","explicit":true},{"label":"2","explicit":true},{"label":"3","explicit":true}]`],
  ["positions no reading → one numbered", () => json(positions([], undefined)),
    `[{"label":"1","explicit":false}]`],
  ["positions empty labels → numbered", () => json(positions([90], [])),
    `[{"label":"1","explicit":true}]`],
  ["positions numbered, configured count only (nothing adds one)", () => json(positions([90], null)),
    `[{"label":"1","explicit":true}]`],
  ["positions numbered, default elements", () => json(positions(["default", "default", 80], null)),
    `[{"label":"1","explicit":false},{"label":"2","explicit":false},{"label":"3","explicit":true}]`],
  // elementOf shows a stored element as it is, in range or not.
  ["elementOf hand-written pace+60 stays 60", () => json(elementOf("pace+60")), `{"kind":"pace","n":60}`],
  ["elementOf hand-written pace+2 stays 2", () => json(elementOf("pace+2")), `{"kind":"pace","n":2}`],
  ["elementOf hand-written pace-60 stays -60", () => json(elementOf("pace-60")), `{"kind":"pace","n":-60}`],
  ["elementOf pace-10", () => json(elementOf("pace-10")), `{"kind":"pace","n":-10}`],
  ["elementOf 120 stays 120", () => json(elementOf(120)), `{"kind":"fixed","n":120}`],
  // capText shows a stored element in a closed box, as written plus %.
  ["capText 90", () => capText(90), "90%"],
  ["capText pace+10", () => capText("pace+10"), "pace+10%"],
  ["capText pace-10", () => capText("pace-10"), "pace-10%"],
  ["capText pace+0", () => capText("pace+0"), "pace+0%"],
  ["capArg pace -10", () => capArg({ kind: "pace", n: -10 }), "pace-10"],
  ["capArg pace 0", () => capArg({ kind: "pace", n: 0 }), "pace+0"],
  ["capArg pace -0", () => capArg({ kind: "pace", n: -0 }), "pace+0"],
  ["capArg pace 10", () => capArg({ kind: "pace", n: 10 }), "pace+10"],
  // boundNumber reads the frame's number box: null for garbage (the box
  // reverts), else the number clamped to the kind's range. A pace number
  // takes its sign from a leading + / - / U+2212, else from the sentence
  // (the third argument, default +).
  ["boundNumber 90 fixed", () => boundNumber("90", "fixed"), 90],
  ["boundNumber ' 90% ' fixed", () => boundNumber(" 90% ", "fixed"), 90],
  ["boundNumber 12.5 fixed keeps the decimal", () => boundNumber("12.5", "fixed"), 12.5],
  ["boundNumber 120 fixed → 100", () => boundNumber("120", "fixed"), 100],
  ["boundNumber -3 fixed → 0", () => boundNumber("-3", "fixed"), 0],
  ["boundNumber 70 pace → 50", () => boundNumber("70", "pace"), 50],
  ["boundNumber +10 pace", () => boundNumber("+10", "pace"), 10],
  ["boundNumber 5 pace", () => boundNumber("5", "pace"), 5],
  ["boundNumber 2 pace", () => boundNumber("2", "pace"), 2],
  ["boundNumber +2 pace", () => boundNumber("+2", "pace"), 2],
  ["boundNumber 0 pace", () => boundNumber("0", "pace"), 0],
  ["boundNumber -2 pace", () => boundNumber("-2", "pace"), -2],
  ["boundNumber -70 pace → -50", () => boundNumber("-70", "pace"), -50],
  ["boundNumber 70 in a − sentence → -50", () => boundNumber("70", "pace", -1), -50],
  ["boundNumber 4.6 pace rounds to 5", () => boundNumber("4.6", "pace"), 5],
  ["boundNumber 12.6 pace rounds to 13", () => boundNumber("12.6", "pace"), 13],
  ["boundNumber -4.6 pace rounds to -5", () => boundNumber("-4.6", "pace"), -5],
  ["boundNumber 12.5 and -12.5 round away from 0 alike",
    () => json([boundNumber("12.5", "pace"), boundNumber("-12.5", "pace")]), "[13,-13]"],
  ["boundNumber 15 in a − sentence → -15", () => boundNumber("15", "pace", -1), -15],
  ["boundNumber 15 in a + sentence → 15", () => boundNumber("15", "pace", 1), 15],
  ["boundNumber +15 in a − sentence → 15", () => boundNumber("+15", "pace", -1), 15],
  ["boundNumber -15 in a + sentence → -15", () => boundNumber("-15", "pace", 1), -15],
  ["boundNumber \u221215 (U+2212) in a + sentence → -15", () => boundNumber("\u221215", "pace", 1), -15],
  ["boundNumber ' -15% ' pace → -15", () => boundNumber(" -15% ", "pace"), -15],
  ["boundNumber '- 15' pace (space after the sign) → -15", () => boundNumber("- 15", "pace"), -15],
  ["boundNumber -0 in a − sentence → 0, not -0", () => Object.is(boundNumber("-0", "pace", -1), 0), true],
  ["boundNumber 0 in a − sentence → 0, not -0", () => Object.is(boundNumber("0", "pace", -1), 0), true],
  ["boundNumber 0.4 in a − sentence → 0, not -0", () => Object.is(boundNumber("0.4", "pace", -1), 0), true],
  ["boundNumber --15 pace → null", () => boundNumber("--15", "pace"), null],
  ["boundNumber +-15 pace → null", () => boundNumber("+-15", "pace"), null],
  ["boundNumber - alone pace → null", () => boundNumber("-", "pace", -1), null],
  ["boundNumber 15 in a − sentence, fixed ignores the sign", () => boundNumber("15", "fixed", -1), 15],
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
  ["previewOf 2 in pace → pace+2", () => json(previewOf("pace", "2", { kind: "pace", n: 10 })),
    `{"kind":"pace","n":2}`],
  ["previewOf 15 in a − sentence → pace-15", () => json(previewOf("pace", "15", { kind: "pace", n: -10 }, -1)),
    `{"kind":"pace","n":-15}`],
  ["previewOf -15 typed in a + sentence → pace-15", () => json(previewOf("pace", "-15", { kind: "pace", n: 10 }, 1)),
    `{"kind":"pace","n":-15}`],
  ["previewOf garbage in a − sentence → stored pace-10",
    () => json(previewOf("pace", "x", { kind: "pace", n: -10 }, -1)), `{"kind":"pace","n":-10}`],
  ["previewOf stored pace-60 untouched → drawn at -50",
    () => json(previewOf("pace", "60", { kind: "pace", n: -60 }, -1)), `{"kind":"pace","n":-50}`],
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
  ["lineParts pace 10", () => json(lineParts("pace", 10)), `["Cap at time elapsed + ","%"]`],
  ["lineParts pace 0 reads +", () => json(lineParts("pace", 0)), `["Cap at time elapsed + ","%"]`],
  ["lineParts pace -10 (U+2212 in prose)", () => json(lineParts("pace", -10)), `["Cap at time elapsed \u2212 ","%"]`],
  ["lineParts fixed ignores the sign", () => json(lineParts("fixed", -1)), `["Cap at ","%"]`],
  ["capLine fixed 90", () => capLine({ kind: "fixed", n: 90 }), "Cap at 90%"],
  ["capLine pace 10", () => capLine({ kind: "pace", n: 10 }), "Cap at time elapsed + 10%"],
  ["capLine pace -10", () => capLine({ kind: "pace", n: -10 }), "Cap at time elapsed \u2212 10%"],
  ["capLine pace 0", () => capLine({ kind: "pace", n: 0 }), "Cap at time elapsed + 0%"],
  ["capLine pace -0", () => capLine({ kind: "pace", n: -0 }), "Cap at time elapsed + 0%"],
  // The bar is 196 px, the editor's width for "Cap at time elapsed + 50%"
  // (owner, 2026-10-24); the range's thumb is 14 px, so the range sits 7 px
  // out on each side and its thumb centre meets x at every value.
  ["barGeometry fixed 90", () => json(barGeometry({ kind: "fixed", n: 90 })),
    `{"x":176.4,"left":-7,"width":210,"range":{"min":0,"max":100,"value":90}}`],
  ["barGeometry fixed 0", () => json(barGeometry({ kind: "fixed", n: 0 })),
    `{"x":0,"left":-7,"width":210,"range":{"min":0,"max":100,"value":0}}`],
  ["barGeometry fixed 120 clamps to the end", () => json(barGeometry({ kind: "fixed", n: 120 })),
    `{"x":196,"left":-7,"width":210,"range":{"min":0,"max":100,"value":100}}`],
  // Pace (owner, 2026-11-01: −50…+50, continuous, including 0): N sits at
  // elapsed + N, and with the illustrative elapsed of 50% the range spans
  // the whole bar, like fixed: −50 at 0, 0 at the middle, +50 at the end.
  ["barGeometry pace+10 at 60%", () => json(barGeometry({ kind: "pace", n: 10 })),
    `{"x":117.6,"left":-7,"width":210,"range":{"min":-50,"max":50,"value":10}}`],
  ["barGeometry pace-10 at 40%", () => json(barGeometry({ kind: "pace", n: -10 })),
    `{"x":78.4,"left":-7,"width":210,"range":{"min":-50,"max":50,"value":-10}}`],
  ["barGeometry pace-50 at the start", () => barGeometry({ kind: "pace", n: -50 }).x, 0],
  ["barGeometry pace+0 at the middle", () => barGeometry({ kind: "pace", n: 0 }).x, 98],
  ["barGeometry pace+50 at the end", () => barGeometry({ kind: "pace", n: 50 }).x, 196],
  ["barGeometry hand-written pace+2 drawn at pace+2",
    () => json([barGeometry({ kind: "pace", n: 2 }).x, barGeometry({ kind: "pace", n: 2 }).range.value]),
    "[101.92,2]"],
  ["barGeometry hand-written pace+60 drawn at the end, range at 50",
    () => json([barGeometry({ kind: "pace", n: 60 }).x, barGeometry({ kind: "pace", n: 60 }).range.value]),
    "[196,50]"],
  ["barGeometry hand-written pace-60 drawn at the start, range at -50",
    () => json([barGeometry({ kind: "pace", n: -60 }).x, barGeometry({ kind: "pace", n: -60 }).range.value]),
    "[0,-50]"],
  ["barGeometry pace thumb centre meets x at every N", () => {
    const off = [];
    for (let n = -50; n <= 50; n++) {
      const g = barGeometry({ kind: "pace", n });
      const c = g.left + 7 + ((n + 50) / 100) * (g.width - 14);
      if (Math.abs(c - g.x) > 1e-9) off.push(n);
    }
    return json(off);
  }, "[]"],
  // Owner, 2026-11-01: the captions say the fill is an example (U+2014).
  ["scaleText fixed", () => scaleText("fixed"), "Example \u2014 token usage: 33%"],
  ["scaleText pace", () => scaleText("pace"), "Example \u2014 time elapsed: 50%"],
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
