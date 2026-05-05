package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLeftmostForwardedFor(t *testing.T) {
	tests := []struct {
		name   string
		header string // empty string → don't set the header at all
		setHdr bool
		want   string
	}{
		{name: "missing header", setHdr: false, want: ""},
		{name: "empty header", setHdr: true, header: "", want: ""},
		{name: "single hop", setHdr: true, header: "1.2.3.4", want: "1.2.3.4"},
		{name: "multi-hop, leftmost wins", setHdr: true, header: "1.2.3.4, 5.6.7.8", want: "1.2.3.4"},
		{name: "leading whitespace trimmed", setHdr: true, header: "  1.2.3.4  ,  5.6.7.8", want: "1.2.3.4"},
		{name: "ipv6 bracketed", setHdr: true, header: "[2001:db8::1], 5.6.7.8", want: "2001:db8::1"},
		{name: "ipv6 bare", setHdr: true, header: "2001:db8::1, 5.6.7.8", want: "2001:db8::1"},
		{name: "garbage leftmost rejected", setHdr: true, header: "not-an-ip, 1.2.3.4", want: ""},
		{name: "lone comma", setHdr: true, header: ",1.2.3.4", want: ""},
		{name: "host:port leftmost rejected", setHdr: true, header: "1.2.3.4:5678, 5.6.7.8", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tt.setHdr {
				req.Header.Set("X-Forwarded-For", tt.header)
			}
			got := LeftmostForwardedFor(req)
			if got != tt.want {
				t.Fatalf("LeftmostForwardedFor(%q): got %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// LeftmostForwardedFor must be safe to call on a nil request — defensive
// guard that lets call sites avoid a nil check when they happen to be
// in a path with an exotic test fixture.
func TestLeftmostForwardedFor_NilRequest(t *testing.T) {
	if got := LeftmostForwardedFor(nil); got != "" {
		t.Fatalf("LeftmostForwardedFor(nil): got %q, want empty", got)
	}
}
