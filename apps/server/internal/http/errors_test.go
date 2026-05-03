package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// test_error_envelope_shape_is_consistent — every JSON response carries the
// three keys ok, data, error with the documented null/non-null pairing.
func TestErrorEnvelopeShapeIsConsistent(t *testing.T) {
	t.Run("ok shape", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteOK(rec, map[string]string{"hello": "world"})

		got := decodeRaw(t, rec)
		assertKeys(t, got)

		if got["ok"] != true {
			t.Fatalf("ok = %v, want true", got["ok"])
		}
		if got["error"] != nil {
			t.Fatalf("error = %v, want nil", got["error"])
		}
		if got["data"] == nil {
			t.Fatalf("data is nil; want non-nil payload")
		}
	})

	t.Run("ok with nil data still ships data key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteOK(rec, nil)

		got := decodeRaw(t, rec)
		assertKeys(t, got)
		if got["ok"] != true {
			t.Fatalf("ok = %v, want true", got["ok"])
		}
		if got["error"] != nil {
			t.Fatalf("error = %v, want nil", got["error"])
		}
		// data must be the JSON null literal — assertKeys already proved
		// the key is present.
		if got["data"] != nil {
			t.Fatalf("data = %v, want nil", got["data"])
		}
	})

	t.Run("error shape", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteError(rec, "bad_request", "missing field foo", http.StatusBadRequest)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		got := decodeRaw(t, rec)
		assertKeys(t, got)

		if got["ok"] != false {
			t.Fatalf("ok = %v, want false", got["ok"])
		}
		if got["data"] != nil {
			t.Fatalf("data = %v, want nil", got["data"])
		}
		errObj, ok := got["error"].(map[string]interface{})
		if !ok {
			t.Fatalf("error = %v, want object", got["error"])
		}
		if errObj["code"] != "bad_request" {
			t.Fatalf("error.code = %v, want bad_request", errObj["code"])
		}
		if errObj["message"] != "missing field foo" {
			t.Fatalf("error.message = %v", errObj["message"])
		}
	})
}

func TestRequestIDContextRoundTrip(t *testing.T) {
	if got := RequestID(context.Background()); got != "" {
		t.Fatalf("RequestID(empty ctx) = %q, want \"\"", got)
	}
	ctx := WithRequestID(context.Background(), "abc123")
	if got := RequestID(ctx); got != "abc123" {
		t.Fatalf("RequestID = %q, want abc123", got)
	}
}

func decodeRaw(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, rec.Body.String())
	}
	return got
}

// assertKeys verifies all three envelope keys are physically present in the
// JSON object (not merely zero-valued in Go after Unmarshal). This catches
// regressions to a {error}-only envelope shape.
func assertKeys(t *testing.T, got map[string]interface{}) {
	t.Helper()
	for _, k := range []string{"ok", "data", "error"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("envelope missing key %q; got %v", k, got)
		}
	}
}
