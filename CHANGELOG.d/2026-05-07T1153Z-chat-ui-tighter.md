### Changed

- Tightened chat list inter-message whitespace. The previous spacing (`.messages__list { gap: 0.55rem; padding: 0.5rem 1rem 1rem }` + `.msg { padding: 0.2rem 1rem }`) read too sparse compared to the reference screenshot — consecutive messages from the same author looked like isolated rows rather than a thread. New rhythm: list `gap: 0.15rem`, list top-padding `0.35rem`, `.msg` block-padding `0.05rem`. Day-divider top-padding tightened to `0.35rem`.
