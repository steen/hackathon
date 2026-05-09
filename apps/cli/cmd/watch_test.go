package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	goclient "hackathon/packages/go-client"
)

func TestStreamOnceWarnsOnUnparseableMessageFrame(t *testing.T) {
	// {type:"message",data:<garbage>}: goclient.decodeEvent sets Type
	// but leaves Message nil because data fails to unmarshal as Message.
	// Two frames within the throttle window -> exactly one warning.
	bad := []byte(`{"type":"message","data":"not-a-message-object"}`)
	rig := newDMWSRig(t, [][]byte{bad, bad})

	client := goclient.New(rig.server.URL, goclient.WithToken("tok"))
	stdout := &safeBuf{}
	stderr := &safeBuf{}
	env := &Env{Stdout: stdout, Stderr: stderr}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := streamOnce(ctx, env, client, "ch-1"); err != nil {
		t.Fatalf("streamOnce: %v", err)
	}

	got := stderr.String()
	if !strings.Contains(got, "watch: drop unparseable frame: malformed message payload") {
		t.Fatalf("stderr missing warning; got %q", got)
	}
	if c := strings.Count(got, "watch: drop unparseable frame:"); c != 1 {
		t.Fatalf("warning count = %d, want 1 (throttled); stderr=%q", c, got)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestStreamOnceIgnoresNonMessageFrames(t *testing.T) {
	// dm/channel/read frames also surface with Message == nil but must
	// not trigger the warning — they are not "unparseable", just other
	// types this watcher chose to ignore.
	dmFrame := []byte(`{"type":"dm","data":{"conversation":{},"dm_message":{}}}`)
	channelFrame := []byte(`{"type":"channel","data":{"kind":"create","channel":{"id":"ch-2","name":"x","created_at":"2025-01-01T00:00:00Z"}}}`)
	rig := newDMWSRig(t, [][]byte{dmFrame, channelFrame})

	client := goclient.New(rig.server.URL, goclient.WithToken("tok"))
	stdout := &safeBuf{}
	stderr := &safeBuf{}
	env := &Env{Stdout: stdout, Stderr: stderr}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := streamOnce(ctx, env, client, "ch-1"); err != nil {
		t.Fatalf("streamOnce: %v", err)
	}

	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty (non-message frames must not warn)", stderr.String())
	}
}
