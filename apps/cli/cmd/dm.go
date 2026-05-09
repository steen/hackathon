package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	goclient "hackathon/packages/go-client"
)

// DM implements `chatd dm <subcommand>` and dispatches list/send/history/
// read/watch. With no sub-subcommand the user gets a usage error so a
// bare `chatd dm` does not silently no-op.
func DM(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: chatd dm list|send|history|read|watch [args]")
	}
	switch args[0] {
	case "list":
		return dmList(ctx, env, args[1:])
	case "send":
		return dmSend(ctx, env, args[1:])
	case "history":
		return dmHistory(ctx, env, args[1:])
	case "read":
		return dmRead(ctx, env, args[1:])
	case "watch":
		return dmWatch(ctx, env, args[1:])
	default:
		return fmt.Errorf("unknown dm subcommand %q (want list|send|history|read|watch)", args[0])
	}
}

// dmList prints `<id>\t<peer_username>\t<unread_count>\t<last_message_at>`
// per conversation. Sort is server-supplied (last_message_at DESC per
// decision-log §9). Empty last_message_at renders as the empty string —
// the server hides empty conversations (decision §3) but the field is a
// pointer in the wire shape so a defensive empty render keeps a future
// behavior change from spilling Go's zero-time format into stdout.
func dmList(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("dm list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("dm list", err)
	}
	convs, err := client.ListDMs(ctx)
	if err != nil {
		return mapDMError("dm list", err)
	}
	for _, c := range convs {
		ts := ""
		if c.LastMessageAt != nil {
			ts = c.LastMessageAt.UTC().Format(time.RFC3339Nano)
		}
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\t%d\t%s\n", c.ID, c.Peer.Username, c.UnreadCount, ts)
	}
	return nil
}

// dmSend resolves <peer> to a user_id, find-or-creates the conversation,
// posts the body, and prints `<message_id>\t<body>`. Body "-" reads from
// stdin (matches `chatd send`).
func dmSend(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("dm send", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: chatd dm send <peer-username-or-id> <body|->")
	}
	peer := rest[0]
	body := strings.Join(rest[1:], " ")
	if body == "-" {
		raw, err := io.ReadAll(env.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		body = strings.TrimRight(string(raw), "\r\n")
		if body == "" {
			return fmt.Errorf("dm send: stdin produced an empty message")
		}
	}
	client, cfg, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("dm send", err)
	}
	peerID, err := resolvePeer(ctx, env, cfg.Token, client, peer)
	if err != nil {
		return fmt.Errorf("dm send: %w", err)
	}
	conv, err := client.CreateDM(ctx, peerID)
	if err != nil {
		return mapDMError("dm send", err)
	}
	msg, err := client.SendDMMessage(ctx, conv.ID.String(), body)
	if err != nil {
		return mapDMError("dm send", err)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", msg.ID, msg.Body)
	return nil
}

// dmHistory prints messages newest-first as
// `<id>\t<sender_user_id>\t<body>\t<created_at>`. Supports the same
// --limit / --before cursors as `chatd history`.
func dmHistory(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("dm history", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	limit := fs.Int("limit", 0, "max messages to return (0 = server default)")
	before := fs.String("before", "", "ULID cursor; only return messages older than this id")
	flagArgs, positional := splitFlagsAndPositional(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return fmt.Errorf("usage: chatd dm history <peer-username-or-id> [--limit N] [--before ID]")
	}
	peer := positional[0]
	client, cfg, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("dm history", err)
	}
	peerID, err := resolvePeer(ctx, env, cfg.Token, client, peer)
	if err != nil {
		return fmt.Errorf("dm history: %w", err)
	}
	conv, err := client.CreateDM(ctx, peerID)
	if err != nil {
		return mapDMError("dm history", err)
	}
	msgs, err := client.ListDMMessages(ctx, conv.ID.String(), goclient.ListDMMessagesOptions{
		Limit:  *limit,
		Before: goclient.ULID(*before),
	})
	if err != nil {
		return mapDMError("dm history", err)
	}
	for _, m := range msgs {
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\t%s\t%s\n",
			m.ID, m.SenderUserID, m.Body, m.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	return nil
}

// dmRead advances the viewer's read pointer for the conversation with
// <peer> to <message-id> and prints `ok` on success. Server applies the
// advance-only rule (decision-log L5).
func dmRead(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("dm read", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: chatd dm read <peer-username-or-id> <message-id>")
	}
	peer, messageID := rest[0], rest[1]
	client, cfg, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("dm read", err)
	}
	peerID, err := resolvePeer(ctx, env, cfg.Token, client, peer)
	if err != nil {
		return fmt.Errorf("dm read: %w", err)
	}
	conv, err := client.CreateDM(ctx, peerID)
	if err != nil {
		return mapDMError("dm read", err)
	}
	if err := client.MarkDMRead(ctx, conv.ID.String(), messageID); err != nil {
		return mapDMError("dm read", err)
	}
	_, _ = fmt.Fprintln(env.Stdout, "ok")
	return nil
}

// dmWatch streams `{type:"dm"}` frames as
// `<conversation_id>\t<message_id>\t<sender_user_id>\t<body>`. With no
// peer arg every DM frame is printed; with a peer arg the conversation
// id is resolved up front and frames are filtered to that conversation.
// The WS connection always lands on the seeded default channel topic
// (L15 fallback) plus the user:<viewer> inbox topic, which is where DM
// frames are fanned (decision §4 / §8).
func dmWatch(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("dm watch", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	once := fs.Bool("once", false, "exit after the first stream closes (skip reconnect)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) > 1 {
		return fmt.Errorf("usage: chatd dm watch [<peer-username-or-id>]")
	}

	client, cfg, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("dm watch", err)
	}

	wantConvID := ""
	if len(rest) == 1 {
		peerID, err := resolvePeer(ctx, env, cfg.Token, client, rest[0])
		if err != nil {
			return fmt.Errorf("dm watch: %w", err)
		}
		conv, err := client.CreateDM(ctx, peerID)
		if err != nil {
			return mapDMError("dm watch", err)
		}
		wantConvID = conv.ID.String()
	}

	backoff := initialWatchBackoff
	for {
		streamErr := dmStreamOnce(ctx, env, client, wantConvID)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // intentional: ctx.Err shadows streamErr on cancel
		}
		if *once {
			return streamErr
		}
		if streamErr != nil {
			_, _ = fmt.Fprintf(env.Stderr, "dm watch: %v (reconnecting in %s)\n", streamErr, backoff)
		} else {
			_, _ = fmt.Fprintf(env.Stderr, "dm watch: stream closed (reconnecting in %s)\n", backoff)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxWatchBackoff {
			backoff = maxWatchBackoff
		}
	}
}

// dmFrame is the payload shape of a {type:"dm"} envelope's data field.
// Mirrors the server-side broadcastDM in apps/server/internal/http/
// dms_handlers.go: the data block carries `conversation` (per-viewer
// view with peer + unread_count) plus `dm_message` (the persisted
// row). Decoded from Event.Raw because the go-client surfaces dm
// frames as a passthrough (ws.go:38-42).
type dmFrame struct {
	Type string `json:"type"`
	Data struct {
		Conversation goclient.Conversation `json:"conversation"`
		DMMessage    goclient.DMMessage    `json:"dm_message"`
	} `json:"data"`
}

func dmStreamOnce(ctx context.Context, env *Env, client *goclient.Client, wantConvID string) error {
	events, err := client.Watch(ctx, goclient.WatchOptions{})
	if err != nil {
		return err
	}
	for ev := range events {
		if ev.Type != goclient.EventTypeDM {
			continue
		}
		var frame dmFrame
		if err := json.Unmarshal(ev.Raw, &frame); err != nil {
			continue
		}
		if wantConvID != "" && frame.Data.DMMessage.ConversationID.String() != wantConvID {
			continue
		}
		if _, err := fmt.Fprintf(env.Stdout, "%s\t%s\t%s\t%s\n",
			frame.Data.DMMessage.ConversationID,
			frame.Data.DMMessage.ID,
			frame.Data.DMMessage.SenderUserID,
			frame.Data.DMMessage.Body,
		); err != nil {
			return err
		}
	}
	return nil
}

// resolvePeer turns a username-or-id token into a user_id. If the token
// passes ULID validation it is returned verbatim so a caller that
// already has the id avoids a /api/users round-trip; otherwise the
// directory is fetched and a case-sensitive username match is required.
// The directory is small (per-invite, PRD §4) so a full GET is fine.
func resolvePeer(ctx context.Context, _ *Env, token string, client *goclient.Client, peer string) (string, error) {
	if peer == "" {
		return "", fmt.Errorf("peer must not be empty")
	}
	if goclient.ULID(peer).Valid() {
		return peer, nil
	}
	users, err := fetchUsers(ctx, token, client.BaseURL())
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Username == peer {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("no user named %q in directory", peer)
}

// userSummary mirrors the server-side http.UserSummary shape returned by
// GET /api/users. Local mirror because the server type lives under an
// internal/ package; drift would surface as a JSON-decode failure on
// first call. The server response is `{users:[{id,username},...]}`.
type userSummary struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type usersListResponse struct {
	Users []userSummary `json:"users"`
}

// fetchUsers issues GET /api/users via the standard http.Client because
// the go-client does not expose a typed method for it (out of footprint
// for this PR). The request mirrors goclient.Client.do: bearer auth,
// JSON envelope decode, MaxResponseBytes-bounded read.
func fetchUsers(ctx context.Context, token, baseURL string) ([]userSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/users", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, goclient.MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("users list: status %d", resp.StatusCode)
		}
		return nil, nil
	}
	var env struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode envelope (status %d): %w", resp.StatusCode, err)
	}
	if !env.OK {
		code, msg := "unknown", resp.Status
		if env.Error != nil {
			code, msg = env.Error.Code, env.Error.Message
		}
		return nil, fmt.Errorf("users list: %s: %s", code, msg)
	}
	var out usersListResponse
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &out); err != nil {
			return nil, fmt.Errorf("decode users: %w", err)
		}
	}
	return out.Users, nil
}

// mapDMError mirrors mapChannelError for the DM surface. Surfaces typed
// APIErrors as `<label>: <code>: <message>` so stderr names the command
// and the canonical code, while non-API errors pass through wrapped.
func mapDMError(label string, err error) error {
	var apiErr *goclient.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %s: %s", label, apiErr.Code, apiErr.Message)
	}
	return fmt.Errorf("%s: %w", label, err)
}
