package body_and_ws_caps_e2e_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hackathon/apps/server/wsproto"
)

// AC-1: WebSocket reads are capped at 64 KiB per frame; oversized
// frames close the connection with a policy-violation code.
//
// PRD §11 SEC-6 specifies the wire close code as 1009 (RFC 6455
// §7.4.1 StatusMessageTooBig). Both `coder/websocket` and the
// gorilla library emit 1009 when a peer's frame exceeds the value
// passed to SetReadLimit.
//
// Two assertions in one test, against the real chat-server binary:
//
//   - over_limit: a frame of 64*1024 + 1 bytes triggers a close with
//     close code 1009. This is the SEC-6 enforcement.
//
//   - at_limit: a frame of 64*1024 bytes does NOT trigger the read-
//     limit close. The handler still closes (because the same frame
//     trips AC-3's 4 KiB body cap — also code 1009 — see
//     apps/server/internal/wsapi/handler.go), but with a distinct
//     close reason ("message body exceeds 4 KiB limit"). Asserting
//     the *reason* differentiates the two close paths and proves the
//     read-limit cap is positioned at >= 64 KiB, not lower.
//
// Without the at_limit assertion, the test would pass even if the
// server's read limit were set to 8 KiB — both 8 KiB+1 and 64 KiB+1
// would close with 1009 (8 KiB+1 via read-limit; 64 KiB+1 via the
// library's behavior on the wire). The reason-text differentiation
// is what pins the cap to the spec value.
func TestAC1_WSFrameOver64KiBClosesConnection(t *testing.T) {
	srv := startServer(t)

	t.Run("over_limit_closes_1009", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, srv.wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
		defer func() { _ = conn.CloseNow() }()
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
		}
		// Lift the client read limit so a long server-side close
		// reason is never truncated before we observe the code.
		conn.SetReadLimit(-1)

		oversize := bytes.Repeat([]byte("x"), 64*1024+1)
		// Write may succeed (frame goes onto the wire before the
		// server reacts) or fail (server closed mid-write). Both are
		// consistent with the AC; the assertion lives on the next Read.
		_ = conn.Write(ctx, websocket.MessageBinary, oversize)

		_, _, err = conn.Read(ctx)
		if err == nil {
			t.Fatalf("oversize frame: Read returned no error; want CloseError(1009)")
		}
		got := websocket.CloseStatus(err)
		if got != websocket.StatusMessageTooBig {
			t.Fatalf("oversize frame: close code = %d, want %d (StatusMessageTooBig / 1009); err=%v",
				got, websocket.StatusMessageTooBig, err)
		}
		// Pin the wire value: PRD §11 SEC-6 names the code 1009. A
		// future library re-numbering would otherwise drift silently.
		if int(websocket.StatusMessageTooBig) != 1009 {
			t.Fatalf("library StatusMessageTooBig is not 1009: got %d", websocket.StatusMessageTooBig)
		}
	})

	t.Run("at_limit_64KiB_not_closed_by_read_limit", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, srv.wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(-1)

		atLimit := bytes.Repeat([]byte("x"), 64*1024)
		if err := conn.Write(ctx, websocket.MessageBinary, atLimit); err != nil {
			t.Fatalf("write at-limit frame: %v", err)
		}

		// The server emits a typed PRD §10 error frame before the close
		// when the body-cap path fires. Drain that text frame first; the
		// close arrives on the next Read.
		mt, _, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("at-limit frame: expected error frame before close, got err=%v", err)
		}
		if mt != websocket.MessageText {
			t.Fatalf("at-limit frame: pre-close frame type = %v, want MessageText", mt)
		}

		_, _, err = conn.Read(ctx)
		if err == nil {
			t.Fatalf("at-limit frame: Read returned no error; expected close from body-cap path")
		}
		var ce websocket.CloseError
		if !errors.As(err, &ce) {
			t.Fatalf("at-limit frame: expected websocket.CloseError, got %T: %v", err, err)
		}
		// Same code (1009) — the body cap and the read-limit cap
		// share it. The Reason text disambiguates: the handler emits
		// "message body exceeds 4 KiB limit" for the body cap; the
		// library emits an empty/different reason for the read-limit
		// path.
		if ce.Code != websocket.StatusMessageTooBig {
			t.Fatalf("at-limit frame: close code = %d, want %d", ce.Code, websocket.StatusMessageTooBig)
		}
		if ce.Reason != wsproto.MessageBodyLimitCloseReason {
			t.Fatalf("at-limit frame: close reason = %q; want %q (proves read-limit at 64 KiB did not fire)", ce.Reason, wsproto.MessageBodyLimitCloseReason)
		}
	})
}
