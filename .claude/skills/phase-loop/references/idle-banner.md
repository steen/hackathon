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
1. **Invent a fresh, detailed ASCII character.** The figure must be a CHARACTER — a humanoid, an animal, a robot, a creature, a monster, a mythological thing, an alien, a ghost, a sentient object given a face and posture. Not a pure inanimate object, not an abstract shape, not a plant alone. Whatever you draw must read as "someone" the user can imagine sighing.
   - **Humanoids:** a figure slumped against a wall with arms hanging, a hooded character looking down, a person hunched over a desk asleep, a clown without their wig, a knight with armor sliding off, a wizard whose hat covers their eyes, a barista staring at an empty espresso machine.
   - **Animals:** a basset hound with ears pooling on the floor, a sloth hanging from one arm by a thread, a cat in a half-collapsed box, a goldfish in a bowl looking out, a pigeon on a wire missing one feather, a bear holding an empty honey jar, a turtle with its head fully retracted.
   - **Robots:** a boxy bot with one antenna bent, a CRT-headed bot whose screen reads `NULL`, a roomba stuck under a chair, a tin-man rusting in light rain, a cute cube-bot with a slow-blinking eye, a service drone tangled in its own cable.
   - **Creatures / monsters:** a tired ghost half-phased through a wall, a slime monster with one eye drooping, a goblin holding a broken sword, a kraken with most tentacles laid flat, a vampire on a sunny park bench, a cyclops squinting because its one eye is dry.
   - **Anthropomorphized things (still characters, with face/posture/voice):** a coffee cup with eyes and a frown sitting in steam, a sad envelope dragging its flap, an exclamation mark lying down with its dot rolled away looking up at the ceiling, a semicolon couple where one half left.
   Each character should have **at least one** of: eyes (open/closed/asymmetric), a mouth (frown/sigh-line/zipper), arms or limbs in a posture (slumped/dangling/tucked), and ideally a small bit of **setting** around them (the wall they lean on, the floor they sit on, the rain falling on them). Characters without expression or posture aren't characters; redraw.
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
- Pure-object banners with no character (a flower in a pot, a cup of coffee on its own, an empty chair). The figure MUST be a character — humanoid, animal, robot, creature, or anthropomorphized thing with a face and posture. Setting + props can surround the character; they cannot replace it.
- Banners over 18 lines or wider than 80 cols (chat will wrap them).
- Naming the user. Passive-aggression is general, not personal.
- Capital letters in the dialogue. The agent is too tired.
