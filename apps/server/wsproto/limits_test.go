package wsproto

import (
	"fmt"
	"testing"
)

// TestMessageBodyLimitIsKiBMultiple guards the invariant documented on
// MessageBodyLimitCloseReason: the close-reason text formats the cap as
// "%d KiB" via integer division by 1024, which silently rounds for any
// cap that is not a clean KiB multiple. If a future change sets
// MessageBodyLimit to e.g. 5000, this test fails before the wire text
// can drift away from the actual byte cap.
func TestMessageBodyLimitIsKiBMultiple(t *testing.T) {
	if MessageBodyLimit%1024 != 0 {
		t.Fatalf("MessageBodyLimit=%d is not a multiple of 1024; "+
			"MessageBodyLimitCloseReason rounds via /1024 and would "+
			"print %q while the real cap is %d bytes",
			MessageBodyLimit,
			fmt.Sprintf("message body exceeds %d KiB limit", MessageBodyLimit/1024),
			MessageBodyLimit)
	}
}
