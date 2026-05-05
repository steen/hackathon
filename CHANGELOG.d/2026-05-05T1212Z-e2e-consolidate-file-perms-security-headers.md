### Changed

- **tests/e2e**: Removed
  `tests/e2e/phase-1/file-perms-and-headers/security_headers_test.go`. Its
  three status cases (200 `/debug/subs`, 401 `/api/auth/me`, 404 `/api/nope`)
  are already covered by `headers_on_errors_test.go` in the same package
  (which asserts the four SEC-10 headers across 200/400/401/404/405/413), so
  the standalone AC-2 test was duplicate coverage. The shared `expectedCSP`
  constant and `requireSecHeaders` helper moved into `harness_test.go` so
  the remaining tests (`headers_on_errors_test.go`,
  `headers_on_panic_500_test.go`) keep compiling. The newer
  `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/headers_everywhere_test.go`
  remains unchanged and continues to cover the WS-upgrade and
  panic-recovered paths the older test didn't reach.
  Closes #664. (2026-05-05T12:12Z)
