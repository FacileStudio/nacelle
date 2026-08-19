# nacelle — Documentation

| Page | What's in it |
|---|---|
| [Architecture](architecture.md) | The `Backend` seam, the event stream, the `Message` union, the tool loop, retry, caching |
| [Configuration](configuration.md) | Every `Config` field, the TUI's own settings layer, and its precedence order |
| [Development](development.md) | Local setup, the quality gate, CI, versioning |
| [API](api.md) | Every exported symbol, package by package |

`nacelle` is a library. It ships no service and has no deployment of its own — it is embedded
by whatever agent imports it, which is why there is no `deployment.md` here.

Release history lives in [CHANGELOG.md](../CHANGELOG.md), at the repo root.

Back to the [README](../README.md).
