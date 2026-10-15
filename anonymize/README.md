# restish-plugin-anonymize

[![CI](https://github.com/natalie-o-perret/restish-plugins/actions/workflows/ci.yml/badge.svg)](https://github.com/natalie-o-perret/restish-plugins/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/natalie-o-perret/restish-plugins)](LICENSE)

`restish-plugin-anonymize` builds the `restish-anonymize` response middleware
for [Restish](https://rest.sh/). It applies
[`go-data/anonymize`](https://pkg.go.dev/github.com/natalie-o-perret/go-data/anonymize)
before Restish renders decoded responses, so the same transformed data reaches
JSON and formatter plugins such as `pretty`.

## Install

```console
$ go build -o restish-anonymize .
$ restish plugin install ./restish-anonymize --yes
Plugin source: ./restish-anonymize
Resolved path: ./restish-anonymize
Manifest: anonymize 0.1.0
Capabilities: response-middleware
warning: installed plugins are trusted executables and may run arbitrary code on future restish invocations
Installed plugin restish-anonymize
```

Once installed, the plugin anonymizes every decoded response before formatting.

## Use

Render anonymized JSON:

```console
$ restish get https://api.example.test/volumes -o json
```

Render the same anonymized data with `pretty`:

```console
$ restish get https://api.example.test/volumes -o pretty
```

## Rules

- Fields named `id`, `ids`, `uuid`, `uuids`, `identifier`, or `identifiers`
  are anonymized. Delimited and camel-case suffixes such as `organization_id`,
  `instance-id`, and `requestId` are also recognized.
- Exact UUID and hexadecimal strings of at least 16 characters are anonymized
  regardless of their field name.
- Identifier-shaped object keys and response header values follow the same
  rules.
- Repeated values receive the same replacement throughout one response,
  including response headers.
- Strings keep their case, character classes, and separators. UUIDs remain
  UUIDs, hexadecimal values remain hexadecimal, and numeric IDs remain numeric.
- Mapping state lives only for one response. Separate commands produce new
  random replacements.
- Empty values, booleans, fractional numbers, names, free text, and other data
  that does not match these rules are unchanged.

This plugin is identifier anonymization, not complete privacy redaction. Review
output before sharing it if names, email addresses, tokens, or free text may be
sensitive. Restish bypasses response middleware for raw-body output, so raw
responses are not anonymized.

## Development

```console
$ gofmt -w *.go
$ go mod tidy
$ go test ./...
$ go test -race ./...
$ go vet ./...
$ go build ./...
```
