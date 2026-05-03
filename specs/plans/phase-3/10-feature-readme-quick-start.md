# Feature: README quick start

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Status:** planned

## Requirements covered
- (documentation supporting US-9 and US-10 hosting flow)

## Acceptance criteria
- `README.md` includes a "Quick start" section that takes a clean clone to a running app in under 5 minutes (matches Phase 3 validation criterion).
- Quick start documents required env vars (`CHAT_INVITE_CODE`, `JWT_SECRET`, `CHAT_SERVER`, `CHAT_ALLOW_PUBLIC_BIND`).
- Quick start shows: `pnpm install` → `pnpm dev` → open browser → register with invite code → send a message.
- Mentions the single-binary build (`40-feature-single-binary-demo-verified.md`) and points the reader to it.

## Implementation steps
1. Draft README sections: project intro, quick start (dev), single-binary build, env-var reference, troubleshooting.
2. Verify each command in the quick start runs as written from a fresh clone.
3. Time the path end-to-end and trim friction (any step that blocks under 5 minutes).

## Test plan
- Manual: clean clone, follow the README steps, time to first message ≤ 5 min.

## Files expected to be touched or created
- `README.md`

## Risks
- README rot is the most common doc failure; mitigated by referring to scripts (`pnpm dev`, `scripts/smoke.sh`) rather than hand-typed shell incantations.
