### Removed

- Deleted `packages/scaffold-stub/`, an empty no-op workspace package with no consumers. Regenerated `pnpm-lock.yaml` to drop the entry. Trims one workspace traversal from every `pnpm install`.
