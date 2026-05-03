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
	return readLine(env.Stdin)
}

// readVisible is the non-secret counterpart to readSecret.
func readVisible(env *Env, value, prompt string) (string, error) {
	if value != "" {
		return value, nil
	}
	_, _ = fmt.Fprintf(env.Stderr, "%s: ", prompt)
	return readLine(env.Stdin)
}

// readLine returns the next \n-terminated line from r, with the
// trailing CR/LF trimmed. EOF before any data is returned as ""+nil.
func readLine(r io.Reader) (string, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
