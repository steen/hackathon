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

// MessageBodyLimitCloseReason is the WebSocket close-reason text the
// server emits when an inbound frame's decoded body exceeds the 4 KiB
// chat-message body cap (PRD §9, SEC-8). Both close code 1009 paths
// (this body cap and the 64 KiB read-limit cap in wsapi) share the
// status code; the e2e frame-size test asserts on this exact reason
// to disambiguate which path fired.
const MessageBodyLimitCloseReason = "message body exceeds 4 KiB limit"
