- **db**: add forward migration `0003_auth_events_kind_index.sql` creating
  `idx_auth_events_kind ON auth_events(kind)` so audit-log queries that filter
  by `kind` alone (`GROUP BY kind`, `WHERE kind = ?`) stay index-covered as
  the table grows. Closes #599.
