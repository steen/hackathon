### Web — accessibility

- Presence join/leave events now announce via a polite `aria-live` status
  region next to the online list. The list itself reorders rather than
  appends, so SR users previously had no audible signal for arrivals or
  departures. Falls back to "a new user joined/left" when the id is not in
  the seeded directory rather than reading out a UUID. (#491)
