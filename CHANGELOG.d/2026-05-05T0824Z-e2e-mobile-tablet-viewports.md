### Added

- **tests/e2e/playwright/web-mobile.spec.ts**: Playwright regression coverage for the phone (375x667, iPhone SE class) and tablet (768x1024, iPad class) viewports. Asserts the login → select channel → send-message happy path at each width, the single-column → two-column layout transition across the 768px breakpoint shipped under #612, and the 44px minimum tap targets on `.sidebar li button`, `.composer button`, and `.composer textarea` shipped under #612 + #626. Chromium only; WebKit/iOS Safari coverage deferred to a follow-up under #448. Closes #634. (2026-05-05T08:24Z)
