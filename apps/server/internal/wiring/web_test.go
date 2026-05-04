package wiring

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestReservedPrefixesCoverWiringMux is the CI-time guard for
// reservedAPITopLevelPrefixes. It parses every non-test .go file in
// this package, finds each mux.Handle / mux.HandleFunc call, extracts
// the pattern literal, derives the top-level URL prefix (the segment
// before the second slash), and fails if that prefix is neither "/"
// (the SPA fallback itself) nor a member of reservedAPITopLevelPrefixes.
//
// A new top-level prefix slipping into the mux without a matching
// reserved-list entry would silently fall through to SPA HTML on miss,
// the wrong UX for a typo'd machine request. This test makes that
// regression a build break, not a runtime surprise.
func TestReservedPrefixesCoverWiringMux(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob wiring sources: %v", err)
	}

	reserved := make(map[string]struct{}, len(reservedAPITopLevelPrefixes))
	for _, p := range reservedAPITopLevelPrefixes {
		reserved[p] = struct{}{}
	}

	fset := token.NewFileSet()

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "mux" {
				return true
			}
			if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// Non-literal pattern (variable / expression). The
				// reserved-list contract only covers patterns the
				// package itself spells out; a dynamic pattern would
				// be a separate refactor to constrain.
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Errorf("%s: cannot unquote pattern %s: %v", file, lit.Value, err)
				return true
			}

			prefix := topLevelPrefix(pattern)
			if prefix == "/" {
				// The SPA catch-all itself.
				return true
			}
			if _, ok := reserved[prefix]; !ok {
				t.Errorf("%s: mux pattern %q has top-level prefix %q which is missing from reservedAPITopLevelPrefixes; either add it to the list in web.go or move the route under an existing reserved prefix",
					file, pattern, prefix)
			}
			return true
		})
	}
}

// topLevelPrefix returns the leading "/segment" of a Go 1.22 mux
// pattern, stripping an optional "METHOD " prefix and any trailing
// path. "GET /api/presence" → "/api"; "/" → "/"; "/ws" → "/ws".
func topLevelPrefix(pattern string) string {
	// Drop method verb if present: ServeMux patterns may be
	// "GET /path" or just "/path".
	if i := strings.Index(pattern, " "); i >= 0 {
		pattern = pattern[i+1:]
	}
	if pattern == "/" || pattern == "" {
		return "/"
	}
	// pattern starts with "/"; find the next "/" to bound the segment.
	rest := pattern[1:]
	if j := strings.Index(rest, "/"); j >= 0 {
		return "/" + rest[:j]
	}
	return pattern
}
