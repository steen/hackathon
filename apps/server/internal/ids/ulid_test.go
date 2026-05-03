package ids_test

import (
	"sync"
	"testing"

	"hackathon/apps/server/internal/ids"
)

// TestNewULIDLength verifies the canonical 26-char Crockford-base32 encoding.
func TestNewULIDLength(t *testing.T) {
	id := ids.NewULID()
	if len(id) != 26 {
		t.Fatalf("ULID length = %d; want 26 (got %q)", len(id), id)
	}
}

// TestNewULIDIsLexicographicallyIncreasing covers the acceptance criterion:
// "NewULID() returns lexicographically increasing IDs across rapid calls."
// The monotonic entropy source guarantees this within the same millisecond
// and the timestamp prefix preserves it across boundaries.
func TestNewULIDIsLexicographicallyIncreasing(t *testing.T) {
	const n = 10_000
	prev := ids.NewULID()
	for i := 1; i < n; i++ {
		next := ids.NewULID()
		if next <= prev {
			t.Fatalf("ULID #%d not strictly greater than #%d:\n  prev=%s\n  next=%s",
				i, i-1, prev, next)
		}
		prev = next
	}
}

// TestNewULIDIsUnique sanity-checks that we don't collide.
func TestNewULIDIsUnique(t *testing.T) {
	const n = 10_000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := ids.NewULID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ULID at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestNewULIDConcurrentSafe drives many goroutines through the shared
// monotonic entropy source. The test fails (under -race) if the source is not
// goroutine-safe, and fails on collisions if the mutex is missing.
func TestNewULIDConcurrentSafe(t *testing.T) {
	const goroutines = 32
	const perGoroutine = 1000

	var wg sync.WaitGroup
	results := make([][]string, goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		results[g] = make([]string, perGoroutine)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				results[g][i] = ids.NewULID()
			}
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perGoroutine)
	for _, slice := range results {
		for _, id := range slice {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate ULID under concurrency: %s", id)
			}
			seen[id] = struct{}{}
		}
	}
}
