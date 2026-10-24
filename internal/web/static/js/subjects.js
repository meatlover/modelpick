// Doctor subjects as the back end sends them (web/api.go, contract 6): an
// account name, "profile <p>", "profile <p>, choice <n>" with n 1-based,
// "schedule <s>" or "config". The page reddens a card and a profile link
// by these, so the grammar lives here once and its test pins it.

// choiceSubject is the subject of profile's choice at index i (0-based).
export function choiceSubject(profile, i) {
  return `profile ${profile}, choice ${i + 1}`;
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
