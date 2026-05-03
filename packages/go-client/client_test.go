package goclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goclient "hackathon/packages/go-client"
)

// envelopeJSON is the canonical {ok,data,error} success body used in
// fixtures. data is whatever the test wants to embed.
func envelopeJSON(data string) string {
	return `{"ok":true,"data":` + data + `,"error":null}`
}

// envelopeError builds a failure envelope with a typed error body.
func envelopeError(code, message string) string {
	return `{"ok":false,"data":null,"error":{"code":"` + code + `","message":"` + message + `"}}`
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := goclient.New("http://example.invalid/")
	if got := c.BaseURL(); got != "http://example.invalid" {
		t.Fatalf("BaseURL = %q, want trailing slash trimmed", got)
	}
}

func TestSetTokenRoundTrip(t *testing.T) {
	c := goclient.New("http://example.invalid")
	if c.Token() != "" {
		t.Fatalf("fresh client should have empty token")
	}
	c.SetToken("abc")
	if got := c.Token(); got != "abc" {
		t.Fatalf("Token = %q, want abc", got)
	}
	c.SetToken("")
	if c.Token() != "" {
		t.Fatalf("SetToken(\"\") should clear")
	}
}

func TestWithTokenOption(t *testing.T) {
	c := goclient.New("http://example.invalid", goclient.WithToken("seed"))
	if got := c.Token(); got != "seed" {
		t.Fatalf("WithToken not applied: %q", got)
	}
}

func TestDoSendsBearerWhenSet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(envelopeJSON(`{"user":{"id":"u1","username":"alice"}}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL, goclient.WithToken("tok-123"))
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if gotAuth != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want Bearer tok-123", gotAuth)
	}
}

func TestDoOmitsBearerWhenUnset(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(envelopeJSON(`{"user":{"id":"u1","username":"alice"}}`)))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty when no token set", gotAuth)
	}
}

func TestDoSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(envelopeError("unauthorized", "missing token")))
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	_, err := c.Me(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *goclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("Status = %d, want 401", apiErr.Status)
	}
	if apiErr.Code != "unauthorized" || apiErr.Message != "missing token" {
		t.Fatalf("error fields = %+v", apiErr)
	}
	if !goclient.IsCode(err, "unauthorized") {
		t.Fatalf("IsCode helper should match")
	}
	if goclient.IsCode(err, "forbidden") {
		t.Fatalf("IsCode should not match unrelated code")
	}
}

func TestDoEmptyBodyOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := goclient.New(srv.URL)
	_, err := c.Me(context.Background())
	var apiErr *goclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Fatalf("Status = %d, want 502", apiErr.Status)
	}
}

func TestContextCancellationPropagates(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		_, _ = w.Write([]byte(envelopeJSON("null")))
	}))
	defer srv.Close()
	defer close(block)

	c := goclient.New(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Me(ctx)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context") && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v should mention context", err)
	}
}
