//go:build !panicprobe

package wiring

import "net/http"

// registerPanicProbe is the no-op default. The real implementation
// lives in panicprobe.go behind //go:build panicprobe; production
// builds compile this file instead so /debug/panic is unreachable.
func registerPanicProbe(_ *http.ServeMux) {}
