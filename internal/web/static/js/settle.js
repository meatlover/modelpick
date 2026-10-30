// Whether the page is done: what api.js has in flight, published on <body>
// for anyone who must know when the view will not be replaced any more (the
// browser tests' act(), a person in the inspector).
//
//   data-settled  "true" when no write is in flight or queued and no reload
//                 is in flight, so the last reload's state is drawn
//   data-gen      how many times the page became settled; it only goes up
//   data-writes   how many writes were sent: ops, Refreshes and Diagnoses,
//                 counted when the user's action starts them
//
// A write counts until everything it does is done, its reload included (a
// refused op, which reloads nothing, until its answer). A reload counts
// until the store has it and every tab drawn from it was redrawn.

const count = { writes: 0, writesDone: 0, reloads: 0, reloadsDone: 0, gen: 0 };
let wasSettled = true; // nothing has started yet

function publish() {
  const settled = count.writes === count.writesDone && count.reloads === count.reloadsDone;
  if (settled && !wasSettled) count.gen += 1;
  wasSettled = settled;
  if (typeof document === "undefined" || !document.body) return;
  const d = document.body.dataset;
  d.settled = String(settled);
  d.gen = String(count.gen);
  d.writes = String(count.writes);
}

// track counts kind ("write" or "reload") from now until promise settles,
// and returns promise.
export function track(kind, promise) {
  const [start, done] = kind === "write" ? ["writes", "writesDone"] : ["reloads", "reloadsDone"];
  count[start] += 1;
  publish();
  promise.finally(() => {
    count[done] += 1;
    publish();
  }).catch(() => {}); // the caller handles a rejection; this copy must not report it
  return promise;
}

publish();
