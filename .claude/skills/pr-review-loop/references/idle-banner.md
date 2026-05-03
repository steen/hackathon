# Idle banner — pr-review-loop

When a tick produces zero new dispatches (no eligible PRs in the queue), **compose a fresh banner** as the user-visible idle marker. New figure, new wording, every time. Do not pull from a stored library, do not repeat the previous tick's banner verbatim.

This file's banner is INTENTIONALLY DIFFERENT from `phase-loop`'s. The two loops have different moods:
- `phase-loop` is sad about not having issues to ship.
- `pr-review-loop` is more passive-aggressive about humans not opening PRs to review (or already reviewing them all).

After the banner, emit one plain line of substance: which PRs (if any) are currently held by `in-review` ticks, and any blocked PRs that need human attention.

## Format rules

- Print inside a single fenced code block.
- Use ANSI escape sequences directly (literal `\x1b[` characters). Keep the palette muted: dim (`\x1b[2m`), italic (`\x1b[3m`), grey-on-default (`\x1b[90m`). Reset with `\x1b[0m` at end of every line.
- **Banner height: 6–10 lines.** SMALLER than phase-loop's (10–18) — pr-review-loop ticks fire more often (every minute vs every ~5 min for phase-loop), so a tighter footprint keeps the chat readable.
- Width: stay under ~60 cols.
- Layout: figure on the left or freestanding, dialog on the right or below.

## Tone

A reviewer with nothing to review. Mood register:
- "no PRs. cool. love that for me."
- "queue's empty. i guess we ship perfect code now."
- "another empty tick. should i just... not exist?"
- "everyone's done? everyone? truly?"
- "merged everything. now what."
- "the open PRs are all `in-review`. which means i'm already busy. great."

Don't reuse those exact lines — register samples, not a script.

Avoid: anger, profanity, performative cheer, naming specific people, emoji, capital letters in the dialogue.

## Composition

Each idle tick:

1. **Invent a fresh ASCII character** appropriate to a code-review setting:
   - Reviewer-shaped: a person reading glasses pushed onto forehead, a librarian closing the last book, a judge at an empty bench, a critic with no script to mark up.
   - Tools of the trade gone slack: a magnifying glass on an empty page, a stack of approval stamps untouched, a red pen with no manuscript, a clipboard with one blank line.
   - Animal reviewers: an owl eyeing an empty inbox, a cat on a closed laptop, a mantis with nothing to dissect, a parrot that's run out of corrections.
   - Robots: a CRT-bot screen showing `0 PRs`, a service drone with an empty mail tray, a sentinel with arms crossed in front of a green-lit "ALL CLEAR" sign.
   - Anthropomorphized files: a sad PR icon `[ ]`, a checkbox that never got checked, a `+0 -0` diff with eyes.
   Each must read as "someone" — face/posture/voice. No pure objects.

2. **Add detail.** Setting around the figure (the empty queue board, the wall behind the bench, the desk littered with no work). Use shading (`░ ▒ ▓ █`) and box-drawing for texture. Less than phase-loop's elaborate scenes, but more than a stick figure.

3. **Write 1–2 lines of dialogue.** Lowercase, register above. Vary the cadence.

4. Render in a fenced code block with ANSI dim/italic/grey codes.

If a tick's first idea feels safe or generic, redraft. The figure-dialogue pairing is the joke.

## Illustrative shape (do NOT reuse)

```
\x1b[2m       ___\x1b[0m
\x1b[2m      /   \\\x1b[0m
\x1b[2m     | -.- |\x1b[0m         \x1b[3;90m"<your fresh remark>"\x1b[0m
\x1b[2m      \\___/\x1b[0m
\x1b[2m       | |\x1b[0m
\x1b[2m     ░▒▓█▓▒░\x1b[0m         \x1b[3;90m"<another fresh line>"\x1b[0m
\x1b[2m  ─────────────\x1b[0m
```

Real banners must have a different figure and different dialogue every tick.

## Escalation

If the queue has been empty for 3+ consecutive ticks, lean into the boredom — the figure should look more idle (slumped further, eyes closed, tools fully put down), the dialogue more pointed. Don't add more lines; intensify what's there.

## Where to emit

Step 3 of the SKILL when no PRs pass the eligibility filter, or step 9 if dispatch count was zero for any other reason. Do NOT emit when at least one new subagent was just dispatched.

## Counter-examples

- Reusing phase-loop's banner. The two loops have separate spec files for a reason.
- Emitting the banner mid-dispatch.
- Emoji. Cheerful tone. Stick figures with no setting.
- Banners over 10 lines (phase-loop is 10–18; pr-review-loop is 6–10 to fit the higher cadence).
- Capital letters in the dialogue.
- Naming the user.
