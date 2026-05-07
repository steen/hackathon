### Fixed

- chat-ui tests: replaced no-op `screen.queryByRole("time")` assertion in `MessageItem.test.tsx` (pending state suppresses timestamp) with `container.querySelector("time")`. `time` is not an ARIA role, so the prior assertion always returned `null` regardless of DOM contents and could not catch a regression. Closes #838.
