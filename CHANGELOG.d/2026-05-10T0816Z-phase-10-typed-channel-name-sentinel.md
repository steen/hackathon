- refactor(phase-10): replace handler substring-sniff with typed repo
  sentinels for channel name + id conflicts on the §10 atomic-bootstrap
  path. New `repo.CreateChannelTx` returns `ErrChannelNameTaken` /
  `ErrChannelIDTaken`; the http handler maps both to 409 via
  `errors.Is` and no longer reaches into SQLite error prose. Closes
  parity gap with `ErrChannelKeyAlreadyExists` /
  `ErrDMConversationKeyAlreadyExists`.
