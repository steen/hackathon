// Visible message timestamp. The raw RFC3339 string stays in the
// <time dateTime={...}> attribute for SR / scrapers; this is just the
// human label.
//
// Rule (24h, locale-neutral by design — fixed widths read consistently
// in a chat list):
//   - today (same Y-M-D in viewer's local zone)        → "HH:MM"
//   - within the last 6 days but not today             → "Mon HH:MM"
//   - older                                            → "MMM D HH:MM"
//
// `now` is parameterized so tests can pin the clock without mocking Date.

const SHORT_WEEKDAY = new Intl.DateTimeFormat("en-US", { weekday: "short" });
const SHORT_MONTH = new Intl.DateTimeFormat("en-US", { month: "short" });

function pad2(n: number): string {
  return n < 10 ? `0${String(n)}` : String(n);
}

function hhmm(d: Date): string {
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

function isSameLocalDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

export function humanizeTimestamp(iso: string, now: Date = new Date()): string {
  if (iso.length === 0) return "";
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return iso;

  if (isSameLocalDay(t, now)) {
    return hhmm(t);
  }

  // Days-elapsed by local-midnight boundaries, not 24h windows. A
  // message from yesterday 23:50 viewed at today 00:10 is 1 day ago,
  // not "<24h so today".
  const today0 = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const that0 = new Date(t.getFullYear(), t.getMonth(), t.getDate());
  const daysAgo = Math.round((today0.getTime() - that0.getTime()) / 86400000);

  if (daysAgo >= 1 && daysAgo <= 6) {
    return `${SHORT_WEEKDAY.format(t)} ${hhmm(t)}`;
  }

  return `${SHORT_MONTH.format(t)} ${String(t.getDate())} ${hhmm(t)}`;
}
