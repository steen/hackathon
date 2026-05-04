// Package wsproto holds WebSocket protocol constants that need to be
// referenced from outside apps/server/internal — specifically the
// black-box e2e tests under tests/e2e/, which Go's internal-package
// rule blocks from importing apps/server/internal/wsapi directly.
//
// Keep this package small and dependency-free: only constants whose
// wire shape is observable from a non-internal caller belong here.
// Behaviour stays in internal/wsapi, which re-exports any constants
// it forwards from this package.
package wsproto

import "fmt"

// MessageBodyLimit caps the decoded chat-message body in bytes
// (PRD §9, SEC-8). Enforced on inbound WS frames in
// apps/server/internal/wsapi; mirrored by the REST path in
// apps/server/internal/http.
const MessageBodyLimit = 4 * 1024

// MessageBodyLimitCloseReason is the WebSocket close-reason text the
// server emits when an inbound frame's decoded body exceeds the
// chat-message body cap (PRD §9, SEC-8). Both close code 1009 paths
// (this body cap and the 64 KiB read-limit cap in wsapi) share the
// status code; the e2e frame-size test asserts on this exact reason
// to disambiguate which path fired.
//
// Derived from MessageBodyLimit so the wire text cannot drift if the
// cap value ever changes.
var MessageBodyLimitCloseReason = fmt.Sprintf("message body exceeds %d KiB limit", MessageBodyLimit/1024)
