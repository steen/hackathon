### Fixed
- Web: composer Send button no longer sits under the iOS home indicator on notched devices. `.composer` now adds `env(safe-area-inset-bottom)` on top of its existing `0.75rem` padding via `calc(0.75rem + env(safe-area-inset-bottom))`. `env()` resolves to `0px` outside a safe-area context, so desktop and non-notched mobile layouts are unchanged. Closes #614.
