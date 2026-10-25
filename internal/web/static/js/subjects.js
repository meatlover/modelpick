// Doctor subjects as the back end sends them (web/api.go, contract 6): an
// account name, "profile <p>", "profile <p>, choice <label>", "schedule <s>"
// or "config". A label is a position number ("3"), or for a member of a load
// balancing group its position and a letter ("2b"). The page reddens a card
// and a profile link by these, so the grammar lives here once and its test
// pins it.

// positions splits a profile's flat choices into positions: a plain choice,
// or a load balancing group (a run of members sharing one group id > 0).
// Each is {start, end, group, load_balance}: end is exclusive; group is 0
// and load_balance null for a plain choice, else the group's id and
// algorithm. As core.Positions does.
export function positions(choices) {
  const out = [];
  choices.forEach((c, i) => {
    const g = c.group || 0;
    const last = out[out.length - 1];
    if (g > 0 && last && last.group === g) last.end = i + 1;
    else out.push({ start: i, end: i + 1, group: g, load_balance: g > 0 ? c.load_balance : null });
  });
  return out;
}

// letters is a, b, …, z, aa, ab, … for 0, 1, … (core.letters).
function letters(i) {
  let s = "";
  for (let n = i + 1; n > 0; n = Math.floor((n - 1) / 26)) s = String.fromCharCode(97 + ((n - 1) % 26)) + s;
  return s;
}

// choiceLabels is the label of each of choices, by flat index: "1", "2a",
// "2b", "3", … (core.Labels).
export function choiceLabels(choices) {
  const out = [];
  positions(choices).forEach((p, n) => {
    for (let i = p.start; i < p.end; i++) out.push(p.group > 0 ? `${n + 1}${letters(i - p.start)}` : String(n + 1));
  });
  return out;
}

// choiceSubject is the subject of profile's choice labelled label; a
// group's position number is the subject of the group as a whole.
export function choiceSubject(profile, label) {
  return `profile ${profile}, choice ${label}`;
}

// profileHasError says whether any subject in red (a Set of error
// subjects) names the profile or one of its choices.
export function profileHasError(red, profile) {
  const own = `profile ${profile}`;
  for (const s of red) {
    if (s === own || s.startsWith(`${own}, choice `)) return true;
  }
  return false;
}

// accountErrorSet is the set of names to redden: the subject of every error
// finding, and each name in its accounts (a grouped finding's subject is
// those names joined, which names no card itself). Warnings add nothing.
export function accountErrorSet(findings) {
  const red = new Set();
  for (const f of findings || []) {
    if (f.severity !== "error") continue;
    red.add(f.subject);
    for (const a of f.accounts || []) red.add(a);
  }
  return red;
}
