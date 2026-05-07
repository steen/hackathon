import type * as React from "react";

const SHORT_WEEKDAY = new Intl.DateTimeFormat("en-US", { weekday: "short" });
const LONG_DATE = new Intl.DateTimeFormat("en-US", {
  weekday: "long",
  month: "short",
  day: "numeric",
  year: "numeric",
});

function isSameLocalDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

// Human label for a day-divider relative to "now". Today / Yesterday /
// short weekday for the last week / full date for older. Picked to
// match the way Slack and Element render their day rules; IRC never
// had this convention, but a chat log spanning multiple days is
// unreadable without one.
export function dayLabel(d: Date, now: Date = new Date()): string {
  const today0 = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const that0 = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const daysAgo = Math.round((today0.getTime() - that0.getTime()) / 86400000);

  if (isSameLocalDay(d, now)) return "Today";
  if (daysAgo === 1) return "Yesterday";
  if (daysAgo > 1 && daysAgo <= 6) return SHORT_WEEKDAY.format(d);
  return LONG_DATE.format(d);
}

interface Props {
  iso: string;
  /** Now-anchor for the relative phrasing. Tests pin a fixed `Date` to
      keep the rendered label deterministic; production omits and gets
      the real wall clock. */
  now?: Date;
}

// Day-divider row inserted between two messages whose local dates
// differ. Rendered inside the messages list as a sibling of <article
// class="msg">; CSS pulls it across the full width with a horizontal
// rule and a centered text. ARIA: `role="separator"` so screen readers
// announce "separator" rather than reading the date as if it were a
// message. Aria-label spells the date in full so the SR speech is
// usable even when the visible label is "Today" or a short weekday.
export function DayDivider({ iso, now }: Props): React.JSX.Element {
  const d = new Date(iso);
  const label = Number.isNaN(d.getTime()) ? iso : dayLabel(d, now);
  const fullDate = Number.isNaN(d.getTime()) ? iso : LONG_DATE.format(d);
  return (
    <div
      className="msg-day-divider"
      role="separator"
      aria-label={fullDate}
      data-testid="day-divider"
    >
      <span className="msg-day-divider__label">{label}</span>
    </div>
  );
}
