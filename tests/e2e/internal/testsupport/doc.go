// Package testsupport holds shared scaffolding for the phase-1
// black-box E2E test packages under tests/e2e/phase-1/<feature>/.
//
// Every phase-1 feature dir grew a near-identical harness_test.go with
// the same repoRoot/freePort/waitForPort/randomSecret/startServer/
// postJSON/register/mintTicket helpers. With five copies in tree the
// drift risk passed the "fix once, propagate everywhere" threshold —
// see issue #310. This package collects the scaffolding once.
//
// Migration plan (per #310): this PR introduces the package only;
// per-feature harness migration follows in separate PRs, one feature
// dir at a time, so existing tests don't all rebase at once.
//
// Conventions:
//   - Exported names (callers live in different packages).
//   - Primitive parameters (httpURL, dbPath, ...) rather than a typed
//     `*Server` struct where it forces a caller to rename its harness
//     — same call out as the clihelp package next door.
//   - No imports from apps/** internal packages: keep the black-box
//     boundary that motivates these tests.
//   - Helpers that may grow per-caller variation take a trailing
//     variadic options struct (e.g. `Register(... opts ...RegisterOptions)`)
//     so the no-options call stays backward-compatible while migrating
//     harnesses can opt into extra request fields without forking a
//     parallel `RegisterWith…` helper. See StartServer/StartOptions for
//     the same shape.
package testsupport
