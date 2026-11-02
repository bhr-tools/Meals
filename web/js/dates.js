// Calendar helpers. Days are "YYYY-MM-DD" strings in the user's local time;
// never use toISOString() for days: it converts to UTC and shifts the date
// around midnight.

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTHS_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function isoDate(d) {
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function todayISO() {
  return isoDate(new Date());
}

/** Date at local midnight for "YYYY-MM-DD". */
export function parseISO(iso) {
  const [y, m, d] = iso.split('-').map(Number);
  return new Date(y, m - 1, d);
}

/** Calendar days from a to b (both "YYYY-MM-DD"). */
export function daysBetween(a, b) {
  return Math.round((parseISO(b) - parseISO(a)) / 86400000);
}

/** "Today", "Tomorrow" or the weekday name. */
export function dayName(iso, today) {
  const diff = daysBetween(today, iso);
  if (diff === 0) return 'Today';
  if (diff === 1) return 'Tomorrow';
  return WEEKDAYS[parseISO(iso).getDay()];
}

/** "Mon 2 Nov". */
export function shortDate(iso) {
  const d = parseISO(iso);
  return `${WEEKDAYS[d.getDay()].slice(0, 3)} ${d.getDate()} ${MONTHS_SHORT[d.getMonth()]}`;
}

/** How long ago a recipe was last planned: "never", "today", "3d ago", "2w ago", "in 2d". */
export function sinceLabel(iso, today) {
  if (!iso) return 'never';
  const diff = daysBetween(iso, today);
  if (diff === 0) return 'today';
  if (diff < 0) return `in ${-diff}d`;
  if (diff < 14) return `${diff}d ago`;
  if (diff < 60) return `${Math.round(diff / 7)}w ago`;
  return `${Math.round(diff / 30)}mo ago`;
}
