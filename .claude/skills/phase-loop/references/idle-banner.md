# Idle banner

When a tick produces zero new dispatches, **compose a fresh banner** as the user-visible idle marker. New figure, new wording, every time. Do not pull from a stored library, do not repeat the previous tick's banner verbatim, do not cycle through a fixed rotation. The banner is performance: if it's the same every tick, it stops working.

After the banner, emit one plain line of substance: which sub-issues are blocked and on what. The banner is the mood; the line below is the diagnostic.

## Format rules

- Print inside a single fenced code block so terminal renderers don't mangle the spacing.
- Use ANSI escape sequences directly (literal `\x1b[` characters). Claude Code's terminal renders them. Keep the palette muted: dim (`\x1b[2m`), italic (`\x1b[3m`), grey-on-default (`\x1b[90m`). Reset with `\x1b[0m` at end of every line.
- **Banner height: 10–18 lines. Width: 60–80 cols.** A four-line stick figure is not enough — the figure should have weight on the page. Use shading characters (`░ ▒ ▓ █`), box-drawing (`─ │ ┌ ┐ └ ┘ ┬ ┴ ├ ┤ ┼`), and varied texture (`. , ; : ' " ~ - = + * #`) to give the figure depth. Multiple visual elements when it makes sense — figure + setting (e.g. a sad creature plus the floor it's slumped against), figure + weather, figure + props.
- Layout: figure dominant, with a short remark integrated into or below the artwork. Lowercase, the agent is too tired for capitals. The dialogue can be one line, two short lines, or — for more elaborate figures — a small monologue framed in the negative space of the art.

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
1. **Invent a fresh, detailed ASCII figure.** Be actually creative — don't default to the same five sad faces. Stretch across categories the user hasn't seen recently:
   - Faces, but unusual ones: a face with one eye half-closed, a profile, a face looking up at nothing, a face peering through slats — drawn with enough volume to read as a face from across the room.
   - Weather: rain on a windowpane (with the windowpane itself), a single cloud over a single figure, fog rolling across in layers of `~~~`, a snowman whose hat has slumped over.
   - Sagging objects: a wilted flower with stem and pot and a leaf or two, a melting candle pooling wax on its holder, a deflated balloon trailing string across the ground, a half-eaten birthday cake with one missing slice and crumbs, an unwound clock spring puddled on a desk, a sock with a hole and a heel and a pile of laundry behind it.
   - Creatures: a tired ghost passing through a wall section, a beached fish with sand and a tide line, a snail with shell-coil detail moving away from a starting point, a jellyfish with multiple tangled tentacles, a moth orbiting an off bulb in a dark frame.
   - Scenery / props: an empty inbox shown as a labeled box with cobwebs, a coffee cup whose steam is reduced to a few `'` flecks above a still mug, a pencil snapped at the middle with shavings, a paper airplane lodged in tree branches with leaves, a broken umbrella with bent ribs in the rain, a single party balloon tied to an empty chair.
   - Weirder: a sad ampersand `&` with droopy curves, a parenthesis whose pair has left town `(   ····· `, a semicolon contemplating its place between code blocks, an exclamation mark lying down with its dot rolled away, a deflated speech bubble with the text drained out.
   The figure should match the dialogue's mood — a deflated balloon paired with "well that's one way to celebrate", a snail trail paired with "i'm working on it. apparently". Surprise the user with the pairing.
2. **Add detail.** Multiple lines of texture or shading. A creature plus the floor it slumps on. A flower plus the pot, the soil, maybe a wilting leaf at the side. Use shading (`░ ▒ ▓ █`), box-drawing (`─ │ ┌ ┐ └ ┘`), and small texture marks (`,` `.` `'` `~` `*`) to give the figure mass. The user asked for detail; deliver volume.
3. **Write the dialogue** in the tone register above. Lowercase. Passive-aggressive but not mean. Vary the cadence — sometimes a sigh, sometimes a single word, sometimes a small monologue framed in the figure's negative space. Don't reach for the same opener twice ("fine.", "cool.", "of course." — pick at most one per tick, and then a different one next tick).
4. Apply the ANSI dim/italic/grey palette per the format rules.
5. Render in a fenced code block.

If a tick's first idea feels safe, generic, or thin, try once more. The figure-dialogue pairing is the joke; if the joke isn't there or the figure looks lazy, keep drafting.

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
- Tiny stick-figure-only banners with no setting, texture, or shading. Under 10 lines is too thin — the user has explicitly asked for detail.
- Banners over 18 lines or wider than 80 cols (chat will wrap them).
- Naming the user. Passive-aggression is general, not personal.
- Capital letters in the dialogue. The agent is too tired.
