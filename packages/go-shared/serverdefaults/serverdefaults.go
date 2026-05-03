// Package serverdefaults exposes constants that are shared by both the chat
// server and any client that needs to know where the server lives by default.
// Keeping these in one place avoids drift between server bind and client dial.
package serverdefaults

// Port is the default TCP port the chat server binds to and that clients dial
// when no override is provided.
const Port = 8080
