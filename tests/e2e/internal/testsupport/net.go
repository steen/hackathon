package testsupport

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
	"time"
)

// RandomSecret returns a hex string of byteLen random bytes (so the
// printed string is 2*byteLen chars). Per CLAUDE.md "No hardcoded
// secrets" — every JWT secret and invite code in the e2e tree is
// process-local.
func RandomSecret(t *testing.T, byteLen int) string {
	t.Helper()
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

// FreePort asks the kernel for a free TCP port on loopback and returns
// it. There is a tiny race window between Close and the spawned
// server's Listen. Callers running in parallel risk landing on the
// same port; until issue #196's FD-handoff lands, the e2e packages
// that use this helper run serially.
func FreePort(t *testing.T) int {
	t.Helper()
	// noctx: net.Listen has no context-taking variant pre-1.20+; the
	// listener is opened and closed synchronously inside this call.
	l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx // see comment
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// WaitForPort polls 127.0.0.1:port until it accepts a TCP connection
// or timeout elapses. Returns nil on success, an error describing the
// timeout otherwise. Callers typically t.Fatalf on the error.
func WaitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		// noctx: poll loop bounds wait via the outer deadline; per-dial
		// timeout is the only knob this helper needs.
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond) //nolint:noctx // see comment
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", addr)
}
