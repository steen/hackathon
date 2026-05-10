### Tests

- Phase 10 channel-membership e2e: parameterize `registerFixture` seeds so alice and bob receive distinct identity keys, matching the key-wrapping fixture shape. Without distinct seeds, the L30 sender-pubkey check could not differentiate the two users; the negative case in `TestPrivateInviteRejectsSenderPubkeyMismatch` now exercises a real ownership mismatch instead of an arbitrary `0xFF` placeholder.
