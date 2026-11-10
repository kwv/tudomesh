# TudoMesh

Combines the maps of several [Valetudo](https://valetudo.cloud/) robot vacuums into one coordinate system and serves it over HTTP: a live map with every robot on it, and per-robot endpoints for "where is my stuck vacuum?" alerts.

- **Alignment from map shape alone**: no room names needed. Searches every rotation and offset for where floor lands on floor and wall on wall, and reports a confidence.
- **Live positions over MQTT**, realigned automatically each time a robot docks.
- **Alert endpoints**: the room a robot is in, and a map image of it circled.
- **Minimal config**: list each robot's Valetudo URL; ids, topics and broker are read from the robot.

New here? Follow the [setup guide](docs/setup.md).

## Install

```bash
docker run -d -p 8080:8080 -v /srv/tudomesh:/data kwv4/tudomesh
sudo chown -R 65532:65532 /srv/tudomesh   # the container's user needs write access
```

The image runs `--http --mqtt --data-dir=/data`. [`docker-compose.yml`](docker-compose.yml) adds a Mosquitto broker. From source (Go 1.26+): `make build`.

## Configuration

`config.yaml` in the data directory:

```yaml
mqtt:
  broker: mqtt://mqtt.home:1883
vacuums:
  - url: http://rockette.home
  - url: http://rocky7.home
```

Each robot's id (`systemId`), MQTT topic and map API are read from its Valetudo API and cached in `.vacuums.json`, so a robot that is offline at startup still resolves. [`config.example.yaml`](config.example.yaml) lists every option, including:

| Option | Purpose |
|---|---|
| `reference` | Robot whose rooms and walls the live map shows (default: largest map) |
| `ignoreRooms` | Rooms drawn as plain floor, e.g. `[Porch, Deck]`; matched ignoring case and punctuation |
| `vacuums[].rotation` | Rotation hint (0/90/180/270) when alignment is ambiguous |
| `render.theme`, `render.colors` | `light`, `dark` or `auto`, and color overrides |
| `render.fontPath` | Font for PNG text in scripts the bundled font lacks (e.g. CJK) |
| `gridSpacing` | Grid line spacing in cm (default 100) |

`MQTT_BROKER`, `MQTT_CLIENT_ID`, `MQTT_USERNAME` and `MQTT_PASSWORD` override the config.

Robots on different floors share no map: run one instance per floor, each with its own data directory and port.

## HTTP endpoints

| Endpoint | Returns |
|---|---|
| `/` | Page showing the live map |
| `/live.svg`, `/live.png` | Floor plan with every robot's live position |
| `/floorplan.svg` | Floor plan without robots |
| `/composite-map.svg`, `.png` | Every robot's map in its own color, to check alignment |
| `/vacuums` | JSON list of every robot |
| `/vacuum/{id}` | JSON: room, last status, position |
| `/vacuum/{id}.png`, `.svg` | Live map with that robot circled and its room hatched red |
| `/health` | Health check |

`{id}` is the robot's id or its MQTT name (`rocky7`), case-insensitive. Map endpoints return `503` until a map is loaded.

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

`room` comes from the robot's own map, so it is right even before maps are aligned. `state` needs `--mqtt`.

Query parameters:

- `?theme=light|dark|auto`: on every live, floorplan and per-vacuum view. `auto` lets the SVG follow the viewer's dark mode.
- `?crop=room` or `?crop=4m`: on `/vacuum/{id}.png|.svg`. Zooms to the robot's room (plus 1 m) or a radius around it, for phone notifications.
- `?scale=1..8`: on `/vacuum/{id}.png`. Image size (default 3).

The live views draw the reference robot's rooms, each outlined and labelled with its Valetudo name. The SVGs give every room and robot its own element with stable ids and `data-*` attributes, for restyling with CSS: see [SVG structure](docs/svg.md).

## Alerts

[`examples/node-red/stuck-vacuum-alert.json`](examples/node-red/stuck-vacuum-alert.json) is an importable Node-RED flow: on a robot's `error` status it fetches `/vacuum/{id}` and the cropped map image and emits both for your notifier. Set `TUDOMESH_URL` on the flow and point its MQTT node at your broker. Any tool that can fetch a URL on an MQTT message works the same way.

## Calibration

When a robot docks, TudoMesh fetches its full map from the Valetudo API, saves it as `ValetudoMapExport-<id>.json`, aligns it to the reference, and stores the result in `.calibration-cache.json`. Recalibration is skipped within 30 minutes if the map area is unchanged. Docking is read from the status topic next to the map topic (`.../StatusStateAttribute/status`).

To align now from the saved maps (using the config's `reference` and `rotation` hints):

```bash
./tudomesh --data-dir ./tudomesh-data --calibrate   # then restart the service
```

## CLI

Without `--mqtt` or `--http`, TudoMesh works on the map exports in `--data-dir` and exits.

| Flag | Description |
|------|-------------|
| `--data-dir=DIR` | Config, map exports and caches (default: current directory) |
| `--config=FILE`, `--calibration-cache=FILE` | Override the config and calibration cache paths (default: inside `--data-dir`) |
| `--mqtt`, `--http`, `--http-port=PORT` | Service mode (port default 8080) |
| `--calibrate` | Align the map exports, print rotation and confidence, write the calibration cache |
| `--compare-rotation=ID` | Render the 4 rotations of one robot, to pick a `rotation` hint |
| `--plan-rooms` | Preview Valetudo room edits that match each robot's rooms to the reference's; changes nothing. See [Planning room edits](docs/plan-rooms.md) |
| `--render`, `--format=raster\|vector\|both`, `--output=FILE` | Render the composite map |
| `--grid-spacing=CM` | Grid spacing for `--render` (default 100) |
| `--reference=ID` | Override the reference robot |
| `--force-rotation=ID=DEG`, `--rotate-all=DEG` | Force one robot's rotation; rotate the whole output |
| `--render-individual`, `--individual-rotation=ID=DEG` | Render each map separately |
| `--parse-only` | Summarize each map export |

## Troubleshooting

- **"No MQTT broker configured"**: set `mqtt.broker` or `MQTT_BROKER`.
- **503 "No maps available"**: no full map yet. Wait for a robot to dock, or save its map to the data directory: `curl -H "Accept: application/json" -o ValetudoMapExport-<id>.json http://<robot>/api/v2/robot/state/map`.
- **A robot is misaligned**: run `--calibrate`. If it reports `grid-ambiguous`, set that robot's `rotation` (use `--compare-rotation` to pick it), run `--calibrate` again and restart.
- **No recalibration on docking**: check the logs for `Discovered <url>` and `Subscribing to ... StatusStateAttribute/status`, and that the robot reports `docked` (`mosquitto_sub -t 'valetudo/+/StatusStateAttribute/status'`).
- **`permission denied` in Docker**: `sudo chown -R 65532:65532` the data directory.

## License

MIT. See [LICENSE](LICENSE).
