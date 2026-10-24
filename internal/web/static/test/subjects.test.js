// Cases for js/subjects.js: the doctor subjects that redden a choice card
// and a profile link, in the grammar the live back end sends (web/api.go,
// contract 6; webtest pins the back-end half). In the browser,
// subjects.test.html writes the verdict into <output id="result">;
// elsewhere run() resolves it.
import { choiceSubject, profileHasError } from "../js/subjects.js";

const red = (...subjects) => new Set(subjects);

const cases = [
  ["first card's subject", () => choiceSubject("coder", 0), "profile coder, choice 1"],
  ["third card's subject", () => choiceSubject("coder", 2), "profile coder, choice 3"],
  ["card 3 red for choice 3", () => red("profile coder, choice 3").has(choiceSubject("coder", 2)), true],
  ["card 1 not red for choice 3", () => red("profile coder, choice 3").has(choiceSubject("coder", 0)), false],
  ["link red for a profile error", () => profileHasError(red("profile coder"), "coder"), true],
  ["link red for a choice error", () => profileHasError(red("go-1", "profile coder, choice 3"), "coder"), true],
  ["link not red for another profile's choice",
    () => profileHasError(red("profile coder-two, choice 1", "profile coder-two"), "coder"), false],
  ["link not red for an account of that name", () => profileHasError(red("coder"), "coder"), false],
  ["link not red with no errors", () => profileHasError(red(), "coder"), false],
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
