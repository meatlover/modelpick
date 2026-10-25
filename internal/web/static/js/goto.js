// A doctor finding's Go to (owner, 2026-10-25): open the tab that holds the
// finding's item, scroll to it, move the focus there and light it up for a
// moment. The items carry their doctor subject as data-subject (account
// rows, profile and schedule links, choice cards and group frames), in the
// grammar subjects.js describes.

const FLASH_MS = 1200;

// findingTarget is where f's Go to leads: {hash, subjects}, the tab's hash
// and the subjects of the items to show; null for a finding about the
// whole file ("config"), which has no item. A finding grouped over several
// accounts shows each account's row.
export function findingTarget(f) {
  const s = f.subject;
  if (s === "config") return null;
  if (s.startsWith("schedule ")) {
    return { hash: `#schedules/${encodeURIComponent(s.slice("schedule ".length))}`, subjects: [s] };
  }
  if (s.startsWith("profile ")) {
    const rest = s.slice("profile ".length);
    const cut = rest.lastIndexOf(", choice ");
    const profile = cut < 0 ? rest : rest.slice(0, cut);
    return { hash: `#profiles/${encodeURIComponent(profile)}`, subjects: [s] };
  }
  const accounts = f.accounts && f.accounts.length > 0 ? f.accounts : [s];
  return { hash: "#accounts", subjects: accounts };
}

const reduced = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches;

// itemsOf finds the items with these subjects in main.
function itemsOf(subjects) {
  const main = document.getElementById("main");
  return [...main.querySelectorAll("[data-subject]")].filter((el) => subjects.includes(el.dataset.subject));
}

// whenDrawn resolves with the items once the tab has drawn them, or with
// none after about a second (the item went away meanwhile).
function whenDrawn(subjects) {
  return new Promise((resolve) => {
    const start = performance.now();
    const look = () => {
      const items = itemsOf(subjects);
      if (items.length > 0 || performance.now() - start > 1000) resolve(items);
      else requestAnimationFrame(look);
    };
    requestAnimationFrame(look);
  });
}

// flash lights el up; the class goes after FLASH_MS (app.css fades it).
function flash(el) {
  el.classList.remove("is-flash");
  void el.offsetWidth; // restart the animation on a second Go to
  el.classList.add("is-flash");
  setTimeout(() => el.classList.remove("is-flash"), FLASH_MS);
}

// goTo opens f's tab and shows its items. The first takes the focus: one
// that is not focusable (an account row) becomes so, for the script only.
export async function goTo(f) {
  const target = findingTarget(f);
  if (!target) return;
  if (location.hash !== target.hash) location.hash = target.hash;
  const items = await whenDrawn(target.subjects);
  if (items.length === 0) return;
  const first = items[0];
  if (first.tabIndex < 0 && !first.hasAttribute("tabindex")) first.setAttribute("tabindex", "-1");
  first.focus({ preventScroll: true });
  first.scrollIntoView({ block: "center", behavior: reduced() ? "auto" : "smooth" });
  items.forEach(flash);
}
