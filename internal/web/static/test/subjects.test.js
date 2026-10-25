// Cases for js/subjects.js: the doctor subjects that redden a choice card
// and a profile link, in the grammar the live back end sends (web/api.go,
// contract 6; webtest pins the back-end half). In the browser,
// subjects.test.html writes the verdict into <output id="result">;
// elsewhere run() resolves it.
import { accountErrorSet, choiceLabels, choiceSubject, positions, profileHasError } from "../js/subjects.js";

const red = (...subjects) => new Set(subjects);

// plain and member build wire choices: only group and load_balance matter.
const plain = () => ({ group: 0, load_balance: null });
const member = (g) => ({ group: g, load_balance: "random" });
const labels = (...cs) => choiceLabels(cs).join(",");
const spans = (...cs) => positions(cs).map((p) => `${p.start}-${p.end}:${p.load_balance}`).join(" ");

const cases = [
  ["first card's subject", () => choiceSubject("coder", "1"), "profile coder, choice 1"],
  ["third card's subject", () => choiceSubject("coder", "3"), "profile coder, choice 3"],
  ["a member's subject", () => choiceSubject("coder", "2b"), "profile coder, choice 2b"],
  ["card 3 red for choice 3", () => red("profile coder, choice 3").has(choiceSubject("coder", "3")), true],
  ["card 1 not red for choice 3", () => red("profile coder, choice 3").has(choiceSubject("coder", "1")), false],
  ["member 2b red for choice 2b", () => red("profile coder, choice 2b").has(choiceSubject("coder", "2b")), true],
  ["member 2a not red for choice 2b", () => red("profile coder, choice 2b").has(choiceSubject("coder", "2a")), false],
  ["the group's position is not a member", () => red("profile coder, choice 2").has(choiceSubject("coder", "2a")), false],
  ["labels with no group", () => labels(plain(), plain(), plain()), "1,2,3"],
  ["labels count positions", () => labels(plain(), member(1), member(1), plain()), "1,2a,2b,3"],
  ["two groups side by side", () => labels(member(1), member(1), member(2), member(2), member(2)), "1a,1b,2a,2b,2c"],
  ["past z the letters go on as aa", () => choiceLabels(Array.from({ length: 28 }, () => member(1))).slice(25).join(","),
    "1z,1aa,1ab"],
  ["no choices, no labels", () => labels(), ""],
  ["positions span a group", () => spans(plain(), member(1), member(1), plain()), "0-1:null 1-3:random 3-4:null"],
  ["positions split adjacent groups", () => spans(member(1), member(1), member(2), member(2)), "0-2:random 2-4:random"],
  ["link red for a member error", () => profileHasError(red("profile coder, choice 2b"), "coder"), true],
  ["link red for a profile error", () => profileHasError(red("profile coder"), "coder"), true],
  ["link red for a choice error", () => profileHasError(red("go-1", "profile coder, choice 3"), "coder"), true],
  ["link not red for another profile's choice",
    () => profileHasError(red("profile coder-two, choice 1", "profile coder-two"), "coder"), false],
  ["link not red for an account of that name", () => profileHasError(red("coder"), "coder"), false],
  ["link not red with no errors", () => profileHasError(red(), "coder"), false],
  ["grouped error reddens each account",
    () => [...accountErrorSet([{ severity: "error", subject: "go-1, go-2", accounts: ["go-1", "go-2"] }])].sort().join("|"),
    "go-1|go-1, go-2|go-2"],
  ["grouped warning reddens none",
    () => accountErrorSet([{ severity: "warning", subject: "go-1, go-2", accounts: ["go-1", "go-2"] }]).size, 0],
  ["plain account error is itself",
    () => [...accountErrorSet([{ severity: "error", subject: "codex-main" }])].join("|"), "codex-main"],
  ["profile subject is in the set, matches no account",
    () => { const s = accountErrorSet([{ severity: "error", subject: "profile coder" }]); return s.has("profile coder") && !s.has("coder"); }, true],
  ["no doctor, no errors", () => accountErrorSet(null).size, 0],
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
