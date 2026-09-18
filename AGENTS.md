# Repository Guide

## Scope

This is a Restish output formatter for readable trees and tables. Keep all
shape detection generic and deterministic.

## Constraints

- Do not add product-specific field names, fixtures, endpoints, or examples.
- Do not add acronym dictionaries. Derive display casing from source keys.
- Preserve JSON array order unless the user explicitly requests sorting.
- Keep separate record collections separate. Never create Cartesian products.
- Prefer standard library code and existing dependencies.
- Keep stdout reserved for Restish protocol messages.
- Do not commit generated plugin binaries.

## Verification

Run before committing:

```console
$ gofmt -w *.go
$ go mod tidy
$ go test ./...
$ go test -race ./...
$ go vet ./...
$ go build ./...
```

For formatter changes, also install the binary and exercise
`examples/response.json` through a local HTTP server.

Use Conventional Commit messages.
