package cmd

import (
	"flag"
	"strings"
)

// splitFlagsAndPositional walks args and partitions them into the
// slice that should be handed to fs.Parse (flag tokens plus their
// values) and the positional tokens that appeared interleaved. A `--`
// terminator forces every following token into the positional slice.
// Unknown flag names are passed through to fs.Parse so its own error
// surfaces with the standard "flag provided but not defined" message.
//
// stdlib flag.Parse stops at the first non-flag token, so commands
// that want to accept `chatd <cmd> <positional> --flag value` ordering
// run their args through this splitter first, hand the flag tail to
// fs.Parse, then take the positional slice as their own arguments.
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
