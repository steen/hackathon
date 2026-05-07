### Changed

- CI e2e job now runs inside the official `mcr.microsoft.com/playwright:v1.59.1-jammy` container instead of a bare `ubuntu-latest` runner. Playwright's system libraries and browser binaries are baked into the image, so the e2e job no longer apt-installs ~180 packages from `azure.archive.ubuntu.com` on every run — the mirror stalls that repeatedly tripped the 8min step timeout (#824) are off the critical path. The image tag tracks the resolved `@playwright/test` version in `pnpm-lock.yaml`; bumping that devDep requires bumping the tag in the same PR.
