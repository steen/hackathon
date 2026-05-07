---
name: feature-spec
description: Run a one-question-at-a-time exploratory session for a new feature. Codebase grounding → lt scratchpad → iterative Q&A with devil's-advocate framing → full hard cold-pass after every answer → GitHub epic + implementation-ready sub-issues. Invoke when the user asks to "investigate", "explore", "design", "spec", or "plan" a feature, especially when they mention devil's advocate / PM framing, lt as a scratchpad, or want the output as a GitHub epic with sub-issues.
---

# feature-spec

Driven by `/feature-spec <feature description>` or any user request that matches the trigger conditions below. The skill runs in the main thread; the user is in the loop on every question.

## When to use

Signals: "investigate this feature", "play devil's advocate", "ask clarifying questions", "epic with sub-issues at the end", "use lt as a scratchpad", "explore / plan / design before we build".

Do NOT use for: bug fixes, one-off code edits, single-answer questions, anything where the user has already decided what to build.

## Rhythm

### 1. Ground first, ask second

Before a single question, read enough code to know what already exists vs. what's missing. Specific file:line references beat abstract questions every time. If the feature already half-exists, the most useful question is "what's the actual gap?".

Use `Explore` (or direct grep/Read) to map: where the feature surfaces today (handlers, hooks, components, CLI subcommands), what wire shapes are already published (PRD §, types files, schema), what conventions the repo enforces (`CLAUDE.md`, lint rules, wiring patterns).

### 2. Set up the lt scratchpad

Create an `lt` project named after the work surface (e.g. `create-channels`, not `session-2026-XX`). Project names match `[a-z0-9_-]`.

```bash
lt project create <work-surface>
```

File these tickets up front:

| Ticket | Purpose | Lifetime |
|---|---|---|
| **Findings** | What exists today, file:line grounded. Updated when re-reads change the picture. | Permanent reference |
| **Open questions** | Every question identified, before any are answered. Items move to Decision log as resolved. | Permanent reference |
| **Decision log** | Numbered decisions + locked defaults + ACs that emerged from cold-passes. The single source of truth for sub-issue authors. | Cited from every sub-issue body |
| **Cold-pass log** | Append-only log of each cold-pass agent's report, one round per entry. Audit trail and shortcut hint for future rounds (always re-verify). | Append-only |
| **Gaps (per round)** | Optional separate ticket per major gap-discovery round if the cold-pass log gets dense. | Audit trail |

### 3. One question at a time — hard rule

Even when the user has been answering quickly, do not batch. Each question:

- States the decision being made.
- Lists concrete options A/B/C with one-sentence consequences each.
- Gives your recommendation with a one-line reason ("My take: B because...").
- Ends with "(A) or (B)?" — never "thoughts?".

If a user reply contains an answer plus a complaint about the question rate, treat the complaint as the next-round answer about cadence and adjust — but still ask the next question singly.

### 4. Devil's advocate, not theatre

Push back when the user's answer has a real downside. Surface the cost in one sentence ("Risk: in a 5-15 person group with this trust model, rename griefing is 'we yell at Bob in chat'") and move on. Don't fold under social pressure; don't argue past one round either. New information changes minds; rhetoric does not.

### 5. Log every answer

After each user answer, update the **Decision log** ticket:

- Add the resolved item with its number, the chosen option, and any rationale that's not self-evident.
- Update the implications block (e.g. "decision §15 implies a refactor PR before the feature PR").
- Move the item out of **Open questions** if it was tracked there.

The decision log replaces conversation memory. Future agents read it, not the chat. Sub-issue bodies cite it by ticket number, not by copy.

### 6. Full hard cold-pass after EVERY answer — non-negotiable

Spawn the `cold-pass-gap-finder` agent with the lt project name and the current list of relevant code paths. The agent re-reads every lt ticket, walks every layer the feature touches (schema → repo → handler → wiring → middleware → client libs → UI → CLI → tests → docs), pressure-tests every decision (not just the latest), and verifies every code claim by reading the file fresh.

No shortcuts. Quick rounds — one-word answers like "yes" or "B" — are exactly when wrong defaults silently compound. Run the pass anyway.

After receiving the agent's report:

- **Blocking gaps** → surface as the next question (one at a time, per rule §3).
- **Lock-as-default** → pick the obvious default, add it to the Decision log under a "Locked-in defaults" block, and call it out to the user with "flip if wrong".
- **Cross-decision contradictions** → surface immediately to the user; do not silently pick a winner.
- **Stale-claim alerts** → update the decision log to remove or correct the false claim; surface to the user only if it changes scope.
- **Confirmed safe** → record in the Cold-pass log so the audit trail is intact.
- Append the full report verbatim to the **Cold-pass log** ticket.

Keep the **list of relevant code paths** small and current. Add a path when a decision starts touching a new layer. Don't let it balloon to "the whole repo" or the cold-pass turns into a re-read of everything.

### 7. Lock-and-surface defaults

When a small detail emerges that doesn't materially change scope (e.g. "validation hint always-visible vs on-blur"), pick the obvious default, add it to the Decision log under "Locked-in defaults", and explicitly call it out to the user with one line: "Locked: helper text always visible — flip if wrong." Don't burn user attention on details they don't care about.

If the user later flips a default, treat it as a normal answer cycle — log, cold-pass, continue.

### 8. Final output: GitHub epic + sub-issues

When the user signals readiness AND the most recent cold-pass returned no blocking gaps:

1. **Match the repo convention.** Read one existing epic + one existing sub-issue first. Mirror the heading layout (Background / Goal / Approach / Acceptance criteria / Out of scope / Pointers / Parent epic), the labelling (`epic` vs `task`), and the title prefix (e.g. `Phase N — ...`).
2. **Sub-issue granularity** = the smallest thing that's coherent enough to be a single PR off main. Split a feature into "introduce dead code" + "wire it up" if the wiring depends on something else not yet merged. Avoid PR stacking. Per `CLAUDE.md` "Don't stack PRs on open PRs".
3. **Each sub-issue body** carries: pointers (file:line) to where edits go, exact API/wire shapes (regex, JSON envelopes, env-var names), explicit ACs as a checklist, dependencies on other sub-issues by number, and `Parent epic: #N`.
4. **Hand off the decision log by reference**, not by copy. Every sub-issue body cites the lt project + ticket number. Implementers read it; you don't paste it eight times.
5. **Native sub-issue API.** After creating issues, link them under the epic via `gh api graphql` `addSubIssue` mutation — body bullets alone are not authoritative (memory `feedback_sub_issue_api_order_authoritative.md`).
6. **Edit the epic body** to inline the sub-issue numbers and a merge-ordering graph. Don't ship placeholder `#NN` text in the final body.
7. **Verify** by reading back `subIssues` on the epic via GraphQL.
8. Optionally hand off issue authoring to the `feature-issue-drafter` agent — frees main-thread context if there are 6+ sub-issues.

### 9. Anti-patterns

- Asking three questions in one message because "they're related". They're never related enough.
- Deciding scope-shaping things as "defaults" without asking. Big calls (real-time vs polled, schema migration yes/no, web-only vs both clients) are always questions.
- Dropping or downgrading the cold-pass when the rhythm gets fast. Quick answers are when the most rot accumulates.
- Skipping the cold-pass for "obvious" answers. The point of a fresh-context audit is that *your* sense of obvious is the polluted one.
- Drafting the epic from memory instead of from the decision log fresh. The conversation is a lossy summary of the log.
- Letting the relevant-code-paths list balloon. Update it deliberately; prune it when a layer is settled.
- Editing `MEMORY.md`, `.claude/agents/`, `.claude/skills/`, or other standing-rule files mid-flow. Out of scope.
- Closing **Open questions** ticket items when the answer was "skip / out of scope" — leave them open with a "Resolved: deferred to <where>" note so future re-reads don't think the item was forgotten.
- Treating the Cold-pass log as a shortcut. It's a hint, never a substitute. Every round redoes the pass.

### 10. Ending the session

Before declaring done, every one of these is true:

- Decision log has every numbered decision answered or explicitly deferred.
- Open questions ticket has no items still labelled "Open" without a corresponding decision-log entry.
- The most recent cold-pass returned **zero blocking gaps**.
- The most recent cold-pass returned no stale-claim alerts (or each was addressed).
- The user has explicitly approved drafting the epic + sub-issues.
- The relevant-code-paths list has been audited once at the end (the cold-pass agent reads from it; if it's stale, the final pass is unsound).

If any of these is false, the session is not done — name what's missing and continue.

## Cost note

Every answer cycle triggers a full cold-pass. A 12-round session means 12 full cold-passes. That's the deliberate trade: catch drift early at the cost of repeated work. Keep it bounded by maintaining a tight relevant-code-paths list (add deliberately, prune when a layer is settled).
