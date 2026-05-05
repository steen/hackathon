### Changed

- `auth_events` audit log now records the attempted username on every row. Failed logins against an unknown username (where `user_id` is NULL because the user does not exist) carry the probed username in the new `username` column so audit queries can attribute the attempt. Successful kinds (`register`, `login_success`, `ws_ticket_issued`, `logout`) also carry the resolved username so flat audit queries no longer need a join on `users`. Schema change: additive, nullable column added by migration `0004_auth_events_username.sql` (PRD §9 / #716).
