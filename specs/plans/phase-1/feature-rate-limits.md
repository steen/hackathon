# Feature: Rate limits (per-IP login/register, per-username login backoff)

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** done (PR pending)

## Requirements covered
- (security hardening; defends US-1 register and US-2 login flows from brute force / abuse)

## Acceptance criteria
- Login and register endpoints enforce a per-IP rate limit (e.g., token bucket with burst + steady-state thresholds documented in code).
- Login enforces a per-username backoff: repeated failures for the same username progressively delay subsequent attempts and/or return 429 after a threshold.
- Limits are observable in `auth_events` (rejected attempts logged).
- Limits return HTTP 429 with the user-safe error envelope (see `feature-logging-and-error-envelope.md`).

## Implementation steps
1. Create `apps/server/internal/ratelimit/iplimit.go` with a token-bucket limiter keyed by IP, including a small LRU to bound memory.
2. Create `apps/server/internal/ratelimit/userlimit.go` with a per-username backoff tracker (failures, next-allowed-at).
3. Add HTTP middleware that applies the IP limiter to `/api/login` and `/api/register`.
4. Inside the login handler, consult the per-username tracker before authenticating; on failure, increment.
5. Reset per-username state on successful login.

## Test plan
- `test_ip_rate_limit_blocks_after_burst` — covers SEC-5. Asserts the 11th login attempt within 5 min from one source IP returns HTTP 429 (per PRD §11 SEC-5).
- `test_username_backoff_grows_with_failures` — covers SEC rate-limit goals.
- `test_successful_login_resets_username_backoff` — covers SEC rate-limit goals.
- `test_rate_limited_responses_use_error_envelope` — covers cross-cutting error envelope.

## Files expected to be touched or created
- `apps/server/internal/ratelimit/iplimit.go`
- `apps/server/internal/ratelimit/userlimit.go`
- `apps/server/internal/ratelimit/*_test.go`
- `apps/server/internal/http/middleware_ratelimit.go`

## Risks
- Memory growth from per-IP tracking; mitigated by a bounded LRU and TTL eviction.
