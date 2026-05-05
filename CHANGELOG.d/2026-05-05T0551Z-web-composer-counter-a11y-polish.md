### Changed

- Chat composer counter no longer double-announces over the cap: the form drops `aria-describedby` once `overCap`, so screen readers only see the counter via the textarea's `aria-errormessage`. The counter also keeps `role="status"` instead of swapping to `role="alert"` once over-cap, so each keystroke past the cap is not re-announced as a fresh alert. (#584)
