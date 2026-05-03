package wsapi

// Test-only seams for gap-D F5: production code never calls these.
// File suffix _test.go keeps them out of production builds; the in-package
// handler_test.go uses them to assert the per-conn state the gap-D commit
// binds at upgrade time.

// userIDForTesting exposes the unexported userID field on connSubscriber.
func (c *connSubscriber) userIDForTesting() string { return c.userID }

// channelForTesting exposes the unexported channel field on connSubscriber.
func (c *connSubscriber) channelForTesting() string { return c.channel }
