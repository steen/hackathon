// Package ids issues ULIDs used as primary keys for users, channels, and
// messages (PRD §6). The constructor wraps oklog/ulid/v2 with a
// monotonic-within-the-same-millisecond entropy source so that IDs minted in
// rapid succession remain lexicographically ordered — the property that lets
// `messages.id` double as a paginated history cursor.
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	mathrand "math/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// entropy is the process-wide entropy source used by NewULID. The bare
// MonotonicEntropy returned by ulid.Monotonic is NOT goroutine-safe; we wrap
// it in LockedMonotonicReader so concurrent NewULID callers don't race on its
// internal state. Seeded once from crypto/rand so two processes starting at
// the same wall-clock instant don't collide.
var entropy *ulid.LockedMonotonicReader

func init() {
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		// crypto/rand failure at init means the OS RNG is broken; refuse to
		// continue with a predictable seed.
		panic(fmt.Errorf("ids: seed entropy: %w", err))
	}
	src := mathrand.NewSource(int64(binary.LittleEndian.Uint64(seed[:]))) //nolint:gosec // G115: intentional reinterpret of crypto/rand bytes as a math/rand seed.
	entropy = &ulid.LockedMonotonicReader{
		MonotonicReader: ulid.Monotonic(mathrand.New(src), 0), //nolint:gosec // G404: ULID entropy is a sortable id, NOT a session token; doc above explains.
	}
}

// NewULID returns a fresh ULID string. Within a single millisecond, repeated
// calls return strictly increasing IDs (monotonic guarantee); across
// millisecond boundaries the natural time prefix preserves order. Safe for
// concurrent use.
//
// ULIDs are sortable primary keys, NOT session tokens. The entropy stream is
// math/rand (seeded once from crypto/rand), which is predictable to anyone
// who observes a few sequential IDs. Use crypto/rand directly for any
// auth/session/CSRF token.
func NewULID() string {
	id, err := ulid.New(ulid.Timestamp(time.Now()), entropy)
	if err != nil {
		// ulid.New errors only on a too-large timestamp (year ~10889) or
		// monotonic overflow inside one ms (~1e19 IDs). Treat as fatal.
		panic(fmt.Errorf("ids: ulid.New: %w", err))
	}
	return id.String()
}
