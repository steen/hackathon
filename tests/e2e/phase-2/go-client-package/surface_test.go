package goclientpackage_e2e_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	goclient "hackathon/packages/go-client"
)

// TestAC1 exercises AC-1 verbatim from
// specs/plans/phase-2/10-feature-go-client-package.md:
//
// "A reusable Go package at `packages/go-client` (part of the single-root
// `hackathon` module, imported as `hackathon/packages/go-client`) exposes
// typed methods for: `Login`, `Register`, `Me`, `Logout`, `ListChannels`,
// `CreateChannel`, `ListMessages`, `PostMessage`, `WsTicket`, and `Watch`
// (returns a stream of inbound events)."
//
// The test does two things:
//
//  1. Compile-time + reflection check that all 10 named methods exist on
//     *goclient.Client with non-nil method values. This is what makes
//     "exposes typed methods" verifiable: a future rename of any one of
//     them breaks this test, not just the call site that happens to use
//     it.
//  2. End-to-end happy-path round-trip against a real apps/server
//     binary that walks every method (Watch returning a `<-chan Event`
//     is asserted by type — the assertion of WS delivery itself is
//     covered by AC-3's auth_transport_test.go and the existing in-
//     package ws_test.go; AC-1 is about the surface).
//
// Spec-required method names are embedded as constants to keep them
// load-bearing: a refactor that drops one shows up here, not as a
// silent compile error somewhere else.
func TestAC1_GoClientPackageExposesTypedMethods(t *testing.T) {
	t.Parallel()

	const acStatement = "A reusable Go package at `packages/go-client` (part of the single-root `hackathon` module, imported as `hackathon/packages/go-client`) exposes typed methods for: `Login`, `Register`, `Me`, `Logout`, `ListChannels`, `CreateChannel`, `ListMessages`, `PostMessage`, `WsTicket`, and `Watch` (returns a stream of inbound events)."

	t.Run(acStatement, func(t *testing.T) {
		t.Parallel()

		// 1. Reflection: every named method must exist on *goclient.Client.
		//    The compile-time check is the package-qualified call below;
		//    reflection covers the rename-to-private case (`me` instead
		//    of `Me`) which would compile inside the package but not
		//    satisfy "exposes" from outside.
		c := goclient.New("http://127.0.0.1:1") // unused for reflection
		want := []string{
			"Login",
			"Register",
			"Me",
			"Logout",
			"ListChannels",
			"CreateChannel",
			"ListMessages",
			"PostMessage",
			"WsTicket",
			"Watch",
		}
		v := reflect.ValueOf(c)
		for _, name := range want {
			m := v.MethodByName(name)
			if !m.IsValid() {
				t.Fatalf("AC-1: *goclient.Client is missing method %q (spec: %s)", name, acStatement)
			}
		}

		// 2. End-to-end: boot a real server and round-trip every method.
		//    Each method's typed return value is asserted, not just its
		//    error. Logout-then-Me asserts the token actually invalidates.
		srv := startServer(t)
		client := goclient.New(srv.url)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		username := randomUsername(t)
		password := randomSecret(t, 16)

		// Register
		regResp, err := client.Register(ctx, username, password, srv.inviteCode)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		if regResp == nil || regResp.Token == "" {
			t.Fatalf("Register: empty token in response %+v", regResp)
		}
		if regResp.User.Username != username {
			t.Fatalf("Register: username = %q, want %q", regResp.User.Username, username)
		}
		// Register sets the token internally — confirm before driving
		// the rest of the surface.
		if got := client.Token(); got != regResp.Token {
			t.Fatalf("Register did not store token: client.Token()=%q want=%q", got, regResp.Token)
		}

		// Me (after Register)
		me, err := client.Me(ctx)
		if err != nil {
			t.Fatalf("Me after Register: %v", err)
		}
		if me.Username != username {
			t.Fatalf("Me: username = %q, want %q", me.Username, username)
		}

		// Login (round-trip the same credentials; clear the token first
		// so the call is genuinely re-authenticating).
		client.SetToken("")
		loginResp, err := client.Login(ctx, username, password)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if loginResp == nil || loginResp.Token == "" {
			t.Fatalf("Login: empty token in response %+v", loginResp)
		}
		if loginResp.User.Username != username {
			t.Fatalf("Login: username = %q, want %q", loginResp.User.Username, username)
		}

		// CreateChannel
		channelName := randomChannelName(t)
		channel, err := client.CreateChannel(ctx, channelName)
		if err != nil {
			t.Fatalf("CreateChannel: %v", err)
		}
		if channel == nil || channel.ID == "" {
			t.Fatalf("CreateChannel: empty id in %+v", channel)
		}
		if channel.Name != channelName {
			t.Fatalf("CreateChannel: name = %q, want %q", channel.Name, channelName)
		}

		// ListChannels — must include the channel we just created.
		channels, err := client.ListChannels(ctx)
		if err != nil {
			t.Fatalf("ListChannels: %v", err)
		}
		var found bool
		for _, ch := range channels {
			if ch.ID == channel.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ListChannels: created channel %q not present in %+v", channel.ID, channels)
		}

		// PostMessage
		messageBody := "hello from AC-1 surface test"
		msg, err := client.PostMessage(ctx, string(channel.ID), goclient.PostMessageOptions{Body: messageBody})
		if err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
		if msg == nil || msg.ID == "" {
			t.Fatalf("PostMessage: empty id in %+v", msg)
		}
		if msg.Body != messageBody {
			t.Fatalf("PostMessage: body = %q, want %q", msg.Body, messageBody)
		}

		// ListMessages — must include the message we just posted.
		messages, err := client.ListMessages(ctx, string(channel.ID), goclient.ListMessagesOptions{Limit: 50})
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		var seen bool
		for _, m := range messages {
			if m.ID == msg.ID {
				seen = true
				break
			}
		}
		if !seen {
			t.Fatalf("ListMessages: posted message %q not present in %+v", msg.ID, messages)
		}

		// WsTicket — non-empty ticket and a future expiry.
		ticket, err := client.WsTicket(ctx)
		if err != nil {
			t.Fatalf("WsTicket: %v", err)
		}
		if ticket == nil || ticket.Ticket == "" {
			t.Fatalf("WsTicket: empty ticket in %+v", ticket)
		}
		if !ticket.ExpiresAt.After(time.Now()) {
			t.Fatalf("WsTicket: expires_at %v not in the future", ticket.ExpiresAt)
		}

		// Watch — assert the static return type matches the spec
		// ("returns a stream of inbound events"), and that an actual
		// connection succeeds end-to-end. The WS-protocol contract
		// (no bearer header on upgrade, ticket flow) belongs to AC-3
		// and lives in auth_transport_test.go (filed as #263); here we
		// only assert (a) the chan type and (b) that Watch can be
		// called against a real server without erroring out.
		watchCtx, watchCancel := context.WithCancel(ctx)
		defer watchCancel()
		events, err := client.Watch(watchCtx, goclient.WatchOptions{ChannelID: string(channel.ID)})
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
		// Reflect-check the channel element type is goclient.Event.
		evType := reflect.TypeOf(events)
		if evType.Kind() != reflect.Chan {
			t.Fatalf("Watch: return type %v is not a channel", evType)
		}
		if evType.ChanDir()&reflect.RecvDir == 0 {
			t.Fatalf("Watch: channel %v is not receivable", evType)
		}
		if evType.Elem() != reflect.TypeOf(goclient.Event{}) {
			t.Fatalf("Watch: channel element type = %v, want goclient.Event", evType.Elem())
		}
		// Tear the subscription down before exercising Logout so the
		// goroutine inside Watch exits cleanly via ctx-cancel.
		watchCancel()
		// Drain any in-flight events so the goroutine reaches its
		// `case <-ctx.Done()` path and closes `events`.
		drainDeadline := time.NewTimer(2 * time.Second)
		defer drainDeadline.Stop()
	drain:
		for {
			select {
			case _, ok := <-events:
				if !ok {
					break drain
				}
			case <-drainDeadline.C:
				break drain
			}
		}

		// Logout — must invalidate the bearer.
		if err := client.Logout(ctx); err != nil {
			t.Fatalf("Logout: %v", err)
		}
		if client.Token() != "" {
			t.Fatalf("Logout: client still holds token %q", client.Token())
		}
		// Re-arm the bearer with the now-invalidated token; subsequent
		// Me must 401.
		client.SetToken(loginResp.Token)
		if _, err := client.Me(ctx); err == nil {
			t.Fatalf("Me after Logout: expected error, got nil")
		} else {
			var apiErr *goclient.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Me after Logout: error is not *goclient.APIError: %T (%v)", err, err)
			}
			if apiErr.Status != 401 {
				t.Fatalf("Me after Logout: status = %d, want 401 (err=%v)", apiErr.Status, err)
			}
		}
	})
}
