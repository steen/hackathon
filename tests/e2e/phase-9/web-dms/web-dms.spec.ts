// Phase 9 #874 — Web DM feature UI, end-to-end.
//
// This file is the Playwright placeholder for the DM UI e2e. It cannot
// run today: `apps/web/playwright.config.ts` scopes its `testDir` to
// `tests/e2e/playwright/` (see the comment in the same config), and
// this directory is outside that path. The placeholder stays here per
// the same convention as `tests/e2e/phase-9/web-channel-badges/` and
// `tests/e2e/phase-9/web-read-marker/` so the AC scenarios stay
// co-located with the sub-issue's footprint until a later PR broadens
// the testDir or moves the spec.
//
// Hook-level coverage that DOES run today (vitest workspace):
//
//   - apps/web/src/hooks/useDMs.test.ts — listing seed + sort, dm-frame
//     upsert + local unread increment (§12), self-vs-peer sender
//     filtering, §8 self-sufficient first-contact upsert, read-frame
//     zero-out, WS-open reload (gap recovery → §12 reconcile-overwrite),
//     startWith() POST /api/dms idempotency.
//   - apps/web/src/components/DMSidebar.test.tsx — per-row + per-section
//     unread badges, 99+ display cap, aria-current on active row,
//     onSelect/onNew callbacks fire on click.
//   - apps/web/src/components/NewDMModal.test.tsx — /api/users fetch,
//     self filter, alphabetical sort, click → onCreate+onCreated+close,
//     directory error, create-side error keeps modal open, no-other-
//     users empty state.
//
// The Playwright AC list below covers the integrated scenarios that a
// real browser confirms (proxy + WS + multi-tab):

import { test } from "@playwright/test";

test.describe("Web DM UI e2e (deferred)", () => {
  test.skip("AC: 'Direct messages' sidebar section sorts last_message_at DESC", () => {
    // Register two peers, post DMs at staggered times, sign in as the
    // viewer. Assert the sidebar's "Direct messages" section lists the
    // most recently active conversation first.
  });

  test.skip("AC: + New DM opens Phase-8 Modal listing /api/users minus self (L24)", () => {
    // Click "+ New DM"; assert the Modal primitive renders, the user
    // list shows every registered user except the viewer, and clicking
    // a user fires POST /api/dms.
  });

  test.skip("AC: selecting a peer with no prior conversation opens an empty thread", () => {
    // Click a never-messaged peer; assert POST /api/dms returns 201 +
    // navigates to a thread with the empty-channel hint and a focused
    // composer.
  });

  test.skip("AC: send DM persists, broadcasts to peer, and updates the thread immediately", () => {
    // Type a message + Enter; assert POST /api/dms/{id}/messages hits
    // the network, the row appears in the active thread, and the peer's
    // tab (separate browser context) receives the {type:'dm'} frame and
    // bumps the matching sidebar entry's unread badge.
  });

  test.skip("AC: offline-arrived DMs surface on next load with correct unread badges (§12 round-3)", () => {
    // Have peer post N messages while the viewer's browser is closed.
    // Re-open the viewer's browser; assert GET /api/dms returns
    // unread_count = N for that conversation and the badge renders the
    // post count.
  });

  test.skip("AC: per-thread + per-section aggregate unread badges render correctly", () => {
    // With unread > 0 in two conversations, assert the per-row badges
    // render their respective counts and the section header carries an
    // aggregate badge equal to the sum.
  });

  test.skip("AC: cross-tab — incoming {type:'dm'} WS frame increments badge in all open tabs", () => {
    // Open two contexts as the same viewer. Have a peer send a DM;
    // assert both tabs' useDMs receives the frame on the user:<viewer>
    // topic and increments the local unread_count for the conversation,
    // independent of which tab has the thread focused.
  });

  test.skip("AC: focusing a DM thread flushes the read-marker debounce (focus-return path)", () => {
    // With a non-zero badge on a DM, switch tabs away then back;
    // assert POST /api/dms/{id}/read fires inside the focus-flush window
    // (sub-250ms) rather than waiting out the debounce timer, and the
    // sidebar badge clears via the cross-tab read frame.
  });
});
