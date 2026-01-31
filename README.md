# TudoMesh

Combines multiple Valetudo vacuum robot maps into a single unified coordinate system using automatic ICP alignment. Supports both CLI batch processing and real-time MQTT integration.

## Features

- Automatic map alignment using Iterative Closest Point (ICP) algorithm
- Multi-rotation testing for accurate orientation detection
- Real-time MQTT integration for live position tracking
- Composite floor plan generation
- Single unified configuration file
- Support for 3+ vacuums on the same floor

## Installation

### Prerequisites

- Go 1.22 or later
- MQTT broker (Mosquitto recommended) - only for MQTT mode
- Valetudo-enabled vacuum robots

### Build from Source

```bash
# Clone the repository
git clone https://github.com/kwv/tudomesh.git
cd tudomesh

# Build binary
make build

# Or build directly with go
go build -o tudomesh .

# Run tests
make test
```

### Docker

```bash
docker pull kwv4/tudomesh:latest
docker run -v $(pwd)/config.yaml:/config.yaml kwv4/tudomesh --mqtt
```

## Quick Start

### 1. Prepare Data (CLI Mode)

Export your Valetudo maps to JSON files:

1. In Valetudo web interface: Map Management → Export Map Data
2. Save files to a directory (e.g., `ValetudoMapExport-VacuumID.json`)
3. Place files in your working directory or use `--data-dir`

### 2. Generate Composite Map (CLI Mode)

Process exported Valetudo JSON files and generate composite floor plans.

```bash
# Simple rendering with automatic ICP alignment
./tudomesh --render

# The ICP algorithm automatically:
# - Selects reference vacuum (largest floor area)
# - Tests all 4 rotations for each vacuum
# - Computes optimal alignment transforms
# - Generates composite-map.png

# Override rotation if ICP picks wrong orientation
./tudomesh --render --force-rotation="VacuumID=180"

# Compare all 4 rotation options visually
./tudomesh --compare-rotation=VacuumID

# Render individual vacuum maps (useful for debugging)
./tudomesh --render-individual
```

**Important**: The ICP algorithm automatically detects the best rotation in most cases. Manual rotation hints with `--force-rotation` are only needed for ambiguous floor plans.

### 3. Example Workflow

```bash
# Step 1: Parse exports and check data
./tudomesh --parse-only

# Step 2: Generate composite with automatic alignment
./tudomesh --render --output=my-floorplan.png

# Step 3: If rotation looks wrong, compare options
./tudomesh --compare-rotation=VacuumID

# Step 4: Apply correct rotation and regenerate
./tudomesh --render --force-rotation="VacuumID=270" --output=final.png
```

### MQTT Service Mode

Real-time position transformation service that subscribes to Valetudo MQTT topics.

```bash
# Create your configuration file first
cp config.example.yaml config.yaml
nano config.yaml  # Add your vacuum topics

# Run MQTT service with HTTP endpoints
./tudomesh --mqtt --http --http-port 8080

# Or use environment variables
export MQTT_BROKER="mqtt://localhost:1883"
./tudomesh --mqtt --http
```

When running in MQTT mode, the service:
1. Subscribes to configured Valetudo vacuum topics
2. Decodes map data (PNG with zTXt chunks or raw JSON)
3. Extracts robot positions from map entities
4. Applies automatic ICP alignment (or cached transforms)
5. Publishes transformed positions to `tudomesh/{vacuumID}` topics
6. Serves HTTP endpoints for live visualization

## HTTP Endpoints

- `/health` - Service health check
- `/composite-map.png` - Color-coded vacuum maps
- `/floorplan.png` - Greyscale floor plan
- `/live.png` - Greyscale floor plan with live position indicators

## Configuration

The application uses a single `config.yaml` file for all settings.

### Create Your Configuration

```bash
# Copy the example config
cp config.example.yaml config.yaml

# Edit with your vacuum details
nano config.yaml
```

### Configuration Structure

```yaml
mqtt:
  broker: "mqtt://localhost:1883"
  publishPrefix: "tudomesh"
  clientId: "tudomesh"
  # username: ""  # Optional MQTT authentication
  # password: ""

reference: vacuum1  # Optional: auto-selected by largest floor area if not specified

vacuums:
  - id: vacuum1                              # Friendly name
    topic: valetudo/YourVacuumID/MapData/map-data
    color: "#FF6B6B"

  - id: vacuum2
    topic: valetudo/AnotherVacuumID/MapData/map-data
    color: "#4ECDC4"
    rotation: 180                            # Optional rotation hint

  - id: vacuum3
    topic: valetudo/ThirdVacuumID/MapData/map-data
    color: "#45B7D1"
    rotation: 270                            # Optional rotation hint
```

### Configuration Notes

- **Reference vacuum**: Specified via top-level `reference` field or auto-selected by largest floor area
- **Rotation hints**: Optional; ICP uses these as starting points for more reliable alignment
- **Cache file**: `.calibration-cache.json` stores computed ICP transforms automatically
- **Manual overrides**: Add `translation: {x: 0, y: 0}` for full manual calibration (rare)
- **MQTT settings**: Can also be set via environment variables (see below)

See `config.example.yaml` for a complete example with all options.

### Environment Variables

```bash
# MQTT configuration (overrides config.yaml)
MQTT_BROKER=mqtt://localhost:1883
MQTT_USERNAME=user
MQTT_PASSWORD=pass
MQTT_PUBLISH_PREFIX=tudomesh
MQTT_CLIENT_ID=tudomesh

# Data directory (default: current directory)
DATA_DIR=/path/to/data
```

## CLI Flags

| Flag | Description |
|------|-------------|
| `--render` | Render composite PNG with ICP alignment |
| `--render-individual` | Render each vacuum as separate PNG |
| `--parse-only` | Parse JSON exports and display info |
| `--calibrate` | Run detailed ICP calibration analysis |
| `--compare-rotation=ID` | Generate 4 rotation options for a vacuum |
| `--force-rotation=ID=DEG,...` | Override ICP with manual rotations |
| `--reference=ID` | Override automatic reference vacuum selection |
| `--output=FILE` | Output filename (default: composite-map.png) |
| `--data-dir=DIR` | Directory containing JSON exports |
| `--config=FILE` | Configuration file path (default: config.yaml) |
| `--mqtt` | Enable MQTT service mode |
| `--http` | Enable HTTP server |
| `--http-port=PORT` | HTTP server port (default: 8080) |

## Testing with Live MQTT

Ready to test with live Valetudo vacuums? Follow these steps to set up real-time position tracking.

### Prerequisites

- Running MQTT broker (Mosquitto recommended)
- Valetudo-enabled vacuums publishing to MQTT
- Network access between service and broker

### Step 1: Verify Valetudo Publishing

First, confirm your vacuums are sending map data to MQTT:

```bash
# Subscribe to all Valetudo map topics
mosquitto_sub -h localhost -p 1883 -v -t 'valetudo/+/MapData/#'
```

**What to look for:**
- Topics like `valetudo/YourVacuumID/MapData/map-data`
- Binary PNG data being published (will appear as garbled text)
- Updates every few seconds or when vacuum moves

### Step 2: Find Your Vacuum IDs

Valetudo generates random IDs like `FrugalLameLion` or `TrickyZanyPartridge`. Note these from the MQTT topics.

```bash
# List active topics to find vacuum IDs
mosquitto_sub -h localhost -p 1883 -v -t 'valetudo/#' | grep MapData
```

### Step 3: Create Configuration File

Update `config.yaml` with your actual vacuum topics:

```yaml
mqtt:
  broker: "mqtt://localhost:1883"
  publishPrefix: "tudomesh"
  clientId: "tudomesh"

reference: living-room  # Optional: auto-selected if not specified

vacuums:
  - id: living-room              # Friendly name (you choose this)
    topic: valetudo/YourVacuumID/MapData/map-data
    color: "#FF6B6B"

  - id: kitchen
    topic: valetudo/AnotherVacuumID/MapData/map-data
    color: "#4ECDC4"
    rotation: 180                # Optional: add if ICP picks wrong rotation

  - id: bedroom
    topic: valetudo/ThirdVacuumID/MapData/map-data
    color: "#45B7D1"
    rotation: 270                # Optional: add if ICP picks wrong rotation
```

### Step 4: Run Initial Calibration (CLI)

Before running the MQTT service, generate an initial calibration using exported maps:

```bash
# Export maps from Valetudo web interface first
# Then run CLI calibration
./tudomesh --calibrate

# This creates .calibration-cache.json for MQTT mode to use
```

### Step 5: Start MQTT Service

```bash
# Build if you haven't already
make build

# Run with MQTT and HTTP enabled
./tudomesh --mqtt --http --http-port 8080
```

**Expected console output:**
```
Loaded configuration with 3 vacuums
MQTT client connecting to mqtt://localhost:1883
MQTT connected
Subscribed to valetudo/YourVacuumID/MapData/map-data
...
HTTP server listening on :8080
```

### Step 6: Monitor Transformed Positions

Open a second terminal and subscribe to the published topics:

```bash
# Subscribe to all tudomesh topics
mosquitto_sub -h localhost -p 1883 -v -t 'tudomesh/#'
```

Or view the live floor plan in your browser:

```
http://localhost:8080/live.png
```

### Troubleshooting

**No MQTT connection:**
```bash
# Check broker is running
systemctl status mosquitto

# Test direct connection
mosquitto_pub -h localhost -p 1883 -t test -m hello
```

**No data received:**
```bash
# Verify Valetudo is publishing
mosquitto_sub -t 'valetudo/#' -v

# Check topic names match exactly
grep topic config.yaml
```

**Decoding errors:**
```bash
# Test with exported JSON first
./tudomesh --parse-only

# Check decoder tests pass
go test -v ./vacuum -run TestDecode
```

**Wrong positions or rotations:**
```bash
# Use CLI mode first to find correct rotations
./tudomesh --compare-rotation=VacuumID

# Once found, add to config.yaml:
vacuums:
  - id: vacuum-name
    rotation: 180  # Use value from --compare-rotation
```

## How ICP Alignment Works

The Iterative Closest Point (ICP) algorithm automatically aligns vacuum maps by:

1. **Feature Extraction**: Detects walls, corners, and structural features from each map
2. **Rotation Detection**: Tests 4 orientations (0°, 90°, 180°, 270°) independently
3. **Multi-scale Refinement**: Coarse-to-fine correspondence matching
4. **Transform Calculation**: Computes optimal affine transformation matrix
5. **Validation**: Scores alignments based on inlier fraction, not raw distance

The algorithm typically identifies the correct rotation automatically. Optional rotation hints in `config.yaml` can improve reliability for ambiguous floor plans.

**Note**: The reference vacuum (largest floor area by default) defines the unified "world" coordinate system. All other vacuums are transformed to match.

## Architecture

```
tudomesh/
├── main.go                     # CLI entry point
├── config.yaml                 # Single configuration file
├── .calibration-cache.json     # Auto-managed transform cache
├── vacuum/
│   ├── types.go               # Core data structures
│   ├── parser.go              # Valetudo JSON parsing
│   ├── decoder.go             # PNG zTXt map decoder (MQTT format)
│   ├── transform.go           # Affine transformation math
│   ├── features.go            # Feature extraction for ICP
│   ├── icp.go                 # ICP alignment algorithm
│   ├── renderer.go            # Composite PNG generation
│   ├── mqtt.go                # MQTT client integration
│   ├── publisher.go           # Position publishing
│   ├── state.go               # Live position tracking
│   ├── calibration.go         # Transform cache persistence
│   └── config.go              # Configuration handling
└── ValetudoMapExport-*.json   # Input map data (CLI mode)
```

**Data Flow:**
```
MQTT (valetudo/*/MapData/map-data)
  → DecodeMapData (PNG with zTXt → JSON)
  → ExtractRobotPosition (mm coordinates)
  → /pixelSize → grid coordinates
  → TransformPoint (calibration matrix)
  → StateTracker.UpdatePosition
  → RenderLive → /live.png
```

## Development

### Running Tests

```bash
# Run all tests
make test

# Run specific test package
go test -v ./vacuum

# Run with race detection
go test -race ./vacuum
```

### Dependencies

- `golang.org/x/image`: PNG encoding and font rendering
- `gopkg.in/yaml.v3`: YAML configuration parsing
- `github.com/eclipse/paho.mqtt.golang`: MQTT client
- Standard library: `math`, `image`, `compress/zlib`, `encoding/json`

Minimal dependencies by design - core math uses only standard library.

## Contributing

Contributions welcome! Before submitting PRs:
1. Run `make test` and `make lint`
2. Update documentation if adding features
3. Add tests for new functionality

## License

MIT License - See LICENSE file for details.

## Acknowledgments

- Built for use with [Valetudo](https://valetudo.cloud/) open-source vacuum firmware
- ICP algorithm implementation inspired by academic research in point cloud alignment
