### Fixed

- Web: at ≤767px viewports the chat header now wraps the connection badge to a second row instead of shoving it past the right edge alongside a long channel title. The desktop space-between layout (≥768px) is unchanged; on phones the heading claims `flex-basis: 100%` (with `min-width: 0` so an unbroken title can shrink) and the badge — keeping its `min-width: 7rem` from #145 so layout stays jitter-free across WS state transitions — falls below. Fixes #632.
