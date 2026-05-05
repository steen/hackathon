### Added

- WebSocket connections now receive a typed `{type:"error", data:{code, message}}`
  frame before the server closes on body-cap and send-rate-limit violations
  (PRD §10, SEC-8). Stable codes: `body_too_large` (precedes close 1009) and
  `rate_limited` (precedes close 1008). Clients can surface a code-specific
  message instead of a bare close code.
