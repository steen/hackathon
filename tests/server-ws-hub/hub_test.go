package server_ws_hub_test

import "testing"

// These tests anchor the AC IDs from
// specs/plans/phase-0/feature-server-ws-hub.md.
//
// They are skipped while apps/server is a stub (only doc.go). Once the
// implementation lands (apps/server/main.go, apps/server/internal/hub),
// remove the t.Skip and replace the body with the real assertion.
//
// See specs/test-analysis/phase-0/server-ws-hub.md for guidance.

func TestAC1_ServerWsHub_WsEndpointAccepts101Upgrade(t *testing.T) {
	t.Skip("AC-1 server-ws-hub: deferred — apps/server is stub; see specs/test-analysis/phase-0/server-ws-hub.md")
}

func TestAC2_ServerWsHub_HubBroadcastsToAllSubscribers(t *testing.T) {
	t.Skip("AC-2 server-ws-hub: deferred — hub package not yet implemented")
}

func TestAC3_ServerWsHub_HubBroadcastReachesEverySubscriberOfChannel(t *testing.T) {
	t.Skip("AC-3 server-ws-hub: deferred — hub package not yet implemented")
}

func TestAC4_ServerWsHub_ServerListensOnConfigurablePort(t *testing.T) {
	t.Skip("AC-4 server-ws-hub: deferred — apps/server has no main")
}

func TestAC5_ServerWsHub_NoAuthRequiredOnUpgrade(t *testing.T) {
	t.Skip("AC-5 server-ws-hub: deferred — apps/server is stub")
}
