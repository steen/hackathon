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
// every channel one per line as `<id>\t<name>`. With `create` or `rename`
// it dispatches to the matching sub-handler.
func Channels(ctx context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "create":
			return channelsCreate(ctx, env, args[1:])
		case "rename":
			return channelsRename(ctx, env, args[1:])
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: chatd channels create <name>")
	}
	name := rest[0]
	if !channelNameRe.MatchString(name) {
		return fmt.Errorf("channels create: invalid name %q: must be 1-40 chars of lowercase letters, digits, hyphens; must start with a letter or digit", name)
	}
	client, _, err := newClient(env, true)
	if err != nil {
		return wrapNotLoggedIn("channels create", err)
	}
	ch, err := client.CreateChannel(ctx, name)
	if err != nil {
		return mapChannelError("channels create", err)
	}
	_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\n", ch.ID, ch.Name)
	return nil
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
