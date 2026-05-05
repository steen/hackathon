### Docs

- `apps/server/internal/wsapi`, `apps/cli/cmd`: document the _why_ behind five `time.Sleep` call sites in tests — the 50ms floor in `TestHandlerDoesNotRebroadcastInboundFrames` (security-boundary test for raw-rebroadcast prevention), and the 10ms polls inside `waitForSubscribers`, `waitForPresenceCount`, and the two inline subscribe/output poll loops in `cli_test.go`. Comment-only; zero behaviour change. Closes #607.
