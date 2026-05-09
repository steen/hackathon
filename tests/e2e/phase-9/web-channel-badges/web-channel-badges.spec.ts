// Phase 9 #873 — Web channel unread badges, end-to-end.
//
// This file is the Playwright placeholder for the badge e2e. It cannot
// run today: `apps/web/playwright.config.ts` scopes its `testDir` to
// `tests/e2e/playwright/` (see the comment in the same config), and
// this directory is outside that path. The placeholder stays here per
// the same convention as `tests/e2e/phase-9/web-read-marker/` so the
// AC scenarios stay co-located with the sub-issue's footprint until a
// later PR broadens the testDir or moves the spec.
//
// Hook-level coverage that DOES run today (vitest workspace):
//
//   - apps/web/src/hooks/useChannels.test.ts — `read:channel` WS frame
//     zeroes `unread_count` (cross-tab sync); unknown id is a no-op;
//     `read:dm` is ignored by useChannels.
//   - apps/web/src/routes/Chat.test.tsx — POST /api/channels/{id}/read
//     fires under the active+visible gate; tab-hidden suppresses it.
//   - packages/chat-ui/src/ChannelsList/ChannelsList.test.tsx — badge
//     renders when `unread_count > 0`, caps at 99+, exposes the exact
//     count to SR via aria-label.
//
// The Playwright AC list below covers the integrated scenarios that a
// real browser confirms (proxy + WS + multi-tab):

import { test } from "@playwright/test";

test.describe("Web channel unread badges e2e (deferred)", () => {
  test.skip("AC: per-channel badge renders when unread_count > 0", () => {
    // Register a viewer; have a second user post N messages to a channel
    // the viewer hasn't selected. Open the web app as the viewer and
    // assert the sidebar entry for that channel renders the badge with
    // text matching the post count.
  });

  test.skip("AC: badge clears on focus + scroll-to-bottom (calls useReadMarker)", () => {
    // With a non-zero badge present on an active channel, scroll the
    // message list to the bottom and verify a POST /api/channels/{id}/read
    // hits the network. After the WS read-frame round-trips, the sidebar
    // badge for that channel disappears.
  });

  test.skip("AC: reload preserves badge state via server-side last_read", () => {
    // After consuming N messages and confirming the badge clears, reload
    // the page (full SPA reload). The sidebar fetch /api/channels returns
    // the server-tracked unread_count (= 0); the badge stays cleared
    // without any client-side re-mark.
  });

  test.skip("AC: cross-tab sync — tab A advances last_read; tab B WS receives {type:'read'} and clears", () => {
    // Open two browser contexts as the same user. Drive a markRead in
    // tab A (focus the channel + scroll to bottom). Tab B's WebSocket
    // receives the {type:"read", scope:"channel"} frame on the
    // user:<viewer> topic and zeroes the in-memory unread_count for the
    // matching channel — the sidebar badge in tab B disappears without
    // a reload.
  });
});
