-- 0003_auth_events_kind_index: index `auth_events.kind` for audit-log queries
-- that filter by kind without a leading `user_id` predicate (e.g. the
-- `GROUP BY kind` aggregate and `WHERE kind = ?` counters used by the
-- audit-observability surface in PRD §9).
--
-- The pre-existing `idx_auth_events_user_at (user_id, at)` from 0001_init does
-- not cover those plans because `user_id` is not in the predicate, so the
-- planner falls back to a full table scan as the audit log grows. A standalone
-- index on `kind` keeps GROUP-BY-kind and kind-equality lookups index-only.
-- Filed against #599.

CREATE INDEX IF NOT EXISTS idx_auth_events_kind
    ON auth_events(kind);
