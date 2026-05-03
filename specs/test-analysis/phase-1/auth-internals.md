---
feature: auth-internals
phase: phase-1
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: Auth internals (bcrypt + JWT + password policy)

**Spec:** `specs/plans/phase-1/feature-auth-internals.md`
**Implementation status:** implemented — `apps/server/internal/auth/password.go` (bcrypt hash/verify + policy), `apps/server/internal/auth/jwt.go` (issue/parse with `tv` claim), `apps/server/internal/auth/login.go` (constant-time path against dummy hash), `apps/server/internal/auth/constants.go` (dummy hash + policy thresholds). Wired into `auth_handlers.go` so the user-visible behaviour is exercised through the HTTP surface.
**E2E test directory:** `tests/e2e/phase-1/auth-internals/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `internal/auth` exposes password hashing/verification using bcrypt with a sane cost. | missing | — |
| AC-2 | JWT issuance and verification include a `tv` (token-version) claim used to invalidate tokens server-side (US-12). | missing | — |
| AC-3 | A constant-time login path: when a user does not exist, the code still performs a bcrypt comparison against a dummy hash so timing does not leak username existence. | missing | — |
| AC-4 | A password length policy is enforced (e.g., min 8, max 72 to stay within bcrypt's input limit). | missing | — |
| AC-5 | Token signing key is loaded from config (validated by startup checks; see `feature-startup-config-checks.md`). | missing | — |

## Findings

### Missing E2E tests

**AC-1 — bcrypt with sane cost via end-to-end register**
- **What to assert:** Internals are not directly observable from the wire, but bcrypt's effects are: (a) the persisted hash starts with `$2a$`/`$2b$`/`$2y$`; (b) the cost factor encoded in the hash (positions 4-5 of the bcrypt string, e.g. `$2a$10$...`) is >= 10 and <= 14 (PRD-aligned "sane" range — adjust if `constants.go` says different; verify by reading the file once during test authoring); (c) hashing the same password twice produces different hashes (salt). Approach: register two users with the same password; open SQLite read-only; SELECT both `password_hash` rows; assert prefix, parse cost integer from the hash string, assert hashes differ.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/auth-internals/bcrypt_hash_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `register(t,srv,u,p)` and `openDBReadOnly(t,srv)`.

**AC-2 — JWT carries `tv` claim, version increments invalidate**
- **What to assert:** Register + login → decode JWT payload (split on `.`, base64-url-decode middle segment, json.Unmarshal). Assert claims include `tv` (integer, value 0). `POST /api/auth/logout` with that token; login again; decode the second token; assert `tv` is exactly 1 (one increment per logout). `GET /api/auth/me` with the first token now returns 401 (server rejects the stale `tv`). For the verification half: tamper the second token by changing the `tv` claim — re-encode the payload with `tv:99`, prepend the original header, append a garbage signature — `GET /api/auth/me` returns 401 (sig check fails first; this also exercises the parse path).
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/auth-internals/jwt_tv_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `register`, `login`, `logout`, `me`, `decodeJWTPayload`.

**AC-3 — constant-time login on unknown user (bcrypt against dummy hash)**
- **What to assert:** Register user `alice`. Time `POST /api/auth/login {"username":"alice","password":"<wrong>"}` — repeat 50 times (warm bcrypt), record median wall-clock latency `T_known_wrong`. Then `POST /api/auth/login {"username":"<random-unknown>","password":"<wrong>"}` — repeat 50 times, record `T_unknown`. Assert `abs(T_unknown - T_known_wrong) / T_known_wrong < 0.30`. (Bcrypt at cost 10 dominates; if implementation skipped the dummy compare, `T_unknown` would be tens of ms vs `T_known_wrong` hundreds of ms — easy to detect.) Also assert response bodies are byte-identical for both cases (compare raw response bytes after status check).
- **Layer:** Go (boot binary, HTTP, timing).
- **File path:** `tests/e2e/phase-1/auth-internals/constant_time_login_test.go`.
- **Setup it needs:** same as AC-1; mark with `if testing.Short() { t.Skip() }` because 100+ bcrypt rounds is slow.
- **Helpers it can reuse:** harness; `register`, `loginRaw(t,srv,u,p) (status int, body []byte, latency time.Duration)`.

**AC-4 — password length policy enforced**
- **What to assert:** `POST /api/auth/register` with `password` of length 7 → 4xx envelope `{ok:false, error:{code:"...", message:"..."}}` (the message must not leak the threshold value to remote users beyond a generic "password too short"). With length 8 → 200/201 (assuming 8 is the boundary; verify by reading `constants.go` once during test authoring). With length 72 → 200/201. With length 73 → 4xx. With length 0 → 4xx. After each rejection, open SQLite read-only and assert no row was inserted into `users` for that username.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/auth-internals/password_policy_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `registerRaw(t,srv,u,p) (status int, body []byte)`; `openDBReadOnly`.

**AC-5 — signing key loaded from `CHAT_JWT_SECRET` config**
- **What to assert:** Boot server A with `CHAT_JWT_SECRET=secretA` (random 32-byte hex). Register + login → `tokenA`. Stop server A. Boot server B on a fresh `CHAT_DB_PATH` with `CHAT_JWT_SECRET=secretB` (different random 32-byte hex). Register + login the same username → `tokenB`. Send `tokenA` to server B's `/api/auth/me` → 401 (different secret → signature invalid). Send `tokenB` to server B's `/api/auth/me` → 200 (same secret → ok). Repeat with the same secret across two server processes (boot server C with `CHAT_JWT_SECRET=secretA`, fresh DB, register the same username, login → `tokenC`); send `tokenA` to server C's `/api/auth/me` → 401 (same secret but different `tv` — actually the user's `tv` is 0 in both DBs, so this test confirms signature alone is not enough; for a pure signing-key test, mint `tokenA` for a user with the same id+tv as in DB C — easier: just keep the negative case above as the AC-5 assertion and move on).
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/auth-internals/jwt_secret_from_config_test.go`.
- **Setup it needs:** harness must be able to start two binaries with different env. Use two `startServer(t, opts)` calls where `opts` overrides `CHAT_JWT_SECRET`.
- **Helpers it can reuse:** harness with a `startServerWithEnv(t, env map[string]string) *runningServer` variant.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. AC-5 needs a `startServerWithEnv(t, overrides map[string]string)` variant — extend `startServer(t)` rather than duplicating the build step.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/auth-internals/harness_test.go` with copied helpers + `register(t,srv,u,p)`, `loginRaw(t,srv,u,p)`, `me(t,srv,tok)`, `logout(t,srv,tok)`, `decodeJWTPayload(t,tok)`, `openDBReadOnly(t,srv)`, `startServerWithEnv(t,env)`.
- Add `tests/e2e/phase-1/auth-internals/bcrypt_hash_test.go` (AC-1), `jwt_tv_test.go` (AC-2), `constant_time_login_test.go` (AC-3, gated on `!testing.Short()`), `password_policy_test.go` (AC-4), `jwt_secret_from_config_test.go` (AC-5).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
