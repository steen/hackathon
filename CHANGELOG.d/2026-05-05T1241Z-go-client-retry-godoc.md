### Docs

- `packages/go-client`: move the `Retry/backoff` paragraph into the package doc so it renders on `go doc` / pkg.go.dev. The block existed since #681 but sat between imports and `const DefaultTimeout`, unattached to any declaration, so godoc dropped it.
