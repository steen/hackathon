### Fixed

- `apps/cli/cmd/history`: accept `chatd history <channel> --limit N` and `chatd history <channel> --before ID` (the call shape the usage string already documented). Stdlib `flag.Parse` stops at the first non-flag token, so the AC-documented order had been rejected with the usage error. The impl now splits the args into a flag slice and a positional slice before handing the flag slice to `flag.Parse`. The old `chatd history --limit N <channel>` order keeps working. Closes #113.
