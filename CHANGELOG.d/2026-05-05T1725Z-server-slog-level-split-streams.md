- Server logger now routes records below `slog.LevelWarn` to `os.Stdout`
  and warn/error records to `os.Stderr` (typical operator convention).
  `CHAT_LOG_LEVEL` continues to gate emission on both streams; the split
  decides only which stream a record lands on, never whether it is
  emitted. Operators redirecting `2>` will now see warn+ records there.
