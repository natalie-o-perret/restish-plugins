# Restish plugins

Plugins for [Restish](https://rest.sh/):

| Plugin | Description |
| --- | --- |
| [`arazzo`](arazzo/) | Run OpenAPI Arazzo workflows |
| [`pretty`](pretty/) | Render nested JSON as readable trees and tables |
| [`progress`](progress/) | Render streaming progress updates |

Each plugin is an independent Go module. Build one from its directory or use
the workspace to test them all:

```console
$ go test ./arazzo/... ./pretty/... ./progress/...
```

Release tags follow Go's submodule convention: `arazzo/vX.Y.Z`,
`pretty/vX.Y.Z`, and `progress/vX.Y.Z`.
