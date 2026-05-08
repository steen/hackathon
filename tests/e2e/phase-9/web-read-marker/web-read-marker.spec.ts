// Phase 9 #872 — Web `useReadMarker` debounce + focus-flush, end-to-end.
//
// This file is the Playwright placeholder for the read-marker e2e. It
// cannot run today because no UI surface drives `useReadMarker` yet —
// channel-badge UI lands in #873, DM UI lands in #874. Without a user
// interaction that calls `markRead()`, a browser-level test has nothing
// to exercise. Vitest unit tests in
// `apps/web/src/hooks/useReadMarker.test.tsx` cover debounce collapse,
// trailing-250ms timing, immediate flush, and unmount-flush at the hook
// level; the missing piece is a real-browser probe (which #873/#874
// will provide via UI scroll + tab-focus).
//
// The spec is parked here so #873/#874 see it and can fill it in. When
// `apps/web/playwright.config.ts` is broadened to cover this directory
// (or the spec is moved into `tests/e2e/playwright/`), the suite will
// pick it up automatically.

import { test } from "@playwright/test";

test.describe("Web read-marker e2e (deferred)", () => {
  test.skip("debounce — rapid markRead calls collapse to one POST /read in the network log", () => {
    // Lands with #873 (channel) or #874 (DM): scroll the message list
    // through 5+ messages, verify exactly one POST /api/channels/{id}/read
    // (or /api/dms/{id}/read) hits the network within 250ms.
  });

  test.skip("focus return — visibility-change flushes the pending advance immediately", () => {
    // Lands with #874: with a pending markRead in flight, dispatch a
    // tab-blur + tab-focus and confirm POST fires before the 250ms
    // window would have elapsed.
  });
});
