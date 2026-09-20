# restish-plugin-progress

[![CI](https://github.com/natalie-o-perret/restish-plugins/actions/workflows/ci.yml/badge.svg)](https://github.com/natalie-o-perret/restish-plugins/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/natalie-o-perret/restish-plugins/progress.svg)](https://pkg.go.dev/github.com/natalie-o-perret/restish-plugins/progress)
[![License](https://img.shields.io/github/license/natalie-o-perret/restish-plugins)](LICENSE)

`restish-plugin-progress` adds a streaming `progress` output formatter to
[Restish](https://rest.sh/).

SSE and NDJSON define how records are transported, but neither defines a
standard progress payload. This plugin uses the following small JSON contract
for each input item:

```json
{
  "id": "deploy",
  "label": "Deploy instances",
  "state": "running",
  "current": 3,
  "total": 5,
  "unit": "steps",
  "message": "starting instance 4"
}
```

`label` falls back to `id`. `state` defaults to `running`. `current` and
`total` are optional, but must be supplied together. `unit` defaults to
`steps`.

The formatter redraws one terminal line when Restish enables terminal
formatting. Redirected or colour-disabled output uses one line per changed
record, which remains readable in logs and pipes.

```text
66% ████████████████░░░░░░░░  Deploy instances  2/3 steps  running: applying changes
```

The visual bar is rendered by
[`schollz/progressbar`](https://github.com/schollz/progressbar).

## Build And Install

```sh
go build -o restish-progress .
restish plugin install ./restish-progress --yes
```

## Use

Select the formatter with `-o progress`. For an SSE or NDJSON endpoint whose
events already use the plugin's JSON contract:

```sh
restish example events --rsh-print bc -o progress
```

Use a Restish filter to map another event shape into the contract before
formatting it:

```sh
restish example events \
  -f 'body.data | {id, label, state, current, total, message}' \
  --rsh-print bc \
  -o progress
```

Use `--rsh-print b` instead when redirecting output and ANSI colours are not
wanted.

## Customise

The defaults use a 24-character Unicode bar with a red-to-pink gradient.
Environment variables can change the style without changing event payloads:

| Variable | Default | Purpose |
| --- | --- | --- |
| `RSH_PROGRESS_WIDTH` | `24` | Bar width from 1 to 200 |
| `RSH_PROGRESS_COLOR` | empty | Solid colour overriding the gradient |
| `RSH_PROGRESS_COLOR_START` | `#ff3b30` | Gradient start colour |
| `RSH_PROGRESS_COLOR_END` | `#ff2d95` | Gradient end colour |
| `RSH_PROGRESS_FILL` | `█` | Filled character |
| `RSH_PROGRESS_HEAD` | `█` | Leading character |
| `RSH_PROGRESS_EMPTY` | `░` | Empty character |
| `RSH_PROGRESS_START` | empty | Bar prefix |
| `RSH_PROGRESS_END` | empty | Bar suffix |

Colours may use `#RRGGBB` or `black`, `blue`, `cyan`, `green`, `magenta`,
`red`, `white`, or `yellow`. The `c` in `--rsh-print bc` enables colour.

```sh
RSH_PROGRESS_WIDTH=32 \
RSH_PROGRESS_COLOR_START='#7c3aed' \
RSH_PROGRESS_COLOR_END='#22d3ee' \
RSH_PROGRESS_FILL='━' \
RSH_PROGRESS_HEAD='╺' \
RSH_PROGRESS_EMPTY='─' \
restish example events --rsh-print bc -o progress
```
