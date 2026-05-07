package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"hackathon/apps/server/internal/config"
)

// healthProbeFlag is the long-form flag that triggers the in-image
// liveness probe. The Dockerfile's HEALTHCHECK and the compose
// healthcheck: block both invoke "/chat-server --health-probe", so the
// chosen literal must match those two call sites verbatim.
const healthProbeFlag = "--health-probe"

// healthProbeTimeout bounds the probe's total wall-clock time. Issue
// #796 caps the probe at 2s; the docker HEALTHCHECK declares a 3s
// timeout above that, so 1.5s leaves headroom for OCI runtime overhead
// without ever racing the container-level deadline.
const healthProbeTimeout = 1500 * time.Millisecond

// hasHealthProbeFlag reports whether the user invoked the binary with
// --health-probe (or its = / space-separated variants we accept). Kept
// allocation-free so it can run before config.Load() without dragging
// in env var or logger state.
func hasHealthProbeFlag(args []string) bool {
	for _, a := range args {
		if a == healthProbeFlag || strings.HasPrefix(a, healthProbeFlag+"=") {
			return true
		}
	}
	return false
}

// runHealthProbe performs a GET against /healthz on the loopback
// interface and returns 0 if the server replies 200, 1 otherwise. The
// probe deliberately does not call config.Load(): a probe that
// crashes on missing env vars (CHAT_JWT_SECRET, CHAT_INVITE_CODE) is
// useless to the docker healthcheck, which runs in the same env as
// the server but should not be coupled to its validation rules.
//
// Port resolution mirrors the server's own bind logic in spirit but
// not in code: read CHAT_LISTEN_ADDR, fall back to the default. We
// always dial 127.0.0.1 regardless of the configured host — the probe
// runs inside the same container/network namespace as the server, so
// loopback is the right target even when the server binds 0.0.0.0.
func runHealthProbe() int {
	port := probePort(os.Getenv(config.EnvListenAddr))
	url := fmt.Sprintf("http://127.0.0.1:%s/healthz", port)

	ctx, cancel := context.WithTimeout(context.Background(), healthProbeTimeout)
	defer cancel()

	// gosec G107/G704 false positive: the URL host is the hardcoded
	// loopback literal and the path is constant. The only env-derived
	// component is the port, which is extracted via net.SplitHostPort
	// and falls back to the default when unparseable. No user input
	// enters the request, so there is no SSRF vector.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // see comment above
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-probe: build request: %v\n", err)
		return 1
	}
	client := &http.Client{Timeout: healthProbeTimeout}
	resp, err := client.Do(req) //nolint:gosec // see comment on http.NewRequestWithContext above
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-probe: %v\n", err)
		return 1
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health-probe: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// probePort extracts the port from a CHAT_LISTEN_ADDR value, falling
// back to the same default the server uses (config.DefaultListenAddr)
// when the env var is unset or unparseable. We accept unparseable
// values silently rather than failing the probe — a malformed env
// causes the server itself to refuse to start, which the probe will
// then report as a connection error against the default port.
func probePort(listenAddr string) string {
	if listenAddr == "" {
		listenAddr = config.DefaultListenAddr
	}
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		_, port, _ = net.SplitHostPort(config.DefaultListenAddr)
	}
	return port
}
