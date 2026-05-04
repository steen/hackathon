### Web

- Keyboard focus is reliably visible across the web app: every interactive element (buttons, links, inputs, textareas, selects, `[tabindex]`) now gets an explicit accent-colored `:focus-visible` ring instead of relying on UA defaults. Accent-filled controls (active-channel button, composer Send, auth submit) get a contrasting double-ring that stays visible against both the blue fill and the surrounding surface. Mouse `:focus` no longer paints a ring — keyboard focus only. (#427)
