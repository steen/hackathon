- Pinned the system clock with `vi.useFakeTimers({ toFake: ["Date"] })`
  and `vi.setSystemTime(...)` in two `apps/web/src/routes/Chat.test.tsx`
  cases that previously read ambient `Date.now()` (humanized-timestamp
  "today" branch and the same-local-day divider assertion). The flake
  fired whenever the runner's local clock sat near midnight — the +5h
  fixture crossed local midnight and a divider appeared between the two
  messages, breaking the count. `toFake: ["Date"]` leaves
  `setTimeout`/`setInterval` real so RTL's `waitFor` keeps polling.
  Verified locally with `TZ=UTC`, `TZ=America/New_York`,
  `TZ=Pacific/Auckland`, `TZ=Asia/Kolkata`. Closes #847.
