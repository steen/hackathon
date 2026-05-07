- Extracted CLI build-identity rendering into `hackathon/internal/buildinfo`,
  exposing `Info`, `Read`, `ReadWith`, `(Info).FormatLine`, and
  `(Info).LogAttrs` for reuse by both `apps/cli` and `apps/server`. The
  CLI's `chatd version` output is unchanged. Refs #789.
