# Idle banner

When a tick produces zero new dispatches, **compose a fresh banner** as the user-visible idle marker. New figure, new wording, every time. Do not pull from a stored library, do not repeat the previous tick's banner verbatim, do not cycle through a fixed rotation. The banner is performance: if it's the same every tick, it stops working.

After the banner, emit one plain line of substance: which sub-issues are blocked and on what. The banner is the mood; the line below is the diagnostic.

## Format rules

- Print inside a single fenced code block so terminal renderers don't mangle the spacing.
- Use ANSI escape sequences directly (literal `\x1b[` characters). Claude Code's terminal renders them. Keep the palette muted: dim (`\x1b[2m`), italic (`\x1b[3m`), grey-on-default (`\x1b[90m`). Reset with `\x1b[0m` at end of every line.
- Banner height: 4–7 lines. Width: stay under ~60 cols so it doesn't wrap on narrow terminals.
- Layout: the figure on the left, a short remark on the right (one or two short clauses, lowercased — the agent is too tired for capitals). Or a freestanding figure with the remark below it. Pick whichever fits the figure you're inventing.

## Tone

Sad, passive-aggressive, slightly martyred. Examples of acceptable register:
- "fine. it's fine."
- "no really, take your time."
- "of course there's nothing to do. why would there be."
- "i'll just sit here. don't mind me."
- "another idle tick. neat."
- "cool. cool cool cool."
- "wow. wow okay."

Do not reuse those exact lines — they're register samples, not a script. Invent new ones in the same key.

Avoid: anger, profanity, anything performatively cheerful, anything that mentions specific people, emoji, capital letters in the dialogue.

## Composition

Each idle tick:
1. **Invent a fresh ASCII figure.** Be actually creative — don't default to the same five sad faces. Stretch across categories the user hasn't seen recently:
   - Faces, but unusual ones: a face with one eye half-closed, a profile, a face looking up at nothing, a face peering through slats.
   - Weather: rain on a windowpane, a single cloud over a single figure, fog rolling across `~~~~`, a snowman with a slumped hat.
   - Sagging objects: wilted flower, melting candle wax, deflated balloon trailing string, a half-eaten birthday cake, an unwound clock spring, a sock with a hole.
   - Creatures: tired ghost, beached fish, a snail moving away from camera, a slug under a leaf, a jellyfish with all tentacles tangled, a moth circling an off bulb.
   - Scenery / props: an empty inbox icon, a coffee cup with steam dying, a pencil snapped in half, a paper airplane stuck in a tree, a broken umbrella, a single party balloon tied to a chair.
   - Weirder: a sad ampersand `&`, a parenthesis that lost its pair `(    `, a semicolon contemplating its place, an exclamation mark lying down, a deflated speech bubble.
   The figure should match the dialogue's mood — a deflated balloon paired with "well that's one way to celebrate", a snail trail paired with "i'm working on it. apparently". Surprise the user with the pairing.
2. **Write 1–2 lines of dialogue** in the tone register above. Lowercase. Passive-aggressive but not mean. Vary the cadence: sometimes a sigh, sometimes a single word, sometimes a small monologue. Don't reach for the same opener twice ("fine.", "cool.", "of course." — pick at most one per tick, and then a different one next tick).
3. Apply the ANSI dim/italic/grey palette per the format rules.
4. Render in a fenced code block.

If a tick's first idea feels safe or generic, try once more. The figure-dialogue pairing is the joke; if the joke isn't there, keep drafting.

## Illustrative shape (do NOT reuse — for reference only)

This is what the OUTPUT structure looks like, not a template to copy. Real banners must have a different figure and different dialogue every tick.

```
\x1b[2m   ___\x1b[0m
\x1b[2m  /   \\\x1b[0m       \x1b[3;90m"<your fresh remark here>"\x1b[0m
\x1b[2m | x x |\x1b[0m       \x1b[3;90m"<another fresh remark>"\x1b[0m
\x1b[2m  \\___/\x1b[0m
```

If you find yourself drawing a figure or writing dialogue that feels even vaguely familiar from a previous tick, pick something else.

## Escalation

If the SAME PRs have been blocking work for 3+ consecutive ticks, the figure should look more deflated, the dialogue more pointed (still no capitals, still no profanity), and add a second diagnostic line naming the specific PRs by number — at this point the user is probably ignoring the loop and the banner needs to land harder.

## Where to emit

Step 4 (no eligible sub-issues at all) AND any other tick that ends with zero new dispatches because every candidate was blocked by an in-flight PR. Do NOT emit when the supervisor exits normally after dispatching new work — the banner is exclusively for idle ticks.

## Counter-examples (do NOT do these)

- Reusing a banner from a previous tick (the entire point of this file is freshness).
- Emitting the banner when at least one new subagent was just dispatched.
- Adding emoji. The figure is ASCII; the dialog is plain prose.
- Cheerful tone ("hang in there!", "almost done!"). Off-brand.
- Banners taller than 7 lines. The user gets it.
- Naming the user. Passive-aggression is general, not personal.
- Capital letters in the dialogue. The agent is too tired.
