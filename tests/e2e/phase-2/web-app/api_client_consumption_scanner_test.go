// Package web_app_e2e_test — unit-level fixtures for the AC-6
// scanner helper `stripCommentsAndStrings`.
//
// `TestAC6_WebAppConsumesApiClientForAllServerInteractions` exercises
// the scanner end-to-end against real `apps/web/src/` content. That
// integration coverage flips red only when production source happens
// to write the exact shape a regression mishandles. The fixtures
// below pin the scanner's output for synthetic inputs so a drift in
// `stripCommentsAndStrings` (off-by-one in braceDepth init, missed
// pop on nested template literals, regression that re-blanks `${`
// and breaks future patterns) is caught directly.
//
// The function is unexported; this file lives in the same package as
// `api_client_consumption_test.go` so direct calls work without
// widening the helper's visibility.
package web_app_e2e_test

import (
	"regexp"
	"strings"
	"testing"
)

// fetchCallRE mirrors the production scan's `fetch(` pattern in
// `TestAC6_WebAppConsumesApiClientForAllServerInteractions`. Keeping
// the regex local (rather than reaching into the sibling test) means
// a future contributor can tighten the production scan without
// dragging this file's expectations along.
var fetchCallRE = regexp.MustCompile(`\bfetch\s*\(`)

func TestStripCommentsAndStrings_TemplateInterpolationKeepsCallsVisible(t *testing.T) {
	cases := []struct {
		name             string
		in               string
		wantFetchVisible bool
		wantContains     []string // optional extra substrings the stripped output must retain
	}{
		{
			name:             "interpolation_pop_back_keeps_fetch_call_visible",
			in:               "`${fetch('/x')}`",
			wantFetchVisible: true,
		},
		{
			name: "nested_object_literal_pops_to_outer_template",
			// After the interpolation closes, the trailing `fetch(`
			// sits in the stTQ frame and must be blanked. A regression
			// that mis-counted braces would leak it into the stripped
			// output and silently widen the scan's true-positive set.
			in:               "`${ {a: 1} }fetch(`",
			wantFetchVisible: false,
		},
		{
			name:             "nested_template_literal_keeps_inner_fetch_visible",
			in:               "`outer${`inner${fetch('/x')}`}`",
			wantFetchVisible: true,
		},
		{
			name: "string_quoted_close_brace_does_not_pop_interpolation_early",
			// If the scanner mis-popped on the `}` inside the double
			// quote, the `+fetch('/y')` tail would be reclassified as
			// template-literal content (stTQ) and blanked. Asserting
			// that `fetch(` remains visible pins the stDQ-consumes-`}`
			// invariant directly.
			in:               "`${\"}\"+fetch('/y')}`",
			wantFetchVisible: true,
		},
		{
			name:             "interpolation_with_only_string_close_brace_strips_clean",
			in:               "`${\"}\"}`",
			wantFetchVisible: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := stripCommentsAndStrings(tc.in)
			if len(got) != len(tc.in) {
				t.Fatalf("length must be preserved (offsets stay meaningful): got %d, want %d",
					len(got), len(tc.in))
			}
			matched := fetchCallRE.MatchString(got)
			if matched != tc.wantFetchVisible {
				t.Errorf("fetch( visibility: got %v, want %v\n  in:      %q\n  stripped: %q",
					matched, tc.wantFetchVisible, tc.in, got)
			}
			for _, sub := range tc.wantContains {
				if !strings.Contains(got, sub) {
					t.Errorf("stripped output missing %q\n  in:      %q\n  stripped: %q",
						sub, tc.in, got)
				}
			}
		})
	}
}

func TestStripCommentsAndStrings_NonTemplateInputsStillStripCleanly(t *testing.T) {
	// These are the shapes the scanner handled before PR #550. Each
	// one places a banned token (`fetch(`, `axios`, etc.) inside a
	// comment or non-template string — the stripped output must not
	// leak the token back to the regex pass.
	cases := []struct {
		name string
		in   string
	}{
		{name: "line_comment_with_fetch_call", in: "// fetch('/x')\nlet x = 1\n"},
		{name: "block_comment_with_fetch_call", in: "/* fetch('/x') */\nlet x = 1\n"},
		{name: "double_quoted_string_with_fetch_call", in: "let s = \"fetch('/x')\";\n"},
		{name: "single_quoted_string_with_fetch_call", in: "let s = 'fetch(/x)';\n"},
		{name: "template_literal_without_interpolation", in: "let s = `fetch('/x')`;\n"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := stripCommentsAndStrings(tc.in)
			if len(got) != len(tc.in) {
				t.Fatalf("length must be preserved: got %d, want %d", len(got), len(tc.in))
			}
			if fetchCallRE.MatchString(got) {
				t.Errorf("fetch( leaked through scanner\n  in:       %q\n  stripped: %q",
					tc.in, got)
			}
		})
	}
}

func TestStripCommentsAndStrings_BareCodeFetchStaysVisible(t *testing.T) {
	// Sanity floor: a bare-code `fetch(` (not in a comment, string, or
	// template) must stay visible. If this case ever fails, the scanner
	// has regressed in the opposite direction (over-stripping) and the
	// production scan would silently miss real violations.
	in := "fetch('/x');\n"
	got := stripCommentsAndStrings(in)
	if !fetchCallRE.MatchString(got) {
		t.Errorf("bare-code fetch( was stripped\n  in:       %q\n  stripped: %q", in, got)
	}
}
