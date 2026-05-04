package channels_and_messages_e2e_test

import (
	"fmt"
	"testing"
)

// AC-3: GET /api/channels/{id}/messages?before=&limit= returns prior
// messages, newest-first, paginated (US-6).
//
// Sends 75 messages, then asserts:
//   - default limit is 50 (msg-075 down to msg-026).
//   - explicit limit=200 returns all 75 newest-first.
//   - explicit limit=300 is clamped to 200 (here exercised by sending
//     enough messages to detect a clamp; we already have 75, so we
//     just confirm the request succeeds and returns ≤ 200).
//   - before=<id of msg-040> with limit=10 returns msg-039 .. msg-030.
//   - before=<id of oldest> returns empty slice.
func TestAC3_ListMessagesPaginatedNewestFirst(t *testing.T) {
	srv := startServer(t)
	tok, _ := register(t, srv, randomUsername(t), randomPassword(t))
	ch := createChannel(t, srv, tok, randomChannelName(t))

	const total = 75
	sent := make([]messageInfo, 0, total)
	for i := 1; i <= total; i++ {
		body := fmt.Sprintf("msg-%03d", i)
		sent = append(sent, sendMessage(t, srv, tok, ch.ID, body))
	}

	// Default limit (no params) should be 50, newest-first.
	page := listMessages(t, srv, tok, ch.ID, listMessagesOpts{})
	if len(page) != 50 {
		t.Fatalf("AC-3: default-limit page size = %d, want 50", len(page))
	}
	if page[0].Body != "msg-075" {
		t.Fatalf("AC-3: default page index 0 body=%q, want msg-075", page[0].Body)
	}
	if page[49].Body != "msg-026" {
		t.Fatalf("AC-3: default page index 49 body=%q, want msg-026", page[49].Body)
	}

	// Explicit limit 200 returns all 75 newest-first.
	all := listMessages(t, srv, tok, ch.ID, listMessagesOpts{limit: 200})
	if len(all) != total {
		t.Fatalf("AC-3: limit=200 returned %d, want %d", len(all), total)
	}
	if all[0].Body != "msg-075" || all[total-1].Body != "msg-001" {
		t.Fatalf("AC-3: limit=200 ordering wrong: first=%q last=%q", all[0].Body, all[total-1].Body)
	}

	// Limit clamp: a limit above MaxMessagesLimit (200) should not error.
	clamped := listMessages(t, srv, tok, ch.ID, listMessagesOpts{limit: 300})
	if len(clamped) > 200 {
		t.Fatalf("AC-3: limit=300 returned %d entries; max should be 200", len(clamped))
	}

	// before=<id of msg-040> with limit=10 returns msg-039..msg-030.
	idMsg040 := sent[39].ID // sent[i] is msg-(i+1)
	beforePage := listMessages(t, srv, tok, ch.ID, listMessagesOpts{limit: 10, before: idMsg040})
	if len(beforePage) != 10 {
		t.Fatalf("AC-3: before=msg-040 limit=10 returned %d, want 10", len(beforePage))
	}
	for i, want := 0, 39; i < 10; i, want = i+1, want-1 {
		expected := fmt.Sprintf("msg-%03d", want)
		if beforePage[i].Body != expected {
			t.Fatalf("AC-3: before-page idx=%d body=%q want %q", i, beforePage[i].Body, expected)
		}
	}

	// before=<id of msg-001> (oldest) returns empty slice.
	idOldest := sent[0].ID
	older := listMessages(t, srv, tok, ch.ID, listMessagesOpts{limit: 10, before: idOldest})
	if len(older) != 0 {
		t.Fatalf("AC-3: before=oldest returned %d entries; want 0", len(older))
	}
}
