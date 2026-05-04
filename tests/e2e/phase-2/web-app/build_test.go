// Package web_app_e2e_test holds black-box E2E tests for
// specs/plans/phase-2/40-feature-web-app.md. The tests in this file
// cover AC-1 only — siblings (#278..#281) own the remaining ACs.
//
// AC-1 verbatim:
//
//	`apps/web` is a Vite + React + TypeScript app that builds with
//	`pnpm --filter web build`.
//
// AC-2..AC-6 (login/register/chat screens, WS realtime, reconnect,
// presence list, api-client consumption) are out of scope here.
package web_app_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestAC1_WebAppViteReactTypeScriptBuild asserts AC-1 from
// specs/plans/phase-2/40-feature-web-app.md verbatim by checking three
// observable, on-disk shapes:
//
//  1. apps/web/package.json declares `vite`, a `react` dependency, and
//     `typescript` — i.e. the package itself names the Vite + React + TS
//     stack the AC requires. Reading package.json (vs. introspecting node
//     internals) keeps the assertion at the contract level the AC names.
//  2. `pnpm --filter web build` exits 0 from the repo root.
//  3. apps/web/dist/index.html exists after the build with non-empty
//     content. This is the on-disk artifact a Vite build produces; its
//     presence is the cheapest proof a build actually ran rather than
//     no-oping.
//
// The test cleans apps/web/dist before invoking the build so a stale
// artifact from a previous run can't masquerade as a successful build.
func TestAC1_WebAppViteReactTypeScriptBuild(t *testing.T) {
	root := repoRoot(t)
	webDir := filepath.Join(root, "apps", "web")

	if _, err := os.Stat(webDir); err != nil {
		t.Fatalf("apps/web not found at %s: %v", webDir, err)
	}

	t.Run("package_json_declares_vite_react_typescript", func(t *testing.T) {
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

		all := map[string]string{}
		for k, v := range pkg.Dependencies {
			all[k] = v
		}
		for k, v := range pkg.DevDependencies {
			all[k] = v
		}

		// "Vite + React + TypeScript" — three concrete package
		// dependencies the AC's stack name implies.
		for _, want := range []string{"vite", "react", "typescript"} {
			if _, ok := all[want]; !ok {
				t.Errorf("apps/web/package.json missing %q in dependencies/devDependencies", want)
			}
		}
	})

	t.Run("pnpm_filter_web_build_exits_zero_and_emits_dist_index_html", func(t *testing.T) {
		for _, tool := range []string{"pnpm", "node"} {
			if _, err := exec.LookPath(tool); err != nil {
				t.Skipf("required tool %q not on PATH: %v", tool, err)
			}
		}

		// Clean the prior dist so the assertion below proves *this*
		// invocation produced the artifact.
		distDir := filepath.Join(webDir, "dist")
		if err := os.RemoveAll(distDir); err != nil {
			t.Fatalf("clean %s: %v", distDir, err)
		}

		// 5-minute ceiling matches the issue-pr-worker stream-timeout
		// budget; a healthy build on this repo runs in well under 30s.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "pnpm", "--filter", "web", "build")
		cmd.Dir = root
		// CI=1 keeps pnpm from prompting and matches the workflow env.
		cmd.Env = append(os.Environ(), "CI=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("pnpm --filter web build did not finish within 5m\n--- stdout ---\n%s\n--- stderr ---\n%s",
				stdout.String(), stderr.String())
		}
		if err != nil {
			t.Fatalf("pnpm --filter web build exited non-zero: %v\n--- stdout ---\n%s\n--- stderr ---\n%s",
				err, stdout.String(), stderr.String())
		}

		indexHTML := filepath.Join(distDir, "index.html")
		info, err := os.Stat(indexHTML)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("expected build artifact %s after `pnpm --filter web build`; not found\n--- stdout ---\n%s",
					indexHTML, stdout.String())
			}
			t.Fatalf("stat %s: %v", indexHTML, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s exists but is empty after build", indexHTML)
		}

		// Sanity-check the artifact looks like an HTML document Vite
		// generated — guards against an unrelated file landing at the
		// path through some hypothetical build misconfiguration.
		raw, err := os.ReadFile(indexHTML)
		if err != nil {
			t.Fatalf("read %s: %v", indexHTML, err)
		}
		lower := strings.ToLower(string(raw))
		if !strings.Contains(lower, "<!doctype html") && !strings.Contains(lower, "<html") {
			t.Fatalf("%s does not look like an HTML document\n--- contents (first 500 bytes) ---\n%s",
				indexHTML, raw[:min(500, len(raw))])
		}
	})
}

// repoRoot walks up from this file until it finds go.mod. Mirrors the
// helper in tests/e2e/phase-0/monorepo-scaffold/gomod_test.go and other
// E2E test packages — each package keeps its own helper local rather
// than introducing a cross-package import for a five-line walker.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", file)
		}
		dir = parent
	}
}
