### Docs

- **tests/e2e**: Reworded the header-comment block in
  `tests/e2e/phase-1/file-perms-and-headers/headers_on_errors_test.go` to
  drop a stale `(security_headers_test.go)` parenthetical. PR #684 deleted
  that sibling file (its 200/401/404 cases were a strict subset of the
  AC-3 matrix in this file), so the reference no longer resolves. The
  comment still names AC-2 as the upstream pin, and the file's lines 1-3
  already cite `specs/plans/phase-1/feature-file-perms-and-headers.md`
  for the spec source-of-truth. No test logic or behavior changed.
  Closes #686. (2026-05-05T12:35Z)
