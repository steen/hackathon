package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	goclient "hackathon/packages/go-client"
)

// lazyWrapDebounceFloor is the minimum interval between two
// wraps-needed queries on the same channel within a single CLI
// session. Mirrors L31 — the web client caches the first response
// for the WS-connection lifetime and only re-queries after 60s; the
// CLI applies the same floor across stream reconnects so a flapping
// network does not loop the endpoint.
const lazyWrapDebounceFloor = 60 * time.Second

// lazyWrapTracker is the per-CLI-process record of the last
// wraps-needed call timestamp per channel. wireUpLazyWrapTrigger
// constructs one tracker for the lifetime of the chatd watch loop
// (or any other long-lived stream); subsequent reconnects share it
// so the L31 debounce applies across stream restarts within one
// process.
type lazyWrapTracker struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newLazyWrapTracker() *lazyWrapTracker {
	return &lazyWrapTracker{last: map[string]time.Time{}}
}

// shouldQuery returns (true, snapshot) when the caller should fire
// the wraps-needed call for channelID, recording `now` as the most
// recent fire time. Returns (false, last) when the last query is
// within the debounce floor.
func (t *lazyWrapTracker) shouldQuery(channelID string, now time.Time) (bool, time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev := t.last[channelID]
	if !prev.IsZero() && now.Sub(prev) < lazyWrapDebounceFloor {
		return false, prev
	}
	t.last[channelID] = now
	return true, prev
}

// triggerLazyWrapForChannel runs one pass of the L14 lazy-wrap-on-
// online loop for channelID:
//
//  1. Apply the L31 client-side debounce (skip when last call was
//     within lazyWrapDebounceFloor).
//  2. Fetch GET /api/channels/{id}/members/wraps-needed.
//  3. Surface the missing-wrap count on stderr so the operator can
//     observe the loop's input.
//
// Computing the wraps and POSTing them back via PostChannelKeys is
// deferred — the CLI does not yet hold a decrypted channel root key
// (that path lands with #983's encrypted-message decrypt loop). The
// structural call here lets the supervisor wire #983 into the CLI
// without re-shaping the trigger.
//
// Returns nil on success (including the debounced + 403/404 paths so
// a non-member channel does not abort the watch loop). Wrap errors
// are logged to stderr; the caller continues streaming.
func triggerLazyWrapForChannel(
	ctx context.Context, env *Env, client *goclient.Client,
	tracker *lazyWrapTracker, channelID string, now time.Time,
) error {
	fire, prev := tracker.shouldQuery(channelID, now)
	if !fire {
		_, _ = fmt.Fprintf(env.Stderr,
			"lazy-wrap: %s skipped (last query %s ago, < %s debounce)\n",
			channelID, now.Sub(prev).Truncate(time.Second), lazyWrapDebounceFloor)
		return nil
	}
	resp, err := client.WrapsNeeded(ctx, channelID)
	if err != nil {
		// 403 (not a member) and 404 (channel gone) are non-fatal —
		// the watch loop continues. Other errors surface so the
		// supervisor can debug a configuration mismatch.
		if isLazyWrapNonFatal(err) {
			return nil
		}
		_, _ = fmt.Fprintf(env.Stderr, "lazy-wrap: %s wraps-needed failed: %v\n", channelID, err)
		return err
	}
	if len(resp.Missing) == 0 {
		return nil
	}
	_, _ = fmt.Fprintf(env.Stderr,
		"lazy-wrap: %s missing %d wrap(s) at gen %d (compute+POST deferred to #983 decrypt loop)\n",
		channelID, len(resp.Missing), missingMaxGen(resp.Missing))
	return nil
}

// isLazyWrapNonFatal classifies the error from WrapsNeeded into the
// "keep streaming" bucket. forbidden + not_found are both expected on
// non-member or stale-id paths and must not abort `chatd watch`.
func isLazyWrapNonFatal(err error) bool {
	if err == nil {
		return true
	}
	return goclient.IsCode(err, "forbidden") || goclient.IsCode(err, "not_found")
}

// missingMaxGen returns the largest generation_id in the missing
// list. The wraps-needed response groups every entry on the channel's
// CURRENT generation per L22, so this is just resp.Missing[0]
// .GenerationID in practice; the loop here keeps the contract honest
// if the server ever surfaces multiple generations in one response.
func missingMaxGen(rows []goclient.WrapsNeededRow) int64 {
	var n int64
	for _, r := range rows {
		if r.GenerationID > n {
			n = r.GenerationID
		}
	}
	return n
}
