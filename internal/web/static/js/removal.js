// What removing an account takes with it, worked out from the profiles the
// page holds, for the confirm dialog. The server decides again from the file
// (account.remove with_choices); a stale page is caught by the base-SHA check.

// removalPlan lists, in sorted profile order, each choice that uses account
// ("<profile>: choice <n> (<model>)") and, after the last of a profile whose
// every choice uses it, "<profile> has no other choice and is removed too".
export function removalPlan(profiles, account) {
  const lines = [];
  let count = 0;
  const sorted = [...profiles].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  for (const p of sorted) {
    const hit = p.choices.map((c, i) => (c.account === account ? i : -1)).filter((i) => i >= 0);
    for (const i of hit) lines.push(`${p.name}: choice ${i + 1} (${p.choices[i].model})`);
    if (hit.length > 0 && hit.length === p.choices.length) {
      lines.push(`${p.name} has no other choice and is removed too`);
    }
    count += hit.length;
  }
  return {
    count,
    lines,
    title: `Remove account ${account} and the choices that use it?`,
    label: `Remove account and ${count} ${count === 1 ? "choice" : "choices"}`,
  };
}
