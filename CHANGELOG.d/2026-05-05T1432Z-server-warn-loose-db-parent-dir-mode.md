### Added

- `apps/server/internal/db`: `db.Open` now emits a soft `WARN` log line when the SQLite parent directory has loose POSIX mode (anything other than owner-only, i.e. `mode & 0077 != 0`). Recommended is `0700` per PRD §9 "Persistence hygiene". The warning is informational — the server continues to start. Windows builds compile against a no-op stub since NTFS lacks POSIX mode bits (#714).
