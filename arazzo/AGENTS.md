# Repository Guide

## Scope

This is a Go command plugin for running Arazzo workflows through Restish. Keep
the implementation aligned with the workflow contract in `README.md`.

## Constraints

- Keep every Go file, including tests, at 100 lines or fewer.
- Prefer the standard library and existing dependencies.
- Send all API requests through `plugin.CommandClient`; Restish owns HTTP,
  authentication, TLS, retries, middleware, profiles, and output overrides.
- Treat each Arazzo source name as the matching Restish API name.
- Preserve source-qualified `operationPath` routing across multiple APIs.
- Preserve `RSH_OUTPUT_FORMAT` as an override for custom table output.
- Do not commit generated plugin binaries.

## Layout

- `workflow.go`: command handling and workflow execution.
- `sources.go`: Restish API loading and Arazzo source mapping.
- `executor.go`: Arazzo operation execution through Restish.
- `operation.go`: OpenAPI operation lookup and parameter metadata.
- `operation_id.go`: source-qualified operation ID lookup.
- `operation_path.go`: source-qualified operation path lookup.
- `parameters.go`: path, query, and header serialization.
- `locations.go`: workflow-to-OpenAPI parameter location checks.
- `values.go`: primitive and array parameter values.
- `decode.go`: recursive runtime value decoding.
- `result.go`: workflow output decoding and fallback.
- `selection.go`: API-backed workflow input selection.
- `choices.go`: selector response validation and rendering.
- `table.go`: boxed single-object table rendering.
- `*_test.go`: focused behavior checks and the 100-line guard.

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

For command or protocol changes, also install the built plugin into an isolated
Restish config and run a workflow against local fake APIs. Verify exact request
routing, cross-step values, and rendered output.

Use Conventional Commit messages.
