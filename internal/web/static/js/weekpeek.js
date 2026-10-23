// A small read-only week of one schedule, for the + schedule picker's hover
// preview: day initials (Sun–Sat) across, 24 hours down, painted cells in
// --paint. Cells are shown in the header's zone, as on the Schedules tab;
// a partial hour's fill height comes from the data (CSSOM).
import { h } from "./dom.js";
import { zone, localOffsetMinutes, shownFraction } from "./time.js";

const INITIALS = ["S", "M", "T", "W", "T", "F", "S"];

function cell(sched, d, hr, offset) {
  const f = Math.min(1, Math.max(0, shownFraction(sched.cells, d, hr, offset)));
  let fill = null;
  if (f > 0) {
    fill = h("span", { class: "sched-peek-fill" });
    fill.style.height = `${f * 100}%`;
  }
  return h("span", { class: "sched-peek-cell" }, fill);
}

// weekPeek builds the preview of sched ({name, cells}). It is decoration
// for the option that names the schedule, so it is hidden from the
// accessibility tree.
export function weekPeek(sched) {
  const offset = zone() === "local" ? localOffsetMinutes() : 0;
  const kids = [INITIALS.map((c) => h("span", { class: "sched-peek-day" }, c))];
  for (let hr = 0; hr < 24; hr++) {
    for (let d = 0; d < 7; d++) kids.push(cell(sched, d, hr, offset));
  }
  return h("div", { class: "sched-peek", "aria-hidden": "true", "data-name": sched.name }, kids);
}
