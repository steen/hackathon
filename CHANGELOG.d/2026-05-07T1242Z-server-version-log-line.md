- The chat server emits a structured `server build` info-level slog record
  on startup with `version`, `revision`, `dirty`, `go`, `os`, and `arch`
  attributes (sourced from `hackathon/internal/buildinfo`). Operators can
  now confirm at a glance which build a container is running. Closes #789.
