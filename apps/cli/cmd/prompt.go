package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// readSecret returns value when non-empty; otherwise prompts on stderr
// and reads a line from env.Stdin. We do not toggle terminal echo —
// the chatd PRD's threat model is friend-group scale and the README
// documents that automation should pass --password / --invite-code
// (or the CHAT_PASSWORD / CHAT_INVITE_CODE env vars).
func readSecret(env *Env, value, prompt string) (string, error) {
	if value != "" {
		return value, nil
	}
	if env.Stdin == nil {
		return "", fmt.Errorf("no stdin to read %s from", prompt)
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s: ", prompt)
	return readLine(env)
}

// readVisible is the non-secret counterpart to readSecret.
func readVisible(env *Env, value, prompt string) (string, error) {
	if value != "" {
		return value, nil
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s: ", prompt)
	return readLine(env)
}

// readLine returns the next \n-terminated line from env.Stdin via a
// cached *bufio.Reader. Caching matters: bufio reads up to 4 KiB
// eagerly, so a fresh wrapper per call would drop already-buffered
// bytes from the second prompt onward when stdin is a scripted pipe.
// EOF before any data is returned as ""+nil.
func readLine(env *Env) (string, error) {
	if env.stdinReader == nil {
		if br, ok := env.Stdin.(*bufio.Reader); ok {
			env.stdinReader = br
		} else {
			env.stdinReader = bufio.NewReader(env.Stdin)
		}
	}
	line, err := env.stdinReader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
