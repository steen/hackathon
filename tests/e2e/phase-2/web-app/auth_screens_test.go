// Package web_app_e2e_test holds black-box E2E tests for
// specs/plans/phase-2/40-feature-web-app.md.
//
// This file covers AC-2 only — siblings (#278..#281) own AC-1, AC-3..AC-6.
//
// AC-2 verbatim:
//
//	The app provides a login screen, a register screen (gated by invite
//	code), and a chat page with channel list + message stream + input.
//
// The assertions read the React source under apps/web/src/ at the contract
// level the AC names: each screen exists at the spec'd path, each form
// surfaces the inputs the AC mandates, the chat page wires a channel list,
// a message stream, and a composer input, and the top-level App routes
// `/login`, `/register`, and the default route to those screens.
//
// Source-level introspection (rather than booting a browser) is the right
// tool for AC-2: the AC is a structural claim ("the app provides … screens
// with … inputs"), and the existing apps/web/src/**/*.test.tsx vitest
// suite already exercises the runtime behaviour. A regression that drops
// any of the AC's named surface — the invite-code field, the channel
// list, the composer input — flips this test red without depending on
// pnpm/playwright/browser availability inside the Go test process.
package web_app_e2e_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAC2_WebAppProvidesLoginRegisterAndChatScreens asserts AC-2 from
// specs/plans/phase-2/40-feature-web-app.md verbatim by checking four
// observable shapes inside apps/web/src/:
//
//  1. Login.tsx renders username + password inputs.
//  2. Register.tsx renders username + password + invite-code inputs and
//     guards submission on a non-empty invite code (the "gated by invite
//     code" half of the AC).
//  3. Chat.tsx renders a channel list, a message stream, and a message
//     input (composer) — the three named pieces of "chat page with
//     channel list + message stream + input".
//  4. App.tsx routes `#/login` → Login, `#/register` → Register, and the
//     default route → Chat — i.e. the app actually exposes the three
//     screens to the user.
//
// Each sub-assertion fails with a message that names the missing landmark
// so a future refactor that drops one piece prints which AC sentence
// regressed rather than a generic "string not found".
func TestAC2_WebAppProvidesLoginRegisterAndChatScreens(t *testing.T) {
	root := repoRoot(t)
	srcDir := filepath.Join(root, "apps", "web", "src")

	if _, err := os.Stat(srcDir); err != nil {
		t.Fatalf("apps/web/src not found at %s: %v", srcDir, err)
	}

	t.Run("login_screen_renders_username_and_password_inputs", func(t *testing.T) {
		body := mustReadFile(t, filepath.Join(srcDir, "routes", "Login.tsx"))

		// `name="username"` and `name="password"` on <input> elements are the
		// two fields a login form must surface for the AC's claim to hold.
		// The patterns tolerate attribute reordering.
		assertInputWithName(t, body, "username", "Login.tsx username input")
		assertInputWithNameAndType(t, body, "password", "password", "Login.tsx password input")

		// The form must submit to the auth flow — guard against a screen
		// that renders the inputs but no submit handler.
		if !strings.Contains(body, "<form") {
			t.Errorf("Login.tsx: expected a <form> element wrapping the inputs")
		}
		if !regexp.MustCompile(`(?s)<button[^>]*type="submit"`).MatchString(body) {
			t.Errorf("Login.tsx: expected a submit button (type=\"submit\")")
		}
	})

	t.Run("register_screen_renders_username_password_and_invite_code_inputs", func(t *testing.T) {
		body := mustReadFile(t, filepath.Join(srcDir, "routes", "Register.tsx"))

		assertInputWithName(t, body, "username", "Register.tsx username input")
		assertInputWithNameAndType(t, body, "password", "password", "Register.tsx password input")
		// "gated by invite code" — the form must surface an invite-code
		// field. The server's API spells the JSON field `invite_code`; the
		// React form uses the same `name` so the wire shape stays clear at
		// the call site.
		assertInputWithName(t, body, "invite_code", "Register.tsx invite_code input")

		if !strings.Contains(body, "<form") {
			t.Errorf("Register.tsx: expected a <form> element wrapping the inputs")
		}

		// "gated" in the AC means an empty invite code blocks submission —
		// the source has to enforce that, either by `required` on the
		// input or by a guard inside onSubmit. Both paths are acceptable;
		// at least one must be present.
		inviteRequired := regexp.MustCompile(
			`(?s)<input[^>]*name="invite_code"[^>]*\brequired\b`,
		).MatchString(body)
		guardedInOnSubmit := regexp.MustCompile(
			`(?s)inviteCode[^}]*\.length\s*===?\s*0`,
		).MatchString(body) ||
			regexp.MustCompile(`(?s)inviteCode\.trim\(\)\.length`).MatchString(body)
		if !inviteRequired && !guardedInOnSubmit {
			t.Errorf("Register.tsx: invite-code gating not enforced — expected " +
				"`required` on the invite_code <input> or an empty-string guard " +
				"inside the submit handler")
		}
	})

	t.Run("chat_page_renders_channel_list_message_stream_and_input", func(t *testing.T) {
		body := mustReadFile(t, filepath.Join(srcDir, "routes", "Chat.tsx"))

		// AC names three pieces. Each maps to a load-bearing piece of the
		// Chat layout:
		//   - channel list: the sidebar <h2>Channels</h2> + a <ul> of
		//     channels driven by useChannels.
		//   - message stream: the messages region rendered into the
		//     [data-testid="message-list"] container the vitest suite
		//     already pins (Chat.test.tsx).
		//   - input: the composer <input aria-label="message"> below the
		//     message list.
		if !strings.Contains(body, "useChannels") {
			t.Errorf("Chat.tsx: expected useChannels hook (channel list source) to be wired")
		}
		if !regexp.MustCompile(`<h2>\s*Channels\s*</h2>`).MatchString(body) {
			t.Errorf("Chat.tsx: expected a `<h2>Channels</h2>` heading for the channel list")
		}
		if !regexp.MustCompile(`channelsState\.channels\.map`).MatchString(body) {
			t.Errorf("Chat.tsx: expected `channelsState.channels.map(...)` rendering the channel list")
		}

		if !strings.Contains(body, "useMessages") {
			t.Errorf("Chat.tsx: expected useMessages hook (message stream source) to be wired")
		}
		if !regexp.MustCompile(`data-testid="message-list"`).MatchString(body) {
			t.Errorf("Chat.tsx: expected a `data-testid=\"message-list\"` container for the message stream")
		}
		if !regexp.MustCompile(`messagesState\.messages\.map`).MatchString(body) {
			t.Errorf("Chat.tsx: expected `messagesState.messages.map(...)` rendering the message stream")
		}

		// The composer element spans multiple lines with inline arrow
		// callbacks (which themselves contain `=>`), so the regex can't
		// stop at the first `>`. Match the opening tag's interior with a
		// lazy `[^<]*?` instead and let the trailing `/>` (or `>`) close
		// it. (?s) lets `.` cross newlines. AC-2 names "input"; the
		// composer is now a <textarea> (issue #137 — multiline + Enter to
		// send), so the alternation tolerates both element names.
		if !regexp.MustCompile(`(?s)<(?:input|textarea)\b[^<]*?aria-label="message"[^<]*?/?>`).MatchString(body) {
			t.Errorf("Chat.tsx: expected a `<input>` or `<textarea>` composer with `aria-label=\"message\"`")
		}
		if !regexp.MustCompile(`(?s)<form\b[^<]*?className="composer"`).MatchString(body) {
			t.Errorf("Chat.tsx: expected a `<form className=\"composer\">` wrapping the composer")
		}
	})

	t.Run("app_router_exposes_login_register_and_chat_routes", func(t *testing.T) {
		body := mustReadFile(t, filepath.Join(srcDir, "App.tsx"))

		// The App must import all three screens — a missing import means
		// that surface is unreachable from the rendered tree.
		for _, want := range []string{
			"./routes/Login.js",
			"./routes/Register.js",
			"./routes/Chat.js",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("App.tsx: expected import of %q (route component)", want)
			}
		}

		// Hash-router shape: `/register` and `/login` must each map to
		// their respective screen. The default route (no/unknown hash)
		// must fall through to the chat view.
		if !regexp.MustCompile(`h\s*===?\s*"/register"`).MatchString(body) {
			t.Errorf("App.tsx: expected `/register` route mapping in readRoute")
		}
		if !regexp.MustCompile(`h\s*===?\s*"/login"`).MatchString(body) {
			t.Errorf("App.tsx: expected `/login` route mapping in readRoute")
		}
		// The auth-gate is observable by name: the chat surface only
		// renders when a token is present, and the unauthenticated branch
		// chooses Login or Register based on the hash.
		if !regexp.MustCompile(`<Login\b`).MatchString(body) {
			t.Errorf("App.tsx: expected <Login /> to be rendered for the login route")
		}
		if !regexp.MustCompile(`<Register\b`).MatchString(body) {
			t.Errorf("App.tsx: expected <Register /> to be rendered for the register route")
		}
		if !regexp.MustCompile(`<Chat\b`).MatchString(body) {
			t.Errorf("App.tsx: expected <Chat /> to be rendered for the authenticated route")
		}
	})

	// Guard: the assertInputWithNameAndType helper must require BOTH
	// name= and type= on the same <input> tag. A regression that
	// accidentally accepts one-of-two would let a Login.tsx that drops
	// type="password" still pass AC-2.
	t.Run("input_with_name_and_type_helper_requires_both_attributes", func(t *testing.T) {
		cases := []struct {
			label string
			body  string
			want  bool
		}{
			{
				label: "both name and type present, name first",
				body:  `<input name="password" type="password" />`,
				want:  true,
			},
			{
				label: "both name and type present, type first",
				body:  `<input type="password" name="password" />`,
				want:  true,
			},
			{
				label: "name missing",
				body:  `<input type="password" />`,
				want:  false,
			},
			{
				label: "type missing",
				body:  `<input name="password" />`,
				want:  false,
			},
			{
				label: "name and type on different <input> tags",
				body:  `<input name="password" /><input type="password" />`,
				want:  false,
			},
		}
		for _, tc := range cases {
			got := hasInputWithNameAndType(tc.body, "password", "password")
			if got != tc.want {
				t.Errorf("%s: hasInputWithNameAndType = %v, want %v", tc.label, got, tc.want)
			}
		}
	})
}

// mustReadFile reads path or fails the test. Used to keep the per-screen
// assertions terse — every read is mandatory and a missing source file
// is a structural regression worth shouting about.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// assertInputWithName asserts the body contains an <input … name="<name>" …>
// element. JSX <input> tags span multiple lines and embed arrow callbacks
// (`onChange={(e) => …}`) whose `=>` defeats a `[^>]*` skip — match with a
// lazy `[^<]*?` across newlines instead.
func assertInputWithName(t *testing.T, body, name, label string) {
	t.Helper()
	pattern := regexp.MustCompile(
		`(?s)<input\b[^<]*?\bname="` + regexp.QuoteMeta(name) + `"[^<]*?/?>`,
	)
	if !pattern.MatchString(body) {
		t.Errorf("%s: expected an <input name=\"%s\"> element", label, name)
	}
}

// inputTagPattern captures the interior of a single <input ...> opening
// tag (everything between `<input` and the closing `>` or `/>`). The body
// of any input element never contains another `<` (inputs are
// self-closing in JSX), so `[^<]*?` is sufficient to bound a tag and
// avoid running across siblings.
var inputTagPattern = regexp.MustCompile(`(?s)<input\b([^<]*?)/?>`)

// hasInputWithNameAndType reports whether body contains an <input> tag
// whose interior names both name="<wantName>" and type="<wantType>",
// in either attribute order.
func hasInputWithNameAndType(body, wantName, wantType string) bool {
	nameAttr := `name="` + wantName + `"`
	typeAttr := `type="` + wantType + `"`
	for _, m := range inputTagPattern.FindAllStringSubmatch(body, -1) {
		interior := m[1]
		if strings.Contains(interior, nameAttr) && strings.Contains(interior, typeAttr) {
			return true
		}
	}
	return false
}

// assertInputWithNameAndType asserts the body contains an <input> with
// both the given name and type attributes (in either order).
func assertInputWithNameAndType(t *testing.T, body, name, typ, label string) {
	t.Helper()
	if !hasInputWithNameAndType(body, name, typ) {
		t.Errorf("%s: expected an <input name=%q type=%q> element", label, name, typ)
	}
}
