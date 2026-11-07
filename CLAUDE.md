# tudomesh

Merges maps from multiple Valetudo vacuums into one coordinate system (ICP alignment), with live robot positions over MQTT and an HTTP-served SVG/PNG map. See README.md for features and config.

## Layout
- `main.go`, `app.go`, `handlers.go` — CLI entry, MQTT service, HTTP handlers
- `mesh/` — decoding, feature extraction, ICP (`icp.go`), calibration cache, unifier, renderers
- `.designs/` — design notes (e.g. pipeline and coordinate systems)

## Commands
```bash
make build      # build ./tudomesh
make test       # go test
make lint       # golangci-lint (also run by the pre-commit hook)
make render     # rebuild, clear calibration cache, render tudomesh-data/test.{png,svg}
```

`tudomesh-data/` holds the local config and map exports used by `make render`; it is not committed.

## Workflow
- Branch off `main`, open a PR on GitHub (`kwv/tudomesh`), merge after CI passes.
- Lint and tests must pass before committing.
