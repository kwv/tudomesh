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
- **Auto-Calibration on Docking**: Automatically recalibrates vacuum alignment when a robot returns to its charger.

## Auto-Calibration

TudoMesh can automatically keep vacuum alignment up to date without manual intervention. When a vacuum finishes cleaning and docks at its charger, TudoMesh detects the state change and recalibrates the coordinate transform.

### How It Works

1. **Docking Detection**: TudoMesh subscribes to each vacuum's `StatusStateAttribute/status` MQTT topic (derived automatically from the MapData topic). When the state changes to `docked`, the calibration handler fires.

2. **Debounce (30 minutes)**: To avoid excessive recalibration, TudoMesh skips recalibration if the last calibration for that vacuum was less than 30 minutes ago and the map area has not changed.

3. **Map Fetch**: TudoMesh fetches the vacuum's full map via its REST API (`apiUrl` in config). This provides a complete, high-quality map suitable for ICP alignment.

4. **ICP Alignment**: The fetched map is aligned against the reference vacuum using the same ICP algorithm used in batch calibration. The resulting affine transform is stored.

5. **Cache Update**: The updated transform is written to `.calibration-cache.json` so it persists across restarts.

### Configuration

Add `apiUrl` to each vacuum in your `config.yaml`:

```yaml
vacuums:
  - id: vacuum1
    topic: valetudo/vacuum1/MapData/map-data
    color: "#43b0f1"
    apiUrl: "http://192.168.1.100/api/v2/robot/state/map"
  - id: vacuum2
    topic: valetudo/vacuum2/MapData/map-data
    color: "#057dcd"
    apiUrl: "http://192.168.1.101/api/v2/robot/state/map"
```

The `apiUrl` field is optional. Vacuums without it will not be auto-calibrated but will still work with cached or manually configured transforms.

### State Topic Derivation

The state topic is derived automatically from the MapData topic by replacing the last two path segments:

| MapData topic | State topic |
|---|---|
| `valetudo/vacuum1/MapData/map-data` | `valetudo/vacuum1/StatusStateAttribute/status` |
| `home/floor1/valetudo/dusty/MapData/map-data` | `home/floor1/valetudo/dusty/StatusStateAttribute/status` |

Topics with fewer than 4 path segments cannot derive a state topic and will not support docking detection.

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

## Local Setup

### Install MQTT Broker (Mosquitto)

TudoMesh requires an MQTT broker to receive robot data. Mosquitto is recommended for local setups.

**Ubuntu/Debian:**

```bash
sudo apt-get update
sudo apt-get install mosquitto mosquitto-clients
sudo systemctl start mosquitto
sudo systemctl enable mosquitto
```

**macOS:**

```bash
brew install mosquitto
brew services start mosquitto
```

**Docker:**

```bash
docker run -d --name mosquitto -p 1883:1883 eclipse-mosquitto:latest
```

### Configure Mosquitto (Optional)

Default config allows anonymous connections to localhost. To enable authentication or configure listeners, edit `/etc/mosquitto/mosquitto.conf` (Linux) or `/opt/homebrew/etc/mosquitto/mosquitto.conf` (macOS).

Basic config for local development (allows all connections on port 1883):

```
listener 1883
protocol mqtt
allow_anonymous true
```

Then restart:

```bash
# Linux
sudo systemctl restart mosquitto

# macOS
brew services restart mosquitto
```

### Test MQTT Connection

Verify your broker is accessible:

```bash
# Subscribe to test topic (in one terminal)
mosquitto_sub -h localhost -p 1883 -t 'test/topic'

# Publish a message (in another terminal)
mosquitto_pub -h localhost -p 1883 -t 'test/topic' -m 'Hello'

# You should see "Hello" appear in the first terminal
```

### Verify Vacuum Topics

Check that your robots are publishing to the expected topics:

```bash
# Listen to all vacuum topics
mosquitto_sub -h localhost -p 1883 -t 'valetudo/+/MapData/map-data'

# Run a clean on your robot and watch for incoming messages
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

## Validation Workflow

Follow these steps to validate your setup is working end-to-end:

### 1. Start MQTT Broker

```bash
# Verify Mosquitto is running (check port 1883)
netstat -an | grep 1883

# Or use Docker
docker run -d --name mosquitto -p 1883:1883 eclipse-mosquitto:latest
```

### 2. Configure TudoMesh

Create `tudomesh-data/config.yaml`:

```yaml
mqtt:
  host: localhost
  port: 1883
  client_id: tudomesh

vacuums:
  - id: vacuum1
    topic: valetudo/vacuum1/MapData/map-data
    color: "#43b0f1"
  - id: vacuum2
    topic: valetudo/vacuum2/MapData/map-data
    color: "#057dcd"
```

### 3. Build TudoMesh

```bash
cd tudomesh
make build
```

### 4. Run Service

```bash
./tudomesh --data-dir ./tudomesh-data --mqtt --http --http-port 4040
```

You should see output like:

```
tudomesh version: dev
Starting tudomesh service...

Service Running
===============

MQTT:
  Subscribed topics:
    - valetudo/vacuum1/MapData/map-data (vacuum1)
    - valetudo/vacuum2/MapData/map-data (vacuum2)
  Publishing to: tudomesh/{vacuumID}
  Combined positions: tudomesh/positions

HTTP endpoints (port 4040):
  GET /health          - Health check
  GET /composite-map.png - Color-coded composite map
  GET /live.png        - Greyscale floor plan with live positions
  GET /composite-map.svg - Color-coded composite map (SVG)
  GET /floorplan.svg   - Greyscale floor plan (SVG)

Press Ctrl+C to stop
```

### 5. Test Endpoints (PNG)

In another terminal:

```bash
# Health check
curl http://localhost:4040/health

# Composite PNG (color-coded)
curl http://localhost:4040/composite-map.png > composite.png

# Live PNG with robot positions
curl http://localhost:4040/live.png > live.png
```

### 6. Test Endpoints (SVG)

```bash
# Composite SVG
curl http://localhost:4040/composite-map.svg > composite.svg

# Floorplan SVG
curl http://localhost:4040/floorplan.svg > floorplan.svg

# View in browser or vector editor
open composite.svg
```

### 7. Verify Calibration Cache

After the first render or when robots send data, check the cache:

```bash
cat tudomesh-data/.calibration-cache.json
```

Expected output (example):

```json
{
  "reference_vacuum": "vacuum1",
  "vacuums": {
    "vacuum1": {
      "a": 1.0,
      "b": 0.0,
      "c": 0.0,
      "d": 1.0,
      "tx": 0.0,
      "ty": 0.0
    },
    "vacuum2": {
      "a": 0.999,
      "b": -0.045,
      "c": 0.045,
      "d": 0.999,
      "tx": -150.5,
      "ty": 200.3
    }
  }
}
```

### 8. Verify MQTT Subscriptions

Monitor incoming position updates:

```bash
mosquitto_sub -h localhost -p 1883 -t 'tudomesh/+' | head -20
```

You should see JSON messages like:

```json
{"vacuum_id": "vacuum1", "x": 1234.5, "y": 5678.9, "angle": 45.0}
```

### Troubleshooting

**No maps available error:**
- Ensure robots are publishing to the configured MQTT topics
- Check that maps are saved to the data directory
- Verify MQTT broker is running: `netstat -an | grep 1883`

**Cache not updating:**
- Run `./tudomesh --data-dir ./tudomesh-data --calibrate` to force recalibration
- Check file permissions on the data directory

**Vector SVG endpoints 404:**
- Vector rendering is only available on HTTP server (not in batch render)
- Use `GET /composite-map.svg` and `GET /floorplan.svg` endpoints

**Auto-calibration not triggering:**
- Verify `apiUrl` is set for the vacuum in `config.yaml`
- Check that the vacuum's MapData topic has at least 4 path segments (e.g., `valetudo/name/MapData/map-data`) so the state topic can be derived
- Look for `Subscribing to ... StatusStateAttribute/status` in the startup logs to confirm state topic subscription
- Check that the vacuum actually reports `{"value":"docked"}` on its state topic: `mosquitto_sub -t 'valetudo/+/StatusStateAttribute/status'`

**Auto-calibration triggers but transform is not changing:**
- The 30-minute debounce prevents recalibration if the last run was recent and the map area has not changed
- If the map area is identical, the debounce must expire before recalibration runs again
- Force immediate recalibration with: `./tudomesh --data-dir ./tudomesh-data --calibrate`

**API fetch failing during auto-calibration:**
- Verify the `apiUrl` is reachable from the TudoMesh host: `curl -H 'Accept: application/json' http://192.168.1.100/api/v2/robot/state/map`
- Check for network/firewall issues between TudoMesh and the vacuum
- TudoMesh retries failed fetches with exponential backoff (up to 3 attempts)

## Service Features

### Auto-Caching
TudoMesh includes a "Lazy Persistence" system. If you start the service without local map files, it will use a grey background. As soon as a robot sends a "Full Map" via MQTT (e.g., when it finishes a clean or docks), TudoMesh will **automatically save that map** to your `--data-dir`. On next restart, your floorplan will load instantly from disk.

### Robust Position Tracking
Robots often send "Lightweight" position updates via MQTT (small packets without pixel data). TudoMesh intelligently merges these: it keeps your rich floorplan from the cache but updates the robot icon using the live lightweight movements.

## Vector Rendering (SVG + PNG)

TudoMesh supports vector rendering for scalable, resolution-independent maps. Render as SVG for web use, or convert to PNG with high DPI.

### Render Formats

Use `--format` to control output:

```bash
# Raster PNG only (default)
./tudomesh --data-dir ./tudomesh-data --render --format=raster

# Vector SVG only
./tudomesh --data-dir ./tudomesh-data --render --format=vector

# Both formats (generates .png and .svg)
./tudomesh --data-dir ./tudomesh-data --render --format=both
```

### Vector Output Format

Use `--vector-format` to choose SVG or PNG output (default: svg):

```bash
# Render as SVG (scalable)
./tudomesh --data-dir ./tudomesh-data --render --format=vector --vector-format=svg

# Render as high-DPI PNG
./tudomesh --data-dir ./tudomesh-data --render --format=vector --vector-format=png
```

### Grid Spacing

Control the distance between grid lines (default: 1000mm = 1 meter):

```bash
# Render with 500mm grid spacing
./tudomesh --data-dir ./tudomesh-data --render --grid-spacing=500

# Render with 2000mm grid spacing
./tudomesh --data-dir ./tudomesh-data --render --grid-spacing=2000
```

### Vector PNG Resolution

When rendering vector to PNG, set DPI (default: 300):

```bash
# High-resolution PNG at 600 DPI
./tudomesh --data-dir ./tudomesh-data --render --format=vector --vector-format=png --vector-resolution=600
```

## HTTP Endpoints

### Raster Formats (PNG)

- `/health` - Service health check
- `/composite-map.png` - Color-coded vacuum maps
- `/live.png` - Greyscale floor plan with live position icons and legend

### Vector Formats (SVG)

- `/composite-map.svg` - Color-coded vacuum maps as scalable vector
- `/floorplan.svg` - Greyscale unified floor plan as scalable vector

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
| `--format=[raster\|vector\|both]` | Render format: raster PNG, vector SVG, or both (default: raster) |
| `--vector-format=[svg\|png]` | Vector output format: SVG or PNG (default: svg) |
| `--grid-spacing=MM` | Grid line spacing in millimeters (default: 1000mm) |
| `--vector-resolution=DPI` | Vector to PNG rasterization DPI (default: 300) |



## License

MIT License - See LICENSE file for details.

## Acknowledgments

- Built for use with [Valetudo](https://valetudo.cloud/) open-source vacuum firmware.
- ICP implementation optimized for 2D structural floorplan alignment.
