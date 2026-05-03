package cmd

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	goclient "hackathon/packages/go-client"
)

// History implements `chatd history <channel> [--limit N] [--before ID]`.
// Prints messages newest-first as `<rfc3339>\t<sender>\t<body>`. Body
// is single-line by contract (server rejects newlines), so this stays
// grep-friendly.
func History(ctx context.Context, env *Env, args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	limit := fs.Int("limit", 0, "max messages to return (0 = server default)")
	before := fs.String("before", "", "ULID cursor; only return messages older than this id")
	// stdlib flag.Parse stops at the first non-flag token, so the
	// AC-documented `chatd history <channel> --limit N` order would
	// otherwise leave --limit in fs.Args(). Split first, parse the
	// flag tail, then assign the positional ourselves.
	flagArgs, positional := splitFlagsAndPositional(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return fmt.Errorf("usage: chatd history <channel> [--limit N] [--before ID]")
	}
	channel := positional[0]
	client, _, err := newClient(env, true)
	if err != nil {
		return err
	}
	msgs, err := client.ListMessages(ctx, channel, goclient.ListMessagesOptions{
		Limit:  *limit,
		Before: *before,
	})
	if err != nil {
		return err
	}
	for _, m := range msgs {
		_, _ = fmt.Fprintf(env.Stdout, "%s\t%s\t%s\n",
			m.CreatedAt.UTC().Format(time.RFC3339Nano), m.SenderUserID, m.Body)
	}
	return nil
}

// splitFlagsAndPositional walks args and partitions them into the
// slice that should be handed to fs.Parse (flag tokens plus their
// values) and the positional tokens that appeared interleaved. A `--`
// terminator forces every following token into the positional slice.
// Unknown flag names are passed through to fs.Parse so its own error
// surfaces with the standard "flag provided but not defined" message.
func splitFlagsAndPositional(fs *flag.FlagSet, args []string) (flagArgs, positional []string) {
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "--" {
			positional = append(positional, args[i+1:]...)
			return flagArgs, positional
		}
		if !isFlagToken(tok) {
			positional = append(positional, tok)
			continue
		}
		flagArgs = append(flagArgs, tok)
		// `--name=value` carries its value in-band.
		name := strings.TrimLeft(tok, "-")
		if strings.Contains(name, "=") {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			// Let fs.Parse produce the canonical error on the unknown
			// flag; consume the next token only if it itself is not a
			// flag, to avoid eating a sibling flag.
			if i+1 < len(args) && !isFlagToken(args[i+1]) {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		if isBoolFlag(f) {
			continue
		}
		if i+1 < len(args) {
			flagArgs = append(flagArgs, args[i+1])
			i++
		}
	}
	return flagArgs, positional
}

func isFlagToken(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	if s == "--" {
		return false
	}
	return true
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}
