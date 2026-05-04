package goclientpackage_e2e_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goclient "hackathon/packages/go-client"
)

// TestAC2 exercises AC-2 verbatim from
// specs/plans/phase-2/10-feature-go-client-package.md:
//
// "The client handles base URL, auth token storage (in memory), and
// JSON/error-envelope decoding (envelope shape is `{ok, data, error:
// {code, message}}` per `apps/server/internal/http/errors.go`)."
//
// The acceptance criterion bundles three observable behaviors. Each
// gets one sub-test, all driven against the real apps/server binary
// booted by harness_test.go::startServer:
//
//  1. Base URL handling — a trailing slash on the constructor argument
//     does not break path concatenation. AC-2 names "base URL" as a
//     thing the client "handles," and the constructor's job is to
//     normalize it; a regression that double-slashes paths would
//     pass surface_test.go (which uses the bare srv.url) and silently
//     break callers that pass a URL with a trailing slash.
//
//  2. In-memory token storage — two independent *Client values pointed
//     at the same server hold distinct tokens. This is what "in
//     memory" means for AC-2: per-instance state, no package-level
//     globals. surface_test.go's existing Token()/SetToken() round-
//     trip on a single client cannot catch a regression that moved
//     the token to a package var or shared sync.Map.
//
//  3. JSON/error-envelope decoding — a real failure response
//     (Login with a wrong password) decodes into a typed
//     *goclient.APIError carrying both the envelope's `code` and
//     `message`. This is the spec's literal envelope shape
//     `{ok, data, error: {code, message}}`. surface_test.go's
//     "Me-after-Logout" check only asserts Status==401; it never
//     pins the Code or Message fields, so a regression that dropped
//     the error.code decode (or hardcoded a single fallback string)
//     would still pass it.
func TestAC2_GoClientHandlesBaseURLTokensAndEnvelopeDecoding(t *testing.T) {
	t.Parallel()

	const acStatement = "The client handles base URL, auth token storage (in memory), and JSON/error-envelope decoding (envelope shape is `{ok, data, error: {code, message}}` per `apps/server/internal/http/errors.go`)."

	srv := startServer(t)

	// One registered user, reused across the three sub-tests. Sub-tests
	// run in parallel against the same server but use independent
	// *Client values, so the only shared state is server-side
	// (the user record). No shared mutable client state.
	bootstrap := goclient.New(srv.url)
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer bootstrapCancel()
	username := randomUsername(t)
	password := randomSecret(t, 16)
	if _, err := bootstrap.Register(bootstrapCtx, username, password, srv.inviteCode); err != nil {
		t.Fatalf("bootstrap Register: %v", err)
	}

	t.Run(acStatement+"/base-url-trailing-slash-is-tolerated", func(t *testing.T) {
		t.Parallel()

		// New is documented (packages/go-client/client.go) to trim the
		// trailing slash. The black-box assertion is "/api/auth/me
		// resolves" — if New stopped trimming, the request URL would
		// contain a double slash and the server's mux would 404.
		clientWithSlash := goclient.New(srv.url + "/")
		loginResp, err := clientWithSlash.Login(context.Background(), username, password)
		if err != nil {
			t.Fatalf("Login via base URL with trailing slash: %v", err)
		}
		if loginResp.Token == "" {
			t.Fatalf("Login: empty token in %+v", loginResp)
		}
		me, err := clientWithSlash.Me(context.Background())
		if err != nil {
			t.Fatalf("Me via base URL with trailing slash: %v", err)
		}
		if me.Username != username {
			t.Fatalf("Me: username = %q, want %q", me.Username, username)
		}

		// And the no-slash form succeeds against the same server, so we
		// know both shapes resolve to the same routes (not just "the
		// server happens to canonicalize //api into /api").
		clientNoSlash := goclient.New(srv.url)
		if _, err := clientNoSlash.Login(context.Background(), username, password); err != nil {
			t.Fatalf("Login via base URL without trailing slash: %v", err)
		}
	})

	t.Run(acStatement+"/in-memory-token-storage-is-per-client", func(t *testing.T) {
		t.Parallel()

		// Two clients pointed at the same server. Each gets its own
		// freshly-issued token via Login. If the client kept the token
		// in package-level state, calls on either *Client would observe
		// whichever token was set last; the per-client assertion below
		// would then fail.
		secondUser := randomUsername(t)
		secondPassword := randomSecret(t, 16)
		setupClient := goclient.New(srv.url)
		if _, err := setupClient.Register(context.Background(), secondUser, secondPassword, srv.inviteCode); err != nil {
			t.Fatalf("Register second user: %v", err)
		}

		clientA := goclient.New(srv.url)
		clientB := goclient.New(srv.url)
		respA, err := clientA.Login(context.Background(), username, password)
		if err != nil {
			t.Fatalf("Login user A: %v", err)
		}
		respB, err := clientB.Login(context.Background(), secondUser, secondPassword)
		if err != nil {
			t.Fatalf("Login user B: %v", err)
		}
		if respA.Token == "" || respB.Token == "" {
			t.Fatalf("expected non-empty tokens, got A=%q B=%q", respA.Token, respB.Token)
		}
		if respA.Token == respB.Token {
			t.Fatalf("expected distinct tokens for distinct users, both = %q", respA.Token)
		}
		if got := clientA.Token(); got != respA.Token {
			t.Fatalf("clientA.Token() = %q, want %q", got, respA.Token)
		}
		if got := clientB.Token(); got != respB.Token {
			t.Fatalf("clientB.Token() = %q, want %q", got, respB.Token)
		}

		// Concurrent Me() calls return each client's own user. If the
		// token were stored in shared state, one of the two would see
		// the other's username (or both would see the same username).
		var wg sync.WaitGroup
		var meA, meB *goclient.User
		var errA, errB error
		wg.Add(2)
		go func() {
			defer wg.Done()
			meA, errA = clientA.Me(context.Background())
		}()
		go func() {
			defer wg.Done()
			meB, errB = clientB.Me(context.Background())
		}()
		wg.Wait()
		if errA != nil {
			t.Fatalf("clientA.Me: %v", errA)
		}
		if errB != nil {
			t.Fatalf("clientB.Me: %v", errB)
		}
		if meA.Username != username {
			t.Fatalf("clientA.Me: username = %q, want %q (token leak across *Client?)", meA.Username, username)
		}
		if meB.Username != secondUser {
			t.Fatalf("clientB.Me: username = %q, want %q (token leak across *Client?)", meB.Username, secondUser)
		}

		// Clearing one client's token does not clear the other's.
		clientA.SetToken("")
		if got := clientB.Token(); got != respB.Token {
			t.Fatalf("clientB.Token() after clientA.SetToken(\"\") = %q, want %q", got, respB.Token)
		}
	})

	t.Run(acStatement+"/error-envelope-decodes-into-typed-APIError", func(t *testing.T) {
		t.Parallel()

		// A wrong password is the canonical envelope-decoding test:
		// apps/server/internal/http/auth_handlers.go::Login replies
		// with status 401, code "unauthorized" (CodeUnauthorized in
		// errors.go), message auth.LoginErrorMessage. The client must
		// decode `error.code` AND `error.message` from the envelope —
		// not just status — so a regression that dropped one of the
		// two fails this test.
		client := goclient.New(srv.url)
		_, err := client.Login(context.Background(), username, "definitely-not-the-right-password")
		if err == nil {
			t.Fatalf("Login with wrong password: expected error, got nil")
		}
		var apiErr *goclient.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("Login with wrong password: error is not *goclient.APIError: %T (%v)", err, err)
		}
		if apiErr.Status != 401 {
			t.Fatalf("APIError.Status = %d, want 401 (err=%v)", apiErr.Status, err)
		}
		// Server-side constants (assert verbatim so a server-side
		// rename surfaces here, not as a silent client decode-then-
		// drop):
		// - apps/server/internal/http/errors.go: CodeUnauthorized = "unauthorized"
		// - apps/server/internal/auth/constants.go: LoginErrorMessage = "invalid username or password"
		if apiErr.Code != "unauthorized" {
			t.Fatalf("APIError.Code = %q, want %q (envelope.error.code did not decode)", apiErr.Code, "unauthorized")
		}
		if apiErr.Message != "invalid username or password" {
			t.Fatalf("APIError.Message = %q, want %q (envelope.error.message did not decode)", apiErr.Message, "invalid username or password")
		}

		// The failed Login must not have stored a token; the in-memory
		// store stays clean on errors.
		if got := client.Token(); got != "" {
			t.Fatalf("Token after failed Login = %q, want empty (client mutated state on error)", got)
		}

		// And IsCode is the documented branching helper — it must
		// match by code regardless of the wrapped layers.
		if !goclient.IsCode(err, "unauthorized") {
			t.Fatalf("goclient.IsCode(err, %q) = false, want true (err=%v)", "unauthorized", err)
		}
	})
}
