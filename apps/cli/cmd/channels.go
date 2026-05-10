package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"regexp"

	goclient "hackathon/packages/go-client"
)

// channelNameRe mirrors the server-side regex in
// apps/server/internal/http/channels_handlers.go: 1-40 chars, ASCII
// lowercase letters, digits, hyphens, must start with a letter or digit.
// Validating client-side avoids a round-trip on obvious typos; the
// server is still the source of truth. Drift against the server copy is
// guarded by TestChannelNameRegexMatchesServer.
var channelNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ChannelNameRe exposes channelNameRe to the cross-package drift test in
// apps/server/internal/http/channels_regex_drift_test.go
// (TestChannelNameRegexMatchesServer), which compares it against the
// server's equivalent. Aliased instead of inlined so the runtime
// validator and the test see the same compiled pattern.
var ChannelNameRe = channelNameRe

// Channels implements `chatd channels`. With no sub-subcommand it lists
// every channel one per line as `<id>\t<name>`. Sub-subcommands:
// create, rename, read, invite, kick, leave, members.
func Channels(ctx context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "create":
			return channelsCreate(ctx, env, args[1:])
		case "rename":
			return channelsRename(ctx, env, args[1:])
		case "read":
			return channelsRead(ctx, env, args[1:])
		case "invite":
			return channelsInvite(ctx, env, args[1:])
		case "kick":
			return channelsKick(ctx, env, args[1:])
		case "leave":
			return channelsLeave(ctx, env, args[1:])
		case "members":
			return channelsMembers(ctx, env, args[1:])
		}
	}
	fs := flag.NewFlagSet("channels", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels", err)
	}
	chans, err := client.ListChannels(ctx)
	if err != nil {
		return err
	}
	for _, c := range chans {
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", c.ID, c.Name)
	}
	return nil
}

func channelsCreate(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels create", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	public := fs.Bool("public", false, "create as a public channel (visible to every registered user; operator-readable per R1.2)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: chatd channels create [--public] <name>")
	}
	name := rest[0]
	if !channelNameRe.MatchString(name) {
		return fmt.Errorf("channels create: invalid name %q: must be 1-40 chars of lowercase letters, digits, hyphens; must start with a letter or digit", name)
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels create", err)
	}
	ch, err := client.CreateChannelOpts(ctx, name, *public)
	if err != nil {
		return mapChannelError("channels create", err)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", ch.ID, ch.Name)
	return nil
}

// channelsInvite implements `chatd channels invite <channel-name> <user-id>`.
// Resolves the channel name to an id via ListChannels, then issues the
// invite. The membership block is omitted in this PR — the server
// accepts invites without a signature on public channels (the auto-add
// carve-out) and rejects them on private channels with a 400; the wrap
// loop in #984 will surface a `--membership` flag once the inviter-
// signature pipeline lands.
func channelsInvite(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels invite", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: chatd channels invite <channel-name> <user-id>")
	}
	chName, userID := rest[0], rest[1]
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels invite", err)
	}
	chID, err := resolveChannelID(ctx, client, chName)
	if err != nil {
		return mapChannelError("channels invite", err)
	}
	m, err := client.InviteChannelMember(ctx, chID, userID, nil)
	if err != nil {
		return mapChannelError("channels invite", err)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s\n", m.UserID)
	return nil
}

// channelsKick implements `chatd channels kick <channel-name> <user-id>`.
// Removes the named user from the channel. #general kicks are rejected
// by the server with 403 per L8.
func channelsKick(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels kick", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: chatd channels kick <channel-name> <user-id>")
	}
	chName, userID := rest[0], rest[1]
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels kick", err)
	}
	chID, err := resolveChannelID(ctx, client, chName)
	if err != nil {
		return mapChannelError("channels kick", err)
	}
	if err := client.KickChannelMember(ctx, chID, userID); err != nil {
		return mapChannelError("channels kick", err)
	}
	_, _ = fmt.Fprintln(env.Stdout, "ok")
	return nil
}

// channelsLeave implements `chatd channels leave <channel-name>`.
// Self-leave for the caller. #general self-leave is rejected by the
// server with 403 per L8.
func channelsLeave(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels leave", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: chatd channels leave <channel-name>")
	}
	chName := rest[0]
	client, cfg, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels leave", err)
	}
	if cfg == nil || cfg.User == nil || cfg.User.ID == "" {
		return fmt.Errorf("channels leave: no cached user id; re-login to refresh")
	}
	chID, err := resolveChannelID(ctx, client, chName)
	if err != nil {
		return mapChannelError("channels leave", err)
	}
	if err := client.KickChannelMember(ctx, chID, cfg.User.ID); err != nil {
		return mapChannelError("channels leave", err)
	}
	_, _ = fmt.Fprintln(env.Stdout, "ok")
	return nil
}

// channelsMembers implements `chatd channels members <channel-name>`.
// Lists members one per line as `<user-id>\t<username>`.
func channelsMembers(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels members", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: chatd channels members <channel-name>")
	}
	chName := rest[0]
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels members", err)
	}
	chID, err := resolveChannelID(ctx, client, chName)
	if err != nil {
		return mapChannelError("channels members", err)
	}
	members, err := client.ListChannelMembers(ctx, chID)
	if err != nil {
		return mapChannelError("channels members", err)
	}
	for _, m := range members {
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", m.UserID, m.Username)
	}
	return nil
}

// resolveChannelID resolves a channel name to its ULID via ListChannels.
// Shared between every member-CRUD subcommand so the user-facing CLI
// stays name-based while the wire is id-based.
func resolveChannelID(ctx context.Context, client *goclient.Client, name string) (string, error) {
	chans, err := client.ListChannels(ctx)
	if err != nil {
		return "", err
	}
	for _, ch := range chans {
		if ch.Name == name {
			return ch.ID.String(), nil
		}
	}
	return "", fmt.Errorf("no channel named %q", name)
}

func channelsRename(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels rename", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: chatd channels rename <current-name> <new-name>")
	}
	currentName, newName := rest[0], rest[1]
	if !channelNameRe.MatchString(newName) {
		return fmt.Errorf("channels rename: invalid name %q: must be 1-40 chars of lowercase letters, digits, hyphens; must start with a letter or digit", newName)
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels rename", err)
	}
	chans, err := client.ListChannels(ctx)
	if err != nil {
		return mapChannelError("channels rename", err)
	}
	var found *goclient.Channel
	for i := range chans {
		if chans[i].Name == currentName {
			found = &chans[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("channels rename: no channel named %q", currentName)
	}
	updated, err := client.RenameChannel(ctx, found.ID.String(), newName)
	if err != nil {
		return mapChannelError("channels rename", err)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", updated.ID, updated.Name)
	return nil
}

// channelsRead implements `chatd channels read <name> <message-id>`.
// Resolves the channel name to its id via ListChannels, then advances
// the viewer's read pointer for that channel via MarkChannelRead. The
// server applies the advance-only rule (decision-log L5); on success
// `ok` is printed so scripts can branch on the no-output-on-error
// invariant. 404 surfaces as a wrapped APIError with code "not_found".
func channelsRead(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("channels read", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: chatd channels read <name> <message-id>")
	}
	name, messageID := rest[0], rest[1]
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels read", err)
	}
	chans, err := client.ListChannels(ctx)
	if err != nil {
		return mapChannelError("channels read", err)
	}
	var found *goclient.Channel
	for i := range chans {
		if chans[i].Name == name {
			found = &chans[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("channels read: no channel named %q", name)
	}
	if err := client.MarkChannelRead(ctx, found.ID.String(), messageID); err != nil {
		return mapChannelError("channels read", err)
	}
	_, _ = fmt.Fprintln(env.Stdout, "ok")
	return nil
}

// mapChannelError attaches the subcommand label to a server error so the
// stderr message names the command and surfaces the typed APIError code
// where one is present. Non-API errors (transport, decode) pass through
// labelled but otherwise untouched.
func mapChannelError(label string, err error) error {
	var apiErr *goclient.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %s: %s", label, apiErr.Code, apiErr.Message)
	}
	return fmt.Errorf("%s: %w", label, err)
}
