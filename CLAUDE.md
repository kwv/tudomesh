# tudomesh

Merges maps from multiple Valetudo vacuums into one coordinate system, with live robot positions over MQTT and an HTTP API (live SVG/PNG maps, composite SVG for checking alignment, per-vacuum state/room/map). Read-only: it never sends commands to the robots. See README.md for features and config; longer docs are in `docs/` (setup guide, SVG structure, room planning).

## Layout
- `main.go`, `app.go`, `handlers.go`, `vacuum_handlers.go` — CLI entry, MQTT service, HTTP handlers (`/vacuum/{key}` per-robot state and map)
- `mesh/` — decoding, alignment (`align.go`: grid occupancy search in `grid_align.go`, room-name fallback in `room_align.go`), calibration cache, renderers (`svg.go`: structured SVG for live, floorplan and composite views; `renderer.go`: PNG; `theme.go`: light/dark/auto colors; `room_colors.go`: room fills, touching rooms differ; `crop.go`: `?crop=`), room sync planning (`room_sync.go`: Valetudo split/join/rename edits that match a robot's rooms to the reference's, simulated only; `--plan-rooms`)
- Units: layer pixels are grid indices; entity points are grid × pixelSize. Only renderers convert to image scale.

## Commands
```bash
make build      # build ./tudomesh
make test       # go test
make lint       # golangci-lint (also run by the pre-commit hook)
make calibrate  # rebuild, clear the calibration cache, realign tudomesh-data/ and print each robot's fit
```

`tudomesh-data/` holds the local config and map exports used by `make calibrate` and the real-map tests; it is not committed.

## Workflow
- Branch off `main` and open a PR on GitHub (`kwv/tudomesh`). The user reviews and merges; don't merge PRs yourself. CI only runs on PRs into `main`, so stacked PRs get CI after retargeting.
- Lint and tests must pass before committing.
- Use placeholder robot and room names (`robo7`, `TidyCalmPanda`, `Kitchen`) in code, tests, docs, commits and PRs, never names from `tudomesh-data/` or the real robots.
- Release: push a single `vX.Y.Z` tag; `build-test-publish.yml` builds and publishes `kwv4/tudomesh:{version}` and `:latest`. GitHub sends no tag events when one push carries more than three tags, so never push tags in bulk.
