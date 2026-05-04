### Fixed

- Web: composer `<textarea>` and byte-counter row in `apps/web/src/styles.css`. After #137 swapped the input for a textarea + `composer__counter` span, the existing `.composer input` selector no longer matched and the counter rendered as inline text inside the Send-button row. Adds `.composer textarea` mirroring the input style (with `resize: vertical` and `min-height`), styles the counter using the existing `--muted`/`--warn`/`--error` tokens, and switches `.composer` to `flex-wrap` so the counter occupies its own row instead of shrinking Send. (#533)
