package main

import (
	"net"
	"net/http"
	"testing"

	"hackathon/apps/server/internal/config"
)

func TestHasHealthProbeFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"empty", nil, false},
		{"unrelated", []string{"--log-level", "debug"}, false},
		{"exact", []string{"--health-probe"}, true},
		{"with equals", []string{"--health-probe=ignored"}, true},
		{"trailing", []string{"--log-level=info", "--health-probe"}, true},
		{"prefix-only is not a match", []string{"--health"}, false},
		{"single dash is not a match", []string{"-health-probe"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasHealthProbeFlag(tc.args); got != tc.want {
				t.Fatalf("hasHealthProbeFlag(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestProbePort(t *testing.T) {
	defaultPort := func() string {
		_, p, err := net.SplitHostPort(config.DefaultListenAddr)
		if err != nil {
			t.Fatalf("split default listen addr: %v", err)
		}
		return p
	}()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls back to default", "", defaultPort},
		{"loopback explicit", "127.0.0.1:9999", "9999"},
		{"public bind keeps port", "0.0.0.0:8081", "8081"},
		{"unparseable falls back to default", "not-a-host-port", defaultPort},
		{"missing port falls back to default", "127.0.0.1", defaultPort},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := probePort(tc.in); got != tc.want {
				t.Fatalf("probePort(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRunHealthProbe_Healthy boots an httptest server bound to 127.0.0.1
// on a kernel-assigned port, points CHAT_LISTEN_ADDR at it, and asserts
// runHealthProbe returns 0. We can't use httptest.NewServer's URL
// directly because runHealthProbe always dials 127.0.0.1 (correct
// behaviour for in-container probes), so we construct a listener
// explicitly to control the host.
func TestRunHealthProbe_Healthy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	t.Setenv(config.EnvListenAddr, ln.Addr().String())
	if rc := runHealthProbe(); rc != 0 {
		t.Fatalf("runHealthProbe() = %d against running server, want 0", rc)
	}
}

// TestRunHealthProbe_Non200 covers the non-OK status path: a /healthz
// returning 503 must fail the probe so docker marks the container
// unhealthy.
func TestRunHealthProbe_Non200(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	t.Setenv(config.EnvListenAddr, ln.Addr().String())
	if rc := runHealthProbe(); rc != 1 {
		t.Fatalf("runHealthProbe() = %d against 503 server, want 1", rc)
	}
}

// TestRunHealthProbe_Unreachable boots no server: the probe must fail
// fast (within healthProbeTimeout) with exit 1.
func TestRunHealthProbe_Unreachable(t *testing.T) {
	// Find a free port, then close the listener so nothing answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	t.Setenv(config.EnvListenAddr, addr)
	if rc := runHealthProbe(); rc != 1 {
		t.Fatalf("runHealthProbe() = %d against closed port, want 1", rc)
	}
}
