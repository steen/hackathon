### Fixed

- web: surface previously-swallowed `me()` rehydrate and `logout()` server failures via a global `<ErrorBanner />` mounted at the App shell. A user whose token rehydrate fails now sees the underlying reason instead of being silently bounced to login; a logout that the server does not acknowledge now leaves a dismissable notice while local state still clears. (#142)
