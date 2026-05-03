package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hackathon/apps/cli/internal/config"
)

const (
	testInvite   = "test-invite-code"
	testPassword = "correct-horse-battery-staple"
)

// seed is a small helper that creates a user via Register so other
// tests can start from a logged-in state. Stdout/stderr buffers are
// reset on the way out so callers can assert on subsequent commands
// without worrying about the registration banner.
func seed(t *testing.T, rig *runRig, username string) {
	t.Helper()
	ctx, cancel := mustCtx()
	defer cancel()
	if err := Register(ctx, rig.env, []string{
		"--password", testPassword,
		"--invite-code", testInvite,
		username,
	}); err != nil {
		t.Fatalf("seed register: %v", err)
	}
	rig.stdout.Reset()
	rig.stderr.Reset()
}

func TestCLILoginPersistsToken(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	// Register first so login has a user to authenticate against.
	seed(t, rig, "alice")

	// Clear stored creds so we exercise the login write path fresh.
	if err := config.Clear(rig.env.ConfigDir); err != nil {
		t.Fatalf("clear config: %v", err)
	}

	ctx, cancel := mustCtx()
	defer cancel()
	if err := Login(ctx, rig.env, []string{
		"--username", "alice",
		"--password", testPassword,
	}); err != nil {
		t.Fatalf("Login: %v", err)
	}

	cfg, err := config.Load(rig.env.ConfigDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Token == "" {
		t.Fatal("config.Token is empty after login")
	}
	if cfg.User == nil || cfg.User.Username != "alice" {
		t.Errorf("config.User = %+v, want alice", cfg.User)
	}
	if cfg.Server != fs.URL() {
		t.Errorf("config.Server = %q, want %q", cfg.Server, fs.URL())
	}
}

func TestCLIRegisterCreatesUserAndStoresToken(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)

	ctx, cancel := mustCtx()
	defer cancel()
	if err := Register(ctx, rig.env, []string{
		"--password", testPassword,
		"--invite-code", testInvite,
		"bob",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg, err := config.Load(rig.env.ConfigDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Token == "" {
		t.Fatal("token not stored after register")
	}
	if cfg.User == nil || cfg.User.Username != "bob" {
		t.Errorf("user = %+v, want bob", cfg.User)
	}
	if !strings.Contains(rig.stdout.String(), "Registered as bob") {
		t.Errorf("stdout = %q, want to contain registration confirmation", rig.stdout.String())
	}
}

func TestCLIChannelsListsChannels(t *testing.T) {
	fs := newFakeServer(t)
	fs.addChannel("random")
	fs.addChannel("dev")
	rig := newRig(t, fs)
	seed(t, rig, "alice")

	ctx, cancel := mustCtx()
	defer cancel()
	if err := Channels(ctx, rig.env, nil); err != nil {
		t.Fatalf("Channels: %v", err)
	}
	out := rig.stdout.String()
	for _, name := range []string{"general", "random", "dev"} {
		if !strings.Contains(out, "\t"+name+"\n") {
			t.Errorf("stdout missing channel %q; got:\n%s", name, out)
		}
	}
}

func TestCLIHistoryReturnsPriorMessages(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	// Post two messages via Send so history has something to read.
	for _, body := range []string{"hello", "world"} {
		ctx, cancel := mustCtx()
		rig.stdout.Reset()
		if err := Send(ctx, rig.env, []string{channel, body}); err != nil {
			cancel()
			t.Fatalf("Send %q: %v", body, err)
		}
		cancel()
	}

	rig.stdout.Reset()
	ctx, cancel := mustCtx()
	defer cancel()
	if err := History(ctx, rig.env, []string{channel}); err != nil {
		t.Fatalf("History: %v", err)
	}
	out := rig.stdout.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("history output missing messages; got:\n%s", out)
	}
}

func TestCLISendPostsMessage(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := mustCtx()
	defer cancel()
	if err := Send(ctx, rig.env, []string{channel, "hi", "there"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	fs.mu.Lock()
	msgs := fs.messages[channel]
	fs.mu.Unlock()
	if len(msgs) != 1 {
		t.Fatalf("server saw %d messages, want 1", len(msgs))
	}
	if msgs[0].Body != "hi there" {
		t.Errorf("body = %q, want %q", msgs[0].Body, "hi there")
	}
	if !strings.HasPrefix(strings.TrimSpace(rig.stdout.String()), "m-") {
		t.Errorf("stdout = %q, want message id", rig.stdout.String())
	}
}

func TestCLISendReadsMessageFromStdinWhenDash(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	rig.stdin.WriteString("piped message\n")

	ctx, cancel := mustCtx()
	defer cancel()
	if err := Send(ctx, rig.env, []string{channel, "-"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	fs.mu.Lock()
	msgs := fs.messages[channel]
	fs.mu.Unlock()
	if len(msgs) != 1 || msgs[0].Body != "piped message" {
		t.Errorf("server messages = %+v, want one with body 'piped message'", msgs)
	}
}

func TestCLIWatchReceivesRealTimeMessage(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")
	channel := fs.channels[0].ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, rig.env, []string{"--once", channel})
	}()

	// Give Watch time to subscribe before broadcasting.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fs.subsMu.Lock()
		n := len(fs.subs[channel])
		fs.subsMu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Post a message via the public path so the fake server broadcasts.
	postCtx, postCancel := mustCtx()
	if err := Send(postCtx, rig.env, []string{channel, "watch-me"}); err != nil {
		postCancel()
		t.Fatalf("Send: %v", err)
	}
	postCancel()

	// Wait for output to materialise then cancel ctx so Watch returns.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rig.stdout.String(), "watch-me") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Watch returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return within 2s of context cancel")
	}

	if !strings.Contains(rig.stdout.String(), "watch-me") {
		t.Errorf("watch stdout missing message; got:\n%s", rig.stdout.String())
	}
}

func TestCLIWhoamiPrintsUsernameWhenLoggedIn(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")

	rig.stdout.Reset()
	ctx, cancel := mustCtx()
	defer cancel()
	if err := Whoami(ctx, rig.env, nil); err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if got := strings.TrimSpace(rig.stdout.String()); got != "alice" {
		t.Errorf("whoami stdout = %q, want %q", got, "alice")
	}
}

func TestCLIWhoamiWhenNotLoggedIn(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)

	ctx, cancel := mustCtx()
	defer cancel()
	err := Whoami(ctx, rig.env, nil)
	if err == nil {
		t.Fatal("Whoami without token returned nil; want error")
	}
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestCLILogoutClearsLocalConfig(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")

	if cfg, _ := config.Load(rig.env.ConfigDir); cfg.Token == "" {
		t.Fatal("precondition: expected token to be present after seed")
	}

	ctx, cancel := mustCtx()
	defer cancel()
	if err := Logout(ctx, rig.env, nil); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	cfg, err := config.Load(rig.env.ConfigDir)
	if err != nil {
		t.Fatalf("load after logout: %v", err)
	}
	if cfg.Token != "" {
		t.Errorf("token still set after logout: %q", cfg.Token)
	}
	if cfg.User != nil {
		t.Errorf("user still set after logout: %+v", cfg.User)
	}
}

func TestCLILogoutThenRequestReturns401(t *testing.T) {
	fs := newFakeServer(t)
	rig := newRig(t, fs)
	seed(t, rig, "alice")

	// Capture the live token so we can replay it after logout has
	// cleared the config — the server must reject it.
	cfg, err := config.Load(rig.env.ConfigDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	staleToken := cfg.Token

	logoutCtx, logoutCancel := mustCtx()
	if err := Logout(logoutCtx, rig.env, nil); err != nil {
		logoutCancel()
		t.Fatalf("Logout: %v", err)
	}
	logoutCancel()

	// Restore the stale token to disk and run channels — must 401.
	if err := config.Save(rig.env.ConfigDir, &config.File{
		Server: fs.URL(),
		Token:  staleToken,
	}); err != nil {
		t.Fatalf("re-save stale token: %v", err)
	}

	ctx, cancel := mustCtx()
	defer cancel()
	err = Channels(ctx, rig.env, nil)
	if err == nil {
		t.Fatal("Channels with stale token returned nil; want 401-style error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unauthorized") &&
		!strings.Contains(err.Error(), "401") {
		t.Errorf("err %v does not mention unauthorized/401", err)
	}
}
