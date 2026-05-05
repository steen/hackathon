### Documentation

- web: document the `setClientForTesting` cleanup contract in `apps/web/src/api.ts`. The helper only swaps the cached pointer and never invokes a cleanup hook on the outgoing Client; tests that install a custom Client with mutable state are responsible for resetting that state between cases. No behaviour change. (#659)
