### Fixed

- Web: `<ErrorBanner />` is now mounted during the auth-loading state, so any future error-path that sets `error` while `loading` is still `true` will render. No behavior change for current callsites — `me()` rejection already flips `loading:false` + `error:<msg>` atomically. (#435)
