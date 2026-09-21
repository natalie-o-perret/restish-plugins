# restish-plugin-pretty

[![CI](https://github.com/natalie-o-perret/restish-plugins/actions/workflows/ci.yml/badge.svg)](https://github.com/natalie-o-perret/restish-plugins/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/natalie-o-perret/restish-plugins)](LICENSE)

`restish-plugin-pretty` builds the `restish-pretty` plugin, which adds the
`pretty` output formatter to [Restish](https://rest.sh/). It renders structured
response bodies as deterministic trees and tables without endpoint-specific
configuration.

## Install

```console
$ go build -o restish-pretty .
$ restish plugin install ./restish-pretty --yes
Plugin source: ./restish-pretty
Resolved path: ./restish-pretty
Manifest: pretty 0.1.0
Capabilities: formatter, formatter(pretty)
warning: installed plugins are trusted executables and may run arbitrary code on future restish invocations
Installed plugin restish-pretty
```

Confirm that Restish discovered the formatter:

```console
$ restish plugin list
pretty               0.1.0      capabilities: formatter, formatter(pretty)
  formatters: pretty
```

## Use

```console
$ restish get https://api.example.test/catalog -o pretty --rsh-print b
```

`-o pretty` selects the formatter. `--rsh-print b` prints only the rendered
response body and omits HTTP headers.

## Example

Given this response body:

```json
{
  "catalog": {
    "name": "Example Press"
  },
  "publications": [
    {
      "id": "pub-001",
      "title": "Field Notes",
      "file_bytes": 8388608
    },
    {
      "id": "pub-002",
      "title": "Short Stories",
      "file_bytes": 4194304
    }
  ]
}
```

`pretty` renders:

```text
Details
├── Catalog
│   └── Name: Example Press
├──────────────────────────────────────╮
│ Publications                         │
├────────────┬─────────┬───────────────┤
│ File Bytes │ Id      │ Title         │
├────────────┼─────────┼───────────────┤
│      8 MiB │ pub-001 │ Field Notes   │
├────────────┼─────────┼───────────────┤
│      4 MiB │ pub-002 │ Short Stories │
╰────────────┴─────────┴───────────────╯
```

## Rendering Rules

- Objects become tree branches and scalar fields become leaves.
- Arrays of flat objects become titled tables.
- Arrays containing nested objects become ordered `Item N` branches.
- Scalar arrays remain complete and wrap across table lines when needed.
- Multiple record collections render separately and are never flattened
  together.
- Empty arrays and objects render as `[]` and `{}`.
- A single root object wrapper and an object-valued `body` envelope are
  collapsed when this cannot overwrite sibling fields.
- Object keys render deterministically with scalar fields before nested fields,
  then alphabetically within each group.
- Array order is preserved.
- Tables and tree values wrap to the current terminal width. Redirected output
  keeps its natural width.
- Snake case, spinal case, camel case, Pascal case, and source acronyms are
  converted to readable titles without a field-name dictionary.
- Numeric `bytes` and `*_bytes` fields use IEC units. `memory` and `*_memory`
  use IEC units only when every value looks like a byte count.
- Values that do not fit these shapes fall back to indented JSON.

Formatter plugins receive decoded bodies and response headers, but not the
OpenAPI response schema or operation metadata. Unit detection therefore stays
deliberately conservative.

## Demo

Serve the included response fixture from the repository root:

```console
$ python3 -m http.server 8765 --bind 127.0.0.1
```

In another terminal:

```console
$ restish get http://127.0.0.1:8765/examples/response.json -o pretty --rsh-print b
```

The fixture covers root metadata, nested objects, independent record arrays,
nested child tables, scalar lists, empty collections, identifier casing, and
byte formatting.

## Compatibility

- Go 1.25.7 or newer is required to build the plugin.
- The manifest targets Restish plugin API v2.
- The module uses Restish v2.3.0 and go-pretty v6.8.3.
- Restish plugins are trusted local executables and are not sandboxed.

## Development

Follow the official [Plugin Quickstart](https://rest.sh/docs/plugins/quickstart/).

```console
$ gofmt -w *.go
$ go mod tidy
$ go test ./...
$ go test -race ./...
$ go vet ./...
$ go build ./...
```

Inspect the plugin manifest as decoded JSON:

```console
$ go build -o restish-pretty .
$ restish plugin debug ./restish-pretty -- --rsh-plugin-manifest
```
