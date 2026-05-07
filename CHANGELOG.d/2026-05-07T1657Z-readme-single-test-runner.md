### Docs

- README "Tests and CI mirror" gains a "Running a single test" sub-section with concrete Go (`go test -run`), Vitest (`pnpm --filter web test <pattern>` and `-t <name>`), and Playwright (`pnpm --filter web run e2e:web -- --grep <spec>`) examples, plus the Playwright-browsers install hint. Closes #812.
