# TudoMesh

Combines multiple Valetudo vacuum robot maps into a single unified coordinate system using automatic ICP alignment. Supports both CLI batch processing and real-time MQTT integration.

## Features

- **Automatic Map Alignment**: Uses Iterative Closest Point (ICP) algorithm for sub-pixel precision.
- **Auto-Rotation**: Tests all 4 orientations (0°, 90°, 180°, 270°) to find the best fit.
- **Smart Persistence**:
  - **Auto-Cache**: Automatically saves full maps received via MQTT to disk.
  - **Transform Cache**: Stores alignment results in `.calibration-cache.json` for instant startups.
- **Real-time MQTT**: Transforms robot positions in milliseconds and republishes to a unified topic.
- **Live Visualization**: Generates a unified composite floorplan reachable via HTTP.


## Installation

### Prerequisites

- Go 1.22 or later
- MQTT broker (Mosquitto recommended)
- Valetudo-enabled vacuum robots

### Build from Source

```bash
# Clone the repository
git clone https://github.com/kwv/tudomesh.git
cd tudomesh

# Build binary
make build

# Run tests
make test
```

### Docker (Recommended)


```bash
docker pull kwv4/tudomesh:latest

# Run with a unified data directory
docker run -v /your/local/path:/data \
  kwv4/tudomesh \
  --mqtt --http --data-dir /data
```

## Quick Start

### 1. Unified Data Directory

TudoMesh works best when you give it a single "workspace" directory. Place your `config.yaml` here.

```bash
mkdir -p ./tudomesh-data
cp config.example.yaml ./tudomesh-data/config.yaml
```

### 2. Configure Your Robots

Edit `config.yaml`.Use the `/map-data` topic if your robots support it (it contains the full pixel data embedded in the PNG metadata).

```yaml
vacuums:
  - id: vacuum1
    topic: valetudo/vacuum1/MapData/map-data  # Full map data
    color: "#43b0f1"
  - id: vacuum2
    topic: valetudo/vacuum2/MapData/map-data
    color: "#057dcd"
    rotation: 180  # ICP will refine this hint
```

### 3. Generate Composite Map (CLI Mode)

If you have exported Valetudo JSON files, place them in your data directory.

```bash
# Process files and generate composite-map.png
./tudomesh --data-dir ./tudomesh-data --render

# Compare all 4 rotation options visually if alignment looks off
./tudomesh --data-dir ./tudomesh-data --compare-rotation=vacuum2
```

### 4. Run MQTT Service

```bash
# Start the live transformation service
./tudomesh --data-dir ./tudomesh-data --mqtt --http --http-port 4040
```

## Service Features

### Auto-Caching
TudoMesh includes a "Lazy Persistence" system. If you start the service without local map files, it will use a grey background. As soon as a robot sends a "Full Map" via MQTT (e.g., when it finishes a clean or docks), TudoMesh will **automatically save that map** to your `--data-dir`. On next restart, your floorplan will load instantly from disk.

### Robust Position Tracking
Robots often send "Lightweight" position updates via MQTT (small packets without pixel data). TudoMesh intelligently merges these: it keeps your rich floorplan from the cache but updates the robot icon using the live lightweight movements.

## HTTP Endpoints

- `/health` - Service health check
- `/composite-map.png` - Color-coded vacuum maps
- `/floorplan.png` - Greyscale unified floor plan
- `/live.png` - Greyscale floor plan with live position icons and legend

## CLI Flags

| Flag | Description |
|------|-------------|
| `--mqtt` | Enable MQTT service mode (live tracking) |
| `--http` | Enable HTTP server for map visualization |
| `--data-dir=DIR` | Base directory for config, maps, and cache (Recommended) |
| `--config=FILE` | Configuration file path (default: config.yaml inside --data-dir) |
| `--calibration-cache=FILE` | Calibration cache path (relative to --data-dir) |
| `--render` | Batch mode: Render composite PNG from local files |
| `--calibrate` | Batch mode: Run detailed ICP analysis on local files |
| `--compare-rotation=ID` | Debug: Generate 4 rotation options for a vacuum |
| `--force-rotation=ID=DEG` | Override: Manual rotation (0, 90, 180, 270) |



## License

MIT License - See LICENSE file for details.

## Acknowledgments

- Built for use with [Valetudo](https://valetudo.cloud/) open-source vacuum firmware.
- ICP implementation optimized for 2D structural floorplan alignment.
