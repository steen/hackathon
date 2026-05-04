package seed_general_e2e_test

import (
	"path/filepath"
	"testing"
)

const generalChannelName = "general"

// AC: on first server boot (when no channels exist), a channel named
// "general" is created automatically. We assert via the public REST
// surface — register a fresh user, list channels, expect "general".
func TestFreshDBSeedsGeneralChannel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")
	srv := startServerWithDB(t, dbPath)
	t.Cleanup(srv.stop)

	token := register(t, srv, randomUsername(t), randomPassword(t))
	chans := listChannels(t, srv, token)

	var found *channelInfo
	for i := range chans {
		if chans[i].Name == generalChannelName {
			found = &chans[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("fresh db: %q not in %+v", generalChannelName, chans)
	}
	if found.ID == "" {
		t.Fatalf("fresh db: seeded channel has empty id: %+v", found)
	}
}

// AC: re-running with "general" already present is a no-op — no error,
// no duplicate. We boot once, capture the seeded id, stop the server,
// boot a second time against the same db, and assert the id is unchanged
// and the row count is still 1.
func TestRebootIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chatd.sqlite")

	first := startServerWithDB(t, dbPath)
	tokenA := register(t, first, randomUsername(t), randomPassword(t))
	beforeChans := listChannels(t, first, tokenA)
	first.stop()

	var firstGeneralID string
	generalCount := 0
	for _, c := range beforeChans {
		if c.Name == generalChannelName {
			generalCount++
			firstGeneralID = c.ID
		}
	}
	if generalCount != 1 {
		t.Fatalf("first boot: expected exactly 1 %q channel, got %d in %+v",
			generalChannelName, generalCount, beforeChans)
	}

	second := startServerWithDB(t, dbPath)
	t.Cleanup(second.stop)

	tokenB := register(t, second, randomUsername(t), randomPassword(t))
	afterChans := listChannels(t, second, tokenB)

	generalCount = 0
	var secondGeneralID string
	for _, c := range afterChans {
		if c.Name == generalChannelName {
			generalCount++
			secondGeneralID = c.ID
		}
	}
	if generalCount != 1 {
		t.Fatalf("second boot: expected exactly 1 %q channel, got %d in %+v",
			generalChannelName, generalCount, afterChans)
	}
	if secondGeneralID != firstGeneralID {
		t.Fatalf("idempotent reboot replaced channel id: before=%q after=%q",
			firstGeneralID, secondGeneralID)
	}
}
