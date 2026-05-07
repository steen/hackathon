### Removed

- Deleted `packages/scaffold-stub/`, an empty no-op workspace package with no consumers. Regenerated `pnpm-lock.yaml` to drop the entry. Trims one workspace traversal from every `pnpm install`.
- Dropped the matching `COPY packages/scaffold-stub/package.json` line from `Dockerfile` so the chat-server image build no longer references the deleted package.
