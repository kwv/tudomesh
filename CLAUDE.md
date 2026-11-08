# tudomesh

Merges maps from multiple Valetudo vacuums into one coordinate system, with live robot positions over MQTT and an HTTP API (composite SVG/PNG map, per-vacuum state/room/map for alerts). See README.md for features and config.

## Layout
- `main.go`, `app.go`, `handlers.go`, `vacuum_handlers.go` — CLI entry, MQTT service, HTTP handlers (`/vacuum/{key}` for alerts)
- `mesh/` — decoding, alignment (`align.go`: grid occupancy search in `grid_align.go`, room-name fallback in `room_align.go`), calibration cache, consensus floorplan (`consensus.go`: per-cell voting across aligned maps; `floorplan.go`: cached on `StateTracker`, rebuilt when calibration changes), renderers (`svg.go`: structured SVG for live, floorplan and composite views; `renderer.go`: PNG)
- Units: layer pixels are grid indices; entity points are grid × pixelSize. Only renderers convert to image scale.
- `.designs/` — older design notes; they predate the grid alignment and still mention ICP

## Commands
```bash
make build      # build ./tudomesh
make test       # go test
make lint       # golangci-lint (also run by the pre-commit hook)
make render     # rebuild, clear calibration cache, render tudomesh-data/test.{png,svg} and test-floorplan.{png,svg}
```

`tudomesh-data/` holds the local config and map exports used by `make render`; it is not committed.

## Workflow
- Branch off `main`, open a PR on GitHub (`kwv/tudomesh`), merge after CI passes.
- Lint and tests must pass before committing.
- Release: push a single `vX.Y.Z` tag; `build-test-publish.yml` builds and publishes `kwv4/tudomesh:{version}` and `:latest`. GitHub sends no tag events when one push carries more than three tags, so never push tags in bulk.
