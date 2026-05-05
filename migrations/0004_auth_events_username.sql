-- 0004_auth_events_username: add a nullable `username` column to auth_events
-- so failed-login attempts against an unknown username remain attributable.
--
-- Before this migration, a login_failure for an unknown user wrote a row
-- with `user_id` NULL and no other linkage to the attempted username, so
-- the audit log could not answer "which usernames are being probed?". The
-- column is nullable because (a) older rows have no value to backfill,
-- (b) callers without a username in scope (e.g. some rate_limited paths)
-- pass empty string and we record NULL, and (c) the audit log is
-- forward-only — no attempt to reconstruct historical usernames.

ALTER TABLE auth_events ADD COLUMN username TEXT;
