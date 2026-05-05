// Package web_app_e2e_test holds black-box E2E tests for
// specs/plans/phase-2/40-feature-web-app.md.
//
// This file covers AC-6 only — siblings own AC-1..AC-5.
//
// AC-6 verbatim:
//
//	The app consumes `packages/api-client` for all server interactions.
package web_app_e2e_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAC6_WebAppConsumesApiClientForAllServerInteractions asserts AC-6
// from specs/plans/phase-2/40-feature-web-app.md verbatim by checking
// three observable, on-disk shapes:
//
//  1. apps/web/package.json declares `@hackathon/api-client` as a
//     workspace dependency (`workspace:*`). The "consumes
//     `packages/api-client`" half of the AC requires the workspace edge;
//     a normal `^x.y.z` resolution would pull a published artifact, not
//     the in-repo package. Reading package.json (vs. introspecting
//     pnpm's lockfile) keeps the assertion at the contract level the AC
//     names.
//
//  2. apps/web/src/api.ts is the single seam that mints the client:
//     it imports `createClient` from `@hackathon/api-client` and exports
//     a `getClient()` that returns the cached `Client`. Every other web
//     module reaches the server through this seam (or through the
//     api-client types it re-exports), so pinning the seam shape is the
//     load-bearing assertion. A regression that dropped the import — or
//     replaced `createClient` with a hand-rolled wrapper around `fetch`
//     — would flip the AC and this test red simultaneously.
//
//  3. No source file under apps/web/src/ calls `fetch(`, `new
//     WebSocket(`, `new XMLHttpRequest(`, or imports `axios` outside
//     comments/strings. "All server interactions" is a strong claim;
//     the cheapest proof is that the only network primitives the app
//     reaches for are the ones the api-client package exposes. The
//     scan tolerates the literal strings appearing inside `//` line
//     comments or block comments (apps/server/* test fixtures and code
//     comments mention `fetch` as a verb), but rejects them as bare
//     identifiers in code positions.
//
// The test is source-introspective rather than browser-driven on
// purpose: AC-6 is a structural claim about *what* the app calls, not
// *how* the app behaves at runtime. The runtime behaviour (real HTTP
// + WS round-trips) is covered by the sibling AC-1, AC-2, AC-3, AC-4,
// AC-5 tests in this directory and by the api-client's own E2E suite
// at tests/e2e/phase-2/ts-api-client-package/.
func TestAC6_WebAppConsumesApiClientForAllServerInteractions(t *testing.T) {
	root := repoRoot(t)
	webDir := filepath.Join(root, "apps", "web")
	srcDir := filepath.Join(webDir, "src")

	if _, err := os.Stat(srcDir); err != nil {
		t.Fatalf("apps/web/src not found at %s: %v", srcDir, err)
	}

	t.Run("package_json_declares_api_client_as_workspace_dep", func(t *testing.T) {
		pkgPath := filepath.Join(webDir, "package.json")
		raw, err := os.ReadFile(pkgPath)
		if err != nil {
			t.Fatalf("read %s: %v", pkgPath, err)
		}

		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if err := json.Unmarshal(raw, &pkg); err != nil {
			t.Fatalf("parse %s: %v", pkgPath, err)
		}

		const wantName = "@hackathon/api-client"
		got, ok := pkg.Dependencies[wantName]
		if !ok {
			// Surface a clear hint if the dep is in devDependencies
			// instead — AC-6 demands a runtime dependency, not a dev-only
			// edge that would be elided from `pnpm --filter web build`'s
			// production install.
			if _, dev := pkg.DevDependencies[wantName]; dev {
				t.Fatalf("apps/web/package.json: %q is in devDependencies but AC-6 requires "+
					"a runtime dependency", wantName)
			}
			t.Fatalf("apps/web/package.json: missing %q in dependencies", wantName)
		}
		// pnpm workspace protocol: `workspace:*` (or `workspace:^`,
		// `workspace:~`) is what makes the resolution point at the
		// in-repo package rather than a registry release. A plain
		// version string would resolve to a published artifact and
		// silently skip the local code under packages/api-client/.
		if !strings.HasPrefix(got, "workspace:") {
			t.Fatalf("apps/web/package.json: %q resolves to %q; AC-6 requires a `workspace:` "+
				"dependency on the in-repo package", wantName, got)
		}
	})

	t.Run("api_seam_imports_createClient_from_api_client", func(t *testing.T) {
		seamPath := filepath.Join(srcDir, "api.ts")
		body := mustReadFile(t, seamPath)

		// The seam must name `@hackathon/api-client` directly. A
		// transitive re-export via another local module would still
		// satisfy "consumes" at runtime, but pinning the import here
		// keeps the dependency edge auditable in one place.
		if !regexp.MustCompile(
			`(?s)import\s*\{[^}]*\bcreateClient\b[^}]*\}\s*from\s*"@hackathon/api-client"`,
		).MatchString(body) {
			t.Errorf("apps/web/src/api.ts: expected `import { createClient, ... } from " +
				"\"@hackathon/api-client\"`")
		}

		// The seam must also expose a `getClient()` accessor — every
		// consumer in the app calls `getClient().<method>()`, so a
		// rename of this export would split the AC's "all server
		// interactions" claim.
		if !regexp.MustCompile(`export\s+function\s+getClient\b`).MatchString(body) {
			t.Errorf("apps/web/src/api.ts: expected an exported `getClient` accessor")
		}
	})

	t.Run("no_direct_network_primitives_in_src", func(t *testing.T) {
		// The patterns target *call sites* and *constructions*. Each
		// pattern requires a syntax position that can't legitimately
		// appear in a comment block at the top of an unrelated line:
		//
		//   - `\bfetch\s*\(`                 — `fetch(` as a call expression
		//   - `\bnew\s+WebSocket\s*\(`       — `new WebSocket(`
		//   - `\bnew\s+XMLHttpRequest\s*\(`  — `new XMLHttpRequest(`
		//   - `\baxios\b`                    — any reference to the axios package
		//
		// These intentionally trigger on bare-identifier uses; the
		// stripComments helper below removes // line comments and /*…*/
		// block comments so a comment that mentions `fetch` as a verb
		// (the existing `useMessages.test.ts` uses it twice) does not
		// false-positive.
		patterns := []struct {
			name string
			re   *regexp.Regexp
		}{
			{"fetch(", regexp.MustCompile(`\bfetch\s*\(`)},
			{"new WebSocket(", regexp.MustCompile(`\bnew\s+WebSocket\s*\(`)},
			{"new XMLHttpRequest(", regexp.MustCompile(`\bnew\s+XMLHttpRequest\s*\(`)},
			{"axios", regexp.MustCompile(`\baxios\b`)},
		}

		var hits []string
		walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".ts" && ext != ".tsx" {
				return nil
			}
			// The vitest test files (*.test.ts, *.test.tsx, *.spec.ts,
			// *.spec.tsx) and the jsdom test-setup live alongside
			// production sources but stub WebSocket / fetch
			// deliberately — their job is to drive the api-client
			// substitute under test, not to make real server calls.
			// AC-6 is a claim about the production app's network
			// contract, not its test fixtures, so the scan skips them.
			// `*.spec.ts(x)` is vitest's alternative naming convention;
			// none exist under apps/web/src/ today, but skipping them
			// up front prevents a future contributor's spec file from
			// flipping AC-6 red on a stubbed `fetch`.
			base := filepath.Base(path)
			if strings.HasSuffix(base, ".test.ts") ||
				strings.HasSuffix(base, ".test.tsx") ||
				strings.HasSuffix(base, ".spec.ts") ||
				strings.HasSuffix(base, ".spec.tsx") ||
				base == "test-setup.ts" {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			stripped := stripCommentsAndStrings(string(raw))
			for _, p := range patterns {
				if loc := p.re.FindStringIndex(stripped); loc != nil {
					rel, _ := filepath.Rel(root, path)
					// Pull a 60-char window around the hit so the
					// failure message points at the offending site.
					start := loc[0]
					end := loc[1]
					ctxStart := start - 30
					if ctxStart < 0 {
						ctxStart = 0
					}
					ctxEnd := end + 30
					if ctxEnd > len(stripped) {
						ctxEnd = len(stripped)
					}
					hits = append(hits, "  "+rel+": uses `"+p.name+"` near `"+
						strings.TrimSpace(stripped[ctxStart:ctxEnd])+"`")
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", srcDir, walkErr)
		}
		if len(hits) > 0 {
			t.Errorf("AC-6 violation: production source(s) under apps/web/src/ reach the "+
				"network outside `@hackathon/api-client`:\n%s", strings.Join(hits, "\n"))
		}
	})
}

// stripCommentsAndStrings removes // line comments, /* … */ block
// comments, and the contents of "..."/'...'/`...` string literals from
// the given TypeScript / TSX source so the network-primitive scan above
// matches only call-site / constructor identifiers, not occurrences in
// docs or strings. The body is replaced with spaces of the same length
// so any byte offsets remain meaningful for error context windows.
//
// This is a small hand-rolled scanner rather than a full TS parser:
// the goal is to remove obvious false-positives (a comment that says
// "fetch the history" or a string containing "WebSocket"), not to
// understand TS semantics. Any case the scanner misclassifies is fine
// to widen later — every existing source file in apps/web/src/ is
// covered by this shape today, verified by the test passing on the
// current main.
//
// Template literals get special treatment: the body between a pair
// of backticks is treated as a string (replaced with spaces), but
// any `${ … }` interpolations are popped back into code mode so a
// regression that hides a `fetch(` call inside a template
// interpolation cannot slip past the scan. Interpolations can nest
// (a `${ }` can contain another template literal that contains
// another `${ }`), so the scanner keeps an explicit stack of
// `(state, braceDepth)` frames rather than a single state variable.
func stripCommentsAndStrings(s string) string {
	out := []byte(s)
	const (
		stCode = iota
		stLine
		stBlock
		stDQ
		stSQ
		stTQ
	)
	type frame struct {
		state      int
		braceDepth int // only meaningful for stCode frames sitting under a stTQ
	}
	stack := []frame{{state: stCode}}
	top := func() *frame { return &stack[len(stack)-1] }
	for i := 0; i < len(out); i++ {
		ch := out[i]
		switch top().state {
		case stCode:
			if ch == '/' && i+1 < len(out) && out[i+1] == '/' {
				out[i] = ' '
				out[i+1] = ' '
				i++
				stack = append(stack, frame{state: stLine})
				continue
			}
			if ch == '/' && i+1 < len(out) && out[i+1] == '*' {
				out[i] = ' '
				out[i+1] = ' '
				i++
				stack = append(stack, frame{state: stBlock})
				continue
			}
			if ch == '"' {
				out[i] = ' '
				stack = append(stack, frame{state: stDQ})
				continue
			}
			if ch == '\'' {
				out[i] = ' '
				stack = append(stack, frame{state: stSQ})
				continue
			}
			if ch == '`' {
				out[i] = ' '
				stack = append(stack, frame{state: stTQ})
				continue
			}
			// Brace tracking only applies inside an interpolation
			// frame — the outermost stCode frame keeps braceDepth at
			// 0 forever and ignores `{` / `}`.
			if len(stack) > 1 {
				switch ch {
				case '{':
					top().braceDepth++
				case '}':
					top().braceDepth--
					if top().braceDepth == 0 {
						// End of `${ … }`: pop the interpolation
						// frame back to its enclosing stTQ.
						stack = stack[:len(stack)-1]
					}
				}
			}
		case stLine:
			if ch == '\n' {
				stack = stack[:len(stack)-1]
				continue
			}
			out[i] = ' '
		case stBlock:
			if ch == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i] = ' '
				out[i+1] = ' '
				i++
				stack = stack[:len(stack)-1]
				continue
			}
			if ch != '\n' {
				out[i] = ' '
			}
		case stDQ:
			if ch == '\\' && i+1 < len(out) {
				out[i] = ' '
				out[i+1] = ' '
				i++
				continue
			}
			if ch == '"' {
				out[i] = ' '
				stack = stack[:len(stack)-1]
				continue
			}
			if ch != '\n' {
				out[i] = ' '
			}
		case stSQ:
			if ch == '\\' && i+1 < len(out) {
				out[i] = ' '
				out[i+1] = ' '
				i++
				continue
			}
			if ch == '\'' {
				out[i] = ' '
				stack = stack[:len(stack)-1]
				continue
			}
			if ch != '\n' {
				out[i] = ' '
			}
		case stTQ:
			if ch == '\\' && i+1 < len(out) {
				out[i] = ' '
				out[i+1] = ' '
				i++
				continue
			}
			if ch == '`' {
				out[i] = ' '
				stack = stack[:len(stack)-1]
				continue
			}
			if ch == '$' && i+1 < len(out) && out[i+1] == '{' {
				// Enter `${ … }`: push a code frame seeded with
				// braceDepth=1 for the `{` we are stepping over.
				// The matching `}` closes the interpolation and
				// returns to this stTQ frame.
				// Leave both `$` and `{` un-blanked so brace pairs
				// inside the interpolation still balance against
				// these characters in any byte-offset diagnostics
				// upstream — the scan only looks at the stripped
				// content, and `${` is not a banned token.
				i++
				stack = append(stack, frame{state: stCode, braceDepth: 1})
				continue
			}
			if ch != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}
