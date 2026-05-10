### Refactored

- `authStore.CreateUser`'s registration auto-join now calls `repo.InsertChannelMemberTx` instead of inlining its own `INSERT INTO channel_members`, so the L33 NULL-signature carve-out for public channels lives in one place. `AuthDeps` gained a `Repo *repo.Repo` field that the wiring file populates from `deps.Repo`. (#1003)
