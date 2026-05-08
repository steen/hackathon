package wiring

import (
	"context"
	"log"
	"time"

	"hackathon/apps/server/internal/seed"
)

// seedTimeout caps the first-boot seed; sqlite is local and the work is one
// SELECT + maybe one INSERT, so any wait beyond a few seconds means the DB
// is wedged and the operator wants to know via the returned error rather
// than a hung listener.
const seedTimeout = 5 * time.Second

// registerSeed runs idempotent first-boot seeds. Called from Build AFTER
// the DB has been opened + migrated and BEFORE the listener starts so a
// fresh install always has a #general channel.
//
// On error the process exits via log.Fatalf: a server that cannot satisfy
// its first-boot invariants must not start serving traffic. The actual
// seeding lives in runSeed so the context cancel defer runs before the
// fatal exit (gocritic exitAfterDefer).
func registerSeed(deps Deps) {
	if err := runSeed(deps); err != nil {
		log.Fatalf("seed: ensure general channel: %v", err)
	}
}

func runSeed(deps Deps) error {
	ctx, cancel := context.WithTimeout(context.Background(), seedTimeout)
	defer cancel()
	return seed.EnsureGeneralChannel(ctx, deps.Repo)
}
