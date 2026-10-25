// Where the server finds the choice an op addresses by index and expect
// (findChoice and findGroup in internal/app/opsprofile.go and opsgroup.go):
// the choice at index when it is the one expected, else the one identical
// choice on the same side of a group boundary. The page follows a card with
// it after an op of its own moved the card before the card was redrawn.
import { positions } from "./subjects.js";

const list = (l) => JSON.stringify(l || []);

// capKey is a wire cap as compared: null (uncapped) differs from [], and
// trailing "default" positions are ignored.
function capKey(cap) {
  if (cap === null || cap === undefined) return "null";
  let n = cap.length;
  while (n > 0 && cap[n - 1] === "default") n--;
  return JSON.stringify(cap.slice(0, n));
}

// sameChoice compares two wire choices as sameChoice in opsprofile.go does:
// null and [] schedules alike; group and algorithm are left to sameStatus.
function sameChoice(a, b) {
  return a.account === b.account && a.model === b.model && (a.thinking ?? null) === (b.thinking ?? null) &&
    capKey(a.cap) === capKey(b.cap) && list(a.skip_during) === list(b.skip_during) &&
    list(a.allow_during) === list(b.allow_during);
}

const inGroup = (c) => (c.group || 0) > 0;

// sameStatus: both plain, or both members of a group with one algorithm.
function sameStatus(a, b) {
  return inGroup(a) === inGroup(b) && (!inGroup(a) || a.load_balance === b.load_balance);
}

// sameGroup: the position holds members identical, in order, to group
// ({load_balance, choices}, an op's member_of).
function sameGroup(choices, pos, group) {
  return pos.group > 0 && pos.load_balance === group.load_balance && pos.end - pos.start === group.choices.length &&
    group.choices.every((w, k) => sameChoice(choices[pos.start + k], w));
}

// theOne is index when match holds there, else the one i in [lo, hi) it
// holds for, else -1.
function theOne(index, lo, hi, match) {
  if (index >= lo && index < hi && match(index)) return index;
  let found = -1;
  let n = 0;
  for (let i = lo; i < hi; i++) {
    if (match(i)) {
      found = i;
      n += 1;
    }
  }
  return n === 1 ? found : -1;
}

// locateChoice is the flat index in choices at which the server applies an
// op on index with expect and memberOf (null for a plain card), or -1 where
// it refuses the op. A member is looked for only inside its group, found as
// the group memberOf names; without memberOf, only at index.
export function locateChoice(choices, index, expect, memberOf) {
  let lo = 0;
  let hi = choices.length;
  if (inGroup(expect)) {
    if (!memberOf) {
      lo = index;
      hi = Math.min(index + 1, choices.length);
    } else {
      const ps = positions(choices);
      const at = ps.findIndex((p) => index >= p.start && index < p.end);
      const g = theOne(at, 0, ps.length, (n) => sameGroup(choices, ps[n], memberOf));
      if (g < 0) return -1;
      lo = ps[g].start;
      hi = ps[g].end;
    }
  }
  return theOne(index, lo, hi, (i) => sameStatus(choices[i], expect) && sameChoice(choices[i], expect));
}
