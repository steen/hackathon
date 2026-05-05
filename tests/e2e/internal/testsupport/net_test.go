package testsupport_test

import (
	"fmt"
	"net"
	"testing"
	"time"

	"hackathon/tests/e2e/internal/testsupport"
)

func TestFreePortAndWaitForPort(t *testing.T) {
	port := testsupport.FreePort(t)
	if port <= 0 {
		t.Fatalf("FreePort returned non-positive %d", port)
	}

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen :%d: %v", port, err)
	}
	t.Cleanup(func() { _ = l.Close() })

	if err := testsupport.WaitForPort(port, 2*time.Second); err != nil {
		t.Fatalf("WaitForPort with active listener: %v", err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	// FreePort below picks a port the kernel currently considers
	// unbound; the just-closed `port` may still be in TIME_WAIT and
	// briefly accept connections on macOS.
	dead := testsupport.FreePort(t)
	if err := testsupport.WaitForPort(dead, 200*time.Millisecond); err == nil {
		t.Fatalf("WaitForPort on unbound :%d: expected timeout, got nil", dead)
	}
}

func TestRandomSecretLength(t *testing.T) {
	for _, n := range []int{1, 8, 32} {
		s := testsupport.RandomSecret(t, n)
		if len(s) != n*2 {
			t.Errorf("RandomSecret(%d): len=%d, want %d", n, len(s), n*2)
		}
	}
}

func TestRepoRoot(t *testing.T) {
	root := testsupport.RepoRoot(t)
	if root == "" {
		t.Fatal("RepoRoot returned empty")
	}
}
