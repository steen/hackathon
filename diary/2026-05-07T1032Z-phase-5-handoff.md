# Phase 5 handoff

Date: 2026-05-07 10:32Z
Author: Claude (orchestrator session for phase-5 close-out)
Predecessor: `2026-05-07T0856Z-phase-4-handoff.md`

## Executive summary

**Phase 5 (`#764`) drained.** All three actionable sub-issues landed; one upstream-blocked follow-up filed during the work was closed `not planned`; one investigated standing concern was deliberately not filed. The phase was small and finite by design — meta follow-ups (agent runtime, supervisor hygiene, reviewer process) coming out of Phase 4's "Standing concerns" list, not feature work.

The three deliverables, all merged the same morning:

1. **`#765` / PR #784** — Adversarial test of Option A's per-worktree `settings.local.json` deny rules. New `.claude/scripts/test-option-a-enforcement.sh` exercises all three hypothesized failure modes from the Phase 4 diary: (a) script-not-firing guard (bad-arg → non-zero exit), (b) substitution correctness (rendered JSON valid, `__PARENT_ABS__` fully replaced, parent path in every rule), (c) live harness enforcement (Edit + Write hard-rejected via structured `permission_denials[]` parsing — not prose, which had false-greened on model safety self-refusal during development). MultiEdit live-fire is skipped because claude CLI 2.1.118 doesn't expose the tool in `system.init.tools[]`; the deny rule's structural presence is validated by step (b). That gap was filed as #783 and closed `not planned` once we confirmed the trigger condition is upstream-controlled — see "What did not land" below.
2. **`#766` / PR #781** — `.claude/scripts/cleanup-stale-worktrees.sh`, dry-run by default, with `--execute` to actually remove. Reads `git worktree list --porcelain` (never the human-readable form, never `rm -rf`), uses `git worktree remove --force` (single `--force` only — the dirty-worktree gate is the safety net, not `--force --force`). Skips in-flight agents by checking lock-PID liveness with `kill -0`. Surfaces (rather than removes) dirty worktrees and worktrees on named branches without a merged PR. The first dry-run on the live repo classified the ~50 stale entries as: 39 eligible, 1 dirty, 1 in-flight sibling agent, 1 self-skip, 13 open/unknown.
3. **`#767` / PR #782** — Deferred-unskip guard inserted into `.claude/agents/pr-reviewer.md` §3 (between the existing `Tests:` and `Security:` bullets). Triggers when a PR or its linked issue mentions `unskip` / `.skip` / `t.Skip` / `it.skip` / `describe.skip` and the PR doesn't itself remove the markers. The reviewer agent must locally remove the skip, run the named test, paste output into the review under "Deferred-unskip smoke-check", and classify a failing test as a hard blocker. Closes the post-mortem on PR #709 → PR #749 from Phase 4 (~30 hours of skipped AC-2 against a non-matching CHANGELOG form). Approval recorded as a comment on issue #767 before the file edit per the standing `.claude/agents/` rule.

The Phase 5 phase-loop ran end-to-end via the supervisor's auto-fired `/pr-review-loop`: workers landed PRs, reviewers reviewed and merged, no escalations.

## What landed by area

| Sub-issue | PR | Substance |
|-----------|----|-----------|
| #765 | #784 | `.claude/scripts/test-option-a-enforcement.sh` — adversarial deny-rule test; structured `permission_denials[]` parsing |
| #766 | #781 | `.claude/scripts/cleanup-stale-worktrees.sh` — dry-run by default, `--execute` to perform; safety contract spelled out in code + README |
| #767 | #782 | `pr-reviewer.md` §3 deferred-unskip guard bullet |

Total: 3 PRs, +470/−10 lines (estimate from individual PR diffs), all four CI jobs green at merge time.

## What did not land

- **#783 — re-enable MultiEdit live-fire on CLI upgrade.** Filed by the #765 worker as a follow-up because the current `claude` CLI release doesn't expose `MultiEdit` in `system.init.tools[]`. Verified the trigger condition (upstream CLI exposing the tool) is still false; closed `not planned`. Reopen criteria spelled out in the close comment: either upstream adds the tool, or someone rescopes step 5 to auto-detect tool availability (different scope than the original issue). The deny rule for `MultiEdit(...)` IS structurally validated in the test today — just not live-fired.

## De-scoped (investigated, not filed)

- **Dispatch-template RULE 0 dedup re-add** (Phase 4 standing concern #4). The Phase 4 diary suggested re-adding the duplicate RULE 0 block to dispatch templates "if leak rates climb." Investigation found the cold-start argument is weak — the dispatch prompt already tells the agent to read its definition before any tool call, and the supervisor's existing `PARENT_STATUS` check + agent §0 self-check produce a detectable leak signal. Not filing now; explicit re-trigger documented in the Phase 5 epic body: ≥2 leak events in Phase 5 sessions where (a) the agent's §0 report shows no mention of RULE 0 / worktree path checking AND (b) the leak occurred before the agent loaded its definition. The Option A enforcement test (#765) provides a stronger structural fix and is the better place to invest.

## What this phase confirmed about agent infrastructure

The Option A structural layer (per-worktree `settings.local.json` deny rules + `sandbox.enabled: true`, materialized by `write-agent-worktree-settings.sh` as the agent's first tool call) is **proven to fire** for `Edit` and `Write` against parent-rooted paths. Today's session ran ~7 worker/reviewer dispatches across Phases 5 + 6 + 7 work; zero parent-checkout leaks observed. The post-Option-A leak the Phase 4 diary cited was either a transient or has been further fixed by PRs #759 + #763 (deny-list expansion + MultiEdit symmetry). MultiEdit enforcement remains structurally-only-validated, blocked on upstream.

## Standing concerns for the next session

1. **Phase 6 (chat UI extraction) is in flight.** Epic #768, ten sub-issues #769–#778. Out of scope for Phase 5; flagged here because the supervisor's working tree carried Phase 6 WIP throughout the Phase 5 close-out. Phase 6 work continues on its own cadence.
2. **Phase 7 (deployment readiness) was scoped + posted during this session.** Epic #786, seven active sub-issues #787–#792 + #795 (with #793 + #794 closed as superseded into #792's unified ops doc). Two related follow-ups outside Phase 7 scope: #785 (`CHAT_BCRYPT_COST` env-wire — PRD §9 deviation) and #796 (`--health-probe` flag for the chat-server binary, prerequisite for adding real `HEALTHCHECK` to the Dockerfile/compose once landed). Phase 7 is sized for hackathon/homelab — single-host docker-compose deploy, no registry, no K8s, no metrics, no in-binary TLS. Ready for the `/phase-loop` worker.
3. **The pr-review-loop auto cadence** (270s wakeup, scheduled in this session) was stopped on user direction once the Phase 5 queue drained. Restart with `/pr-review-loop auto` when the next batch of PRs needs reviewers.

## Numbers

- Sub-issues opened in Phase 5: 4 (3 actionable + 1 follow-up filed during work)
- Sub-issues closed in Phase 5: 4 (3 merged, 1 `not planned` for upstream block)
- PRs merged in Phase 5: 3 (#781, #782, #784)
- Standing-rule-edit approvals: 1 (`pr-reviewer.md` deferred-unskip-guard bullet, approval recorded on #767)
- Worker / reviewer dispatches this session: ~7 (3 issue-pr-workers + 3 pr-reviewers + 1 re-dispatched issue-pr-worker after approval gate cleared)
- Stale agent worktrees observed at session start (motivated #766): ~50–55 (`git worktree list` showed 54 entries on first inventory)
