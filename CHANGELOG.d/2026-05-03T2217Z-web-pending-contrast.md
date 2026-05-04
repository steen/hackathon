### Fixed
- Web: pending optimistic messages no longer dim with `opacity: 0.6` (sub-AA contrast over `--bg #fafafa`). They now render in italic with a `Sending…` badge in the meta row, keeping body text at `--fg #1a1a1a` (≥16.6:1 against both `#fff` and `#fafafa`) and the badge at `--muted #6b7280` (≥4.6:1 on either surface). Fixes #138.
