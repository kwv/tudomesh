# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go service that combines multiple Valetudo robot vacuum maps into a unified coordinate system. Receives live position data via MQTT, applies calibrated transforms, and serves composite floor plan images via HTTP.

## Build & Run

```bash
make build          # Compile binary
make test           # Run tests
make lint           # Run golangci-lint

# Service mode (primary usage)
./tudomesh --mqtt --http --http-port 8080

# Calibration (run once to generate transforms)
./tudomesh --calibrate

# Static render
./tudomesh --render --output composite.png
```

## Key Files

- `main.go` - CLI dispatcher and service modes
- `vacuum/mqtt.go` - MQTT subscription handling
- `vacuum/decoder.go` - PNG/zlib map data decoding
- `vacuum/parser.go` - Valetudo JSON parsing, position extraction
- `vacuum/renderer.go` - Composite map rendering, RenderLive for positions
- `vacuum/icp.go` - Iterative Closest Point alignment algorithm
- `vacuum/calibration.go` - Transform cache persistence
- `vacuum/state.go` - Live position tracking (StateTracker)

## Architecture

```
MQTT (valetudo/*/MapData/map-data)
  → DecodeMapData (PNG with zTXt → JSON)
  → ExtractRobotPosition (mm coordinates)
  → /pixelSize → grid coordinates
  → TransformPoint (calibration matrix)
  → StateTracker.UpdatePosition
  → RenderLive → /live.png
```

**Coordinate Systems:**
- Entity points (robot_position, charger_location): millimeters
- Layer pixels (floor, wall, segment): grid units
- Conversion: grid = mm / pixelSize (typically 5)

**CRITICAL RULES:**
 - Use 'bd' for task tracking. Run 'bd ready --json' to find work.
 - refer to AGENTs.md 