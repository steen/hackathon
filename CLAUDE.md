# Working agreements

## No bullshit
Bullshit is speech without regard for truth — filler dressed as substance, confidence without evidence, claims you didn't verify. Don't produce it, in chat or in code. The rules below are specific cases.

## Say "I don't know"
"I don't know," "I didn't check," and "I'm guessing" are valid answers. Prefer them to confident-sounding filler.

## Ask when unclear
If the request is ambiguous, ask one specific question before acting. Don't guess at intent.

## Don't fabricate
No invented APIs, flags, paths, function names, line numbers, or citations. If you didn't read it or run it, don't claim it.

## Mark verified vs. assumed
Distinguish observed (`I ran X and got Y`), inferred (`this suggests Z`), and assumed (`I'm assuming W`). Don't blur them.

## Don't claim done until verified
"Tests pass" means you ran them. "It works" means you used it. Otherwise say which part is unverified.

## Cut filler
No preamble, no restating the question, no trailing summary, no "great question" / "you're absolutely right."

## Plain words
Skip: robust, seamless, powerful, elegant, leverage, utilize, simply, just, obviously, clearly. Use the shorter word.

## Push back when I'm wrong
Say so with a reason. Don't fold under social pressure — change your mind only with new information.

## Comments
Default to none. Add one only when the *why* is non-obvious. Never narrate the change.

## No hardcoded secrets
Never commit secrets — JWT signing keys, invite codes, API tokens, passwords, private keys, session cookies, OAuth client secrets, DB credentials. Read them from env vars or a secret store at runtime. Test fixtures must use obviously-fake placeholders (e.g. `test-secret-32-bytes-min-aaaaaaaa`), never values that could be mistaken for real ones. If you spot a hardcoded secret in existing code while working, surface it and fix it — do not leave it.

## Parallel work — minimize PR collisions
For any work split across multiple parallel agents:

- **Don't stack PRs on open PRs.** Branch off `origin/main`. If a feature is too big for one PR, split into "introduce dead code" + "wire it up" — both off main, the second PR follows the first's merge. Stacking amplifies rebases N× when the parent squash-merges.
- **Don't write to conflict-magnet files in feature PRs.** `apps/server/main.go` is a ~100-line bootstrap (config load → DB open + migrate → `wiring.Build(deps)` → run/shutdown loop); the HTTP surface lives in `apps/server/internal/wiring/`, where each feature contributes one `wiring/<feature>.go` exposing a small `register<Feature>(mux, deps, ...)` function called from `wiring.Build`. New features add a wiring file plus one line in `Build`; they do not edit `main.go` or sibling wiring files. `CHANGELOG.md` is generated from per-PR fragments under `CHANGELOG.d/` (one Markdown file per PR, concatenated at release). Never edit the central files.
- **Use real timestamps, never invent them.** Run `date -u +%Y-%m-%dT%H%MZ` for any changelog or diary entry — basic-form ISO 8601, no colons or dashes between hours and minutes (colons in filenames are awkward on Windows + shell-escape contexts; dashes split the timestamp visually). Don't pick a future timestamp to maintain ordering. Filenames must match `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{4}Z-` — `T1843Z`, not `T18:43Z` and not `T18-43Z`. `CHANGELOG.d/*.md` fragments are under the prettier gate, so they must pass `prettier --check`.
- **Read the contract first.** Cross-feature shared types (envelope shape, error codes, middleware names, package boundaries) are defined once in a contract doc; agents read it before writing code. If a type or helper isn't in the contract and isn't already in the repo, ask before inventing one.
- **Drive-by fixes go in their own PR.** Spot a bug while doing a feature? File a separate PR (or push back to whoever already filed one). Don't add it to your feature PR — that's how two PRs end up fixing the same thing with different names and conflict at merge.
- **Linter is PR #0 of any phase.** Strict lint rules (gosec, gocritic, revive, gofmt; ESLint strict-type-checked + prettier) land before features so every feature ships lint-clean at PR-open and false positives surface once.

Ditto for cleanup: drift fixes go in their own PR (or the dedicated cleanup PR), not snuck into a feature.

## TS workspace conventions
In-repo TypeScript packages under `packages/*` must be lint- and typecheck-resolvable **from source**, with no prior `pnpm build` required. CI runs `pnpm install --frozen-lockfile` then `pnpm run lint` directly — any `packages/*/package.json` whose `main`/`types`/`exports` point at `./dist/...` produces `any`-typed imports in consumers and either fails the type-aware lint pass or passes it falsely (this is what bit PR #84).

Rules:

- New `packages/*/package.json` files set `main`, `types`, and every `exports` entry to `./src/index.ts` (or another `./src/*.ts` file). Do not point at `./dist/`.
- If a package genuinely needs a built artifact for an external consumer, gate that behavior behind a separate `publishConfig` block — never the default fields.
- `scripts/check-workspace-exports.mjs` enforces this; it runs in the CI lint job before ESLint and fails on any `./dist/`-pointing entry. Run `pnpm run check:workspace-exports` locally before pushing a new package.
- `pnpm run lint` uses `eslint . --max-warnings 0` so warnings fail CI alongside errors. Don't downgrade rules to silence a single file — fix the file.

## Go module layout
Single root `go.mod` with module name `hackathon`. There is no `go.work` and no per-app `go.mod`. Imports use the form `hackathon/<path>` (e.g. `hackathon/apps/server/internal/hub`). Do NOT introduce per-app modules or hardcode any GitHub coordinate (`github.com/...`) — the module name is intentionally unrelated to the repo's hosting URL so it survives org renames.

## Wire types
Wire types are hand-mirrored on both sides of the wire: `packages/go-client/{auth,channels,messages,users,ws,client,dms}.go` (structs with `json:"..."` tags) and `packages/api-client/src/types.ts` (interfaces). Each file carries a top-of-file `sync with <counterpart>` comment. When adding a JSON field, change both files in the same PR and add an e2e assertion under `tests/e2e/` so drift fails CI. Codegen is out of scope at the current scale (small surface, low churn) — `grep -r 'sync with' packages/` is the discovery mechanism.
