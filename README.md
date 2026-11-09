# TudoMesh

Combines the maps of several Valetudo vacuum robots into one coordinate system and serves the result over HTTP: a live map with every robot's position, color-coded composite maps for checking alignment, and per-robot endpoints for "where is my stuck vacuum?" alerts. Runs as a live MQTT service or as a one-shot CLI over exported map files.

## Features

- **Automatic map alignment**: Searches every rotation and offset for the placement where floor lands on floor and wall on wall. Uses map geometry only (no room names needed), gives the same answer every run, and reports a confidence score. Same-named rooms break ties for very symmetric layouts; a `rotation` hint in config narrows the search.
- **Stuck-vacuum alerts**: `/vacuum/{id}` reports which room a robot is in and its last status; `/vacuum/{id}.png` returns the map with that robot circled. See [Alerts with Node-RED](#alerts-with-node-red).
- **Live positions over MQTT**: Follows each robot's map and status topics and places it on the shared map as it moves.
- **Auto-calibration on docking**: When a robot docks, TudoMesh fetches its full map, realigns it, and saves both the map and the transform. See [Auto-Calibration](#auto-calibration).
- **Fast restarts**: Alignment results are cached in `.calibration-cache.json`, and maps fetched from the robots are saved to the data directory, so the floorplan is back as soon as the service starts.
- **Minimal config**: List each robot's Valetudo URL; its id, MQTT topics and broker are read from the robot.
- **Structured SVG**: Every room and robot in the SVG maps is its own element with stable ids, so maps can be restyled with CSS or scripted. See [SVG Structure](#svg-structure).

## Installation

### Docker (recommended)

```bash
docker run -d -p 8080:8080 -v /your/local/path:/data kwv4/tudomesh
```

The image runs `--http --mqtt --data-dir=/data` by default and listens on port 8080. Put your `config.yaml` in the mounted directory (see [Configuration](#configuration)). The container runs as UID `65532`, which needs write access to that directory:

```bash
sudo chown -R 65532:65532 /your/local/path
```

[`docker-compose.yml`](docker-compose.yml) starts TudoMesh together with a Mosquitto broker.

### Build from source

Requires Go 1.26 or later.

```bash
git clone https://github.com/kwv/tudomesh.git
cd tudomesh
make build   # builds ./tudomesh
make test
```

### Requirements

- Valetudo robots with MQTT enabled
- An MQTT broker the robots publish to (see [Setting up Mosquitto](#setting-up-mosquitto) if you don't have one)

## Configuration

TudoMesh reads `config.yaml` from its data directory (`--data-dir`). Set the MQTT broker and list each robot by its Valetudo URL:

```yaml
mqtt:
  broker: mqtt://mqtt.home:1883
vacuums:
  - url: http://rockette.home
    color: "#43b0f1"   # optional; used by the composite views
  - url: http://rocky7.home
```

At startup TudoMesh asks each robot's Valetudo API for the rest:

| Value | Source | Override |
|---|---|---|
| Vacuum id | `systemId` from `/api/v2/valetudo` | `id` in config |
| MQTT map topic | `<topicPrefix>/<identifier>/MapData/map-data` from the robot's MQTT settings | `topic` in config |
| Map API for auto-calibration | `<url>/api/v2/robot/state/map` | `apiUrl` in config |
| MQTT broker | `mqtt.broker`; if unset, the broker the first reachable robot uses | `MQTT_BROKER` env var |
| MQTT client id | `tudomesh-<hostname>` | `mqtt.clientId` or `MQTT_CLIENT_ID` |
| MQTT credentials | none | `mqtt.username` / `mqtt.password` or `MQTT_USERNAME` / `MQTT_PASSWORD` |

Environment variables take precedence over `config.yaml`.

What each robot reports is saved to `.vacuums.json` in the data directory, so a robot that is offline at startup still resolves. With nothing saved, the URL's host name is used as the robot's name and id (`http://rocky6.home` → `rocky6`); once the robot answers, the id becomes its `systemId`, so delete any `ValetudoMapExport-rocky6.json` saved under the guessed id. Older configs that set `id`, `topic` and `apiUrl` keep working.

[`config.example.yaml`](config.example.yaml) lists the remaining options: `reference` (the robot used as the world origin; default is the one with the largest map), `gridSpacing`, `ignoreRooms` (see [Maps](#maps)), and per-vacuum `rotation` and `translation` overrides.

### One instance per floor

Robots on different floors share no map, so run one TudoMesh per floor, each with its own data directory and port. A robot alone on a floor works fine on its own: it becomes its own reference and every endpoint serves its map.

```yaml
# basement/config.yaml
mqtt:
  broker: mqtt://mqtt.home:1883
vacuums:
  - url: http://rocky.home
```

```bash
docker run -d -p 4041:8080 -v /srv/tudomesh-basement:/data kwv4/tudomesh
```

Each instance gets a unique MQTT client id from its container's host name, and the instances know nothing about each other. To find a robot, call `/vacuum/{id}` on each instance until one answers.

## Quick Start

1. Create a data directory and config:

   ```bash
   mkdir -p ./tudomesh-data
   cp config.example.yaml ./tudomesh-data/config.yaml
   # edit: set mqtt.broker and your robots' URLs
   ```

2. Start the service:

   ```bash
   ./tudomesh --data-dir ./tudomesh-data --mqtt --http --http-port 4040
   ```

   You should see output like:

   ```
   tudomesh version: dev
   Starting tudomesh service...
   ...
   Service Running
   ===============

   MQTT:
     Subscribed topics:
       - valetudo/rocky7/MapData/map-data (TrickyZanyPartridge)
       - valetudo/rockette/MapData/map-data (FrugalLameLion)

   HTTP endpoints (port 4040):
     GET /                - Homepage (live SVG map)
     GET /health          - Health check
     GET /live.svg        - Live map with vacuum positions (SVG)
     GET /live.png        - Live map with vacuum positions (PNG)
     GET /composite-map.png - Color-coded composite map
     GET /composite-map.svg - Color-coded composite map (SVG)
     GET /floorplan.svg   - Greyscale floor plan (SVG)

   Press Ctrl+C to stop
   ```

3. Open `http://localhost:4040/`. The homepage shows the live map; each robot appears as a colored circle with its name. Reload to update positions.

4. Check alignment on `http://localhost:4040/composite-map.svg`, where each robot's map is drawn in its own color. If one is rotated wrongly, see [Troubleshooting](#troubleshooting).

The map stays empty until each robot's full map is known: either from a `ValetudoMapExport-*.json` in the data directory or after the robot docks once (see [Auto-Calibration](#auto-calibration)).

## HTTP Endpoints

### Maps

- `/` - HTML page showing the live SVG map
- `/health` - Health check
- `/live.svg` - Greyscale unified floorplan with live vacuum positions. The primary live view, structured for styling and scripting (see [SVG Structure](#svg-structure)).
- `/live.png` - Greyscale floorplan with live position icons and a legend of robot names (the MQTT identifier, e.g. `rocky7`; the id if unknown)
- `/floorplan.svg` - The live SVG without vacuums

The live views (`/live.*`, `/floorplan.svg`, `/vacuum/{id}.png|.svg`) draw the reference robot's rooms and walls; other robots only add grey floor where the reference has none. The composite views draw every robot's walls.

`ignoreRooms` in config lists rooms drawn as plain floor on the live views, with no fill, label or alert highlight, for areas such as a porch seen through glass:

```yaml
ignoreRooms: [Porch, Deck]
```

Names match ignoring case and anything but letters and digits, so `Front Porch`, `front-porch` and `FrontPorch` are the same. A robot standing in an ignored room still reports it in `/vacuum/{id}` JSON and the caption.
- `/composite-map.svg` - Every vacuum's map overlaid in its own color, with chargers; use it to check alignment
- `/composite-map.png` - The composite map as PNG

The map endpoints return `503 No maps available` until at least one map is loaded.

### Per-vacuum (alerts)

`{id}` is the vacuum id (its Valetudo `systemId`, or `id` from config) or the identifier in its MQTT topic, e.g. `rocky7` for `valetudo/rocky7/MapData/map-data`. Matching is case-insensitive.

- `/vacuums` - JSON list of every vacuum (same fields as below)
- `/vacuum/{id}` - JSON: room, last Valetudo status, position on the shared map
- `/vacuum/{id}.png` - Live map with the vacuum circled and a caption (`rocky7 | Kitchen | error`). `?scale=1..8` sets the size (default 3).
- `/vacuum/{id}.svg` - Live SVG with the vacuum highlighted (pulsing ring) and its room tinted, when the base map has a room of the same name

```json
{
  "id": "TrickyZanyPartridge",
  "name": "rocky7",
  "state": "error",
  "stateSince": "2026-11-07T14:46:25Z",
  "room": { "name": "Kitchen", "segmentId": "4", "distance": 0 },
  "position": { "x": 517.3, "y": 314.9, "angle": 183, "updatedAt": "2026-11-07T14:46:25Z" },
  "mapUrl": "/vacuum/rocky7.png"
}
```

`room` comes from the robot's own map (the segment it is in, or the nearest one within ~50cm), so it is correct even before maps are aligned. Unnamed segments are reported as `segment <id>`. `state` requires `--mqtt`.

### SVG Structure

`/live.svg`, `/floorplan.svg` and `/vacuum/{id}.svg`:

```text
<svg>
  <style>…</style>                          default colors (override freely)
  <g id="map" transform="…">                map geometry in grid cells
    <g id="rooms">
      <path id="room-16" class="room" data-room="Kitchen" data-segment-id="16"/>
      <path class="floor"/>                 unsegmented floor
    </g>
    <g id="walls"><path class="wall"/></g>
  </g>
  <g id="grid"><path class="grid-line"/></g>
  <g id="vacuums">
    <g id="vacuum-trickyzanypartridge" class="vacuum state-error highlight"
       data-id="TrickyZanyPartridge" data-name="rocky7" data-room="Kitchen" data-state="error">
      <circle class="ring"/>                only when highlighted
      <circle class="body"/> <line class="heading"/> <text class="label">rocky7</text>
    </g>
  </g>
</svg>
```

Example: embed `/live.svg` inline and flag any vacuum in an error state, hide one robot, or tint a room:

```css
.vacuum.state-error .body { fill: #e61e1e; }
#vacuum-frugallamelion { display: none; }
.room[data-room="Kitchen"] { fill: #fff3c4; }
```

`/composite-map.svg` uses the same room, floor and wall paths, but one group per vacuum (reference first). Each vacuum's colors are rules in the `<style>` block (`#map-<id> .rooms`, `#map-<id> .walls`, `.charger[data-id=…]`), so restyling means editing or overriding CSS:

```text
<g id="maps">
  <g id="map-frugallamelion" class="map" data-id="FrugalLameLion" transform="…">
    <g class="rooms"><path class="room" data-room="Kitchen" …/></g>
    <g class="walls"><path class="wall"/></g>
  </g>
  …
</g>
<g id="grid">…</g>
<g id="chargers"><circle class="charger" data-id="FrugalLameLion"/>…</g>
```

## Alerts with Node-RED

[`examples/node-red/stuck-vacuum-alert.json`](examples/node-red/stuck-vacuum-alert.json) is an importable flow:

1. Listens on `valetudo/+/StatusStateAttribute/status` and passes on only `error`.
2. Calls `GET /vacuum/{id}` for the room, then `GET /vacuum/{id}.png` for the map.
3. Emits `msg.topic` = "rocky7 is stuck", `msg.payload` = "rocky7 reported an error in Kitchen.", `msg.image` = PNG Buffer.

To use it: import the flow, set the `TUDOMESH_URL` environment variable on the flow (default `http://tudomesh:8080`), point the MQTT node at your broker, and replace the debug node with your notifier (Telegram, Pushover, Home Assistant, ...).

## Auto-Calibration

TudoMesh keeps alignment up to date without manual work. When a robot finishes cleaning and docks, TudoMesh recalibrates its transform:

1. **Docking detection**: TudoMesh subscribes to each robot's `StatusStateAttribute/status` topic (derived from its map topic, see below). When the state changes to `docked`, recalibration starts.
2. **Debounce**: Skipped if that robot was calibrated less than 30 minutes ago and its map area has not changed.
3. **Map fetch**: The full map is fetched from the robot's REST API (`<url>/api/v2/robot/state/map`), retried up to 3 times with backoff, and saved as `ValetudoMapExport-<id>.json` in the data directory.
4. **Alignment**: The map is aligned against the reference robot with the same grid search as `--calibrate`.
5. **Cache update**: The new transform is written to `.calibration-cache.json`.

### State topic derivation

The state topic replaces the last two segments of the map topic:

| MapData topic | State topic |
|---|---|
| `valetudo/vacuum1/MapData/map-data` | `valetudo/vacuum1/StatusStateAttribute/status` |
| `home/floor1/valetudo/dusty/MapData/map-data` | `home/floor1/valetudo/dusty/StatusStateAttribute/status` |

Topics with fewer than 4 segments have no state topic, so those robots get no docking detection.

### Calibration cache

```bash
cat tudomesh-data/.calibration-cache.json
```

```json
{
  "referenceVacuum": "FrugalLameLion",
  "vacuums": {
    "FrugalLameLion": {
      "transform": { "a": 1, "b": 0, "tx": 0, "c": 0, "d": 1, "ty": 0 },
      "lastUpdated": 1771094662,
      "mapAreaAtCalibration": 912400
    },
    "AppropriateMinorAntelope": {
      "transform": { "a": 0.9997, "b": -0.026, "tx": -1165.1, "c": 0.026, "d": 0.9997, "ty": -706.1 },
      "lastUpdated": 1771094662,
      "mapAreaAtCalibration": 850900
    }
  }
}
```

Delete the file (or run `--calibrate`) to realign from scratch.

## CLI Mode

Without `--mqtt` or `--http`, TudoMesh works on the `ValetudoMapExport-*.json` files in the data directory and exits.

```bash
# Align the maps, print rotation and confidence per vacuum, write the calibration cache
./tudomesh --data-dir ./tudomesh-data --calibrate

# Render composite-map.png
./tudomesh --data-dir ./tudomesh-data --render

# Render SVG instead, or both (composite-map.png + composite-map.svg)
./tudomesh --data-dir ./tudomesh-data --render --format=vector
./tudomesh --data-dir ./tudomesh-data --render --format=both

# Grid lines every 500mm instead of 1m
./tudomesh --data-dir ./tudomesh-data --render --grid-spacing=500

# Alignment looks wrong for one robot: render all 4 rotations to compare
./tudomesh --data-dir ./tudomesh-data --compare-rotation=TrickyZanyPartridge
```

The vector output is the same structured SVG as `/composite-map.svg`.

### Planning room edits

Robots split and name rooms differently. `--plan-rooms` works out the Valetudo room edits that would make each robot's rooms match the reference robot's. It only simulates them: nothing is sent to the robots.

```bash
./tudomesh --data-dir ./tudomesh-data --plan-rooms
```

It uses each robot's current map from its API, or its map export when the robot is offline. For each robot it prints the edits in order, as the request bodies Valetudo takes, and how much of its room area matches the reference before and after:

- `split_segment`: cut a room along a straight line. Used where one robot room covers two reference rooms, and to cut off an area of at least 2.5 m² outside every reference room (for example a lidar artifact only that robot maps).
- `join_segments`: join two rooms that are the same reference room.
- `rename_segment`: give the room the reference's name.

It writes to the data dir:

| File | Content |
|------|---------|
| `rooms-<ref>.svg` | The reference rooms |
| `rooms-<id>-before.svg` | The robot's rooms today, in the reference frame, colored by the reference room they match; red where they disagree |
| `rooms-<id>-after.svg` | The same after the edits, with the cut lines dashed |
| `rooms-<id>-plan.json` | The edits as request bodies |

The segment ids in a plan are the simulation's. A robot may number new rooms differently, so re-plan after each edit you apply. Splits and joins renumber a robot's rooms, which breaks anything that cleans rooms by id (Valetudo timers, Home Assistant or Node-RED schedules).

### Flags

| Flag | Description |
|------|-------------|
| `--data-dir=DIR` | Directory for config, map exports and caches (default: current directory) |
| `--config=FILE` | Config file (default: `config.yaml` in `--data-dir`) |
| `--calibration-cache=FILE` | Calibration cache (default: `.calibration-cache.json` in `--data-dir`) |
| `--mqtt` | Service mode: follow robots over MQTT |
| `--http` | Service mode: serve the HTTP endpoints |
| `--http-port=PORT` | HTTP port (default 8080) |
| `--calibrate` | Align map exports, report rotation/confidence per vacuum, write the calibration cache |
| `--render` | Render the composite map from map exports |
| `--output=FILE` | Output file for `--render` (default `composite-map.png`) |
| `--format=raster\|vector\|both` | Render format: PNG, SVG, or both (default `raster`) |
| `--vector-format=svg` | Vector output format (only `svg`) |
| `--grid-spacing=MM` | Grid line spacing in millimeters (default 1000) |
| `--render-individual` | Render each vacuum's map as a separate PNG |
| `--individual-rotation=ID=DEG` | Rotation for `--render-individual` |
| `--compare-rotation=ID` | Render 4 rotation options for one vacuum |
| `--force-rotation=ID=DEG` | Force a vacuum's rotation (0, 90, 180, 270) |
| `--reference=ID` | Override the reference vacuum |
| `--rotate-all=DEG` | Rotate the whole composite (0, 90, 180, 270) |
| `--plan-rooms` | Plan room edits that match each robot's rooms to the reference's; writes previews, changes nothing |
| `--parse-only` | Print a summary of each map export and exit |

## Setting up Mosquitto

Skip this if your robots already publish to a broker.

```bash
# Docker
docker run -d --name mosquitto -p 1883:1883 eclipse-mosquitto:latest

# Ubuntu/Debian
sudo apt-get install mosquitto mosquitto-clients
sudo systemctl enable --now mosquitto

# macOS
brew install mosquitto
brew services start mosquitto
```

Recent Mosquitto versions only accept local connections by default. To accept robots on your network without authentication, add to `/etc/mosquitto/mosquitto.conf` (Linux) or `/opt/homebrew/etc/mosquitto/mosquitto.conf` (macOS) and restart:

```
listener 1883
allow_anonymous true
```

Check that your robots are publishing:

```bash
mosquitto_sub -h localhost -t 'valetudo/+/MapData/map-data' -C 1 | head -c 200
mosquitto_sub -h localhost -t 'valetudo/+/StatusStateAttribute/status' -v
```

## Troubleshooting

**"No MQTT broker configured" at startup:**
- Set `mqtt.broker` in `config.yaml` or the `MQTT_BROKER` environment variable. Without either, TudoMesh can only learn the broker from a robot that answers at startup.

**"No maps available" (503):**
- No robot's full map is known yet. Wait for a robot to dock (see [Auto-Calibration](#auto-calibration)), or copy a Valetudo map export (`ValetudoMapExport-*.json`) into the data directory and restart.

**A robot is misaligned:**
- Run `./tudomesh --data-dir ./tudomesh-data --calibrate` and check its confidence. If it reports `grid-ambiguous`, set a `rotation` hint for that vacuum in `config.yaml` (see `--compare-rotation` to pick one).

**Auto-calibration not triggering:**
- Look for `Discovered <url>` in the startup logs to confirm the robot was reached
- Look for `Subscribing to ... StatusStateAttribute/status`; if missing, the map topic has fewer than 4 segments
- Check that the robot reports `docked`: `mosquitto_sub -t 'valetudo/+/StatusStateAttribute/status'`

**Auto-calibration runs but the transform does not change:**
- The 30-minute debounce skips recalibration while the map area is unchanged. Force it with `--calibrate`.

**Map fetch failing during auto-calibration:**
- Check the map API is reachable from the TudoMesh host: `curl -H 'Accept: application/json' http://<robot>/api/v2/robot/state/map`

**`permission denied` saving the calibration cache (Docker):**
- The container runs as UID `65532`. Run `sudo chown -R 65532:65532 /your/local/path` on the mounted directory. Without it TudoMesh can read `config.yaml` but cannot save caches or maps.

## License

MIT License - See LICENSE file for details.

## Acknowledgments

- Built for use with [Valetudo](https://valetudo.cloud/) open-source vacuum firmware.
- Map alignment uses occupancy-grid search over floor and wall cells.
