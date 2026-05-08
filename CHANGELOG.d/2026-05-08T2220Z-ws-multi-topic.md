### Changed

- `apps/server/internal/wsapi`: every authenticated `/ws` upgrade now subscribes the connection to TWO Hub topics — the channel topic (unchanged) AND a per-viewer inbox topic `user:<viewer>` derived from the redeemed ws-ticket. `connSubscriber.channel string` becomes `channels []string` and the close path unsubscribes from every topic in the slice. Unauthenticated test paths (no `*auth.TicketStore`) preserve the single-topic legacy shape so an empty-id `user:` topic is never registered. Decision log §10 / L15.
