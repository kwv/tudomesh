# Setting up TudoMesh

A step-by-step guide for a fresh install with your own Valetudo robots: one robot or several, any floor plan, any room names. The [README](../README.md) has the reference for every option mentioned here.

## 1. Check the robots

For each robot:

- It runs [Valetudo](https://valetudo.cloud/) and its web UI opens from the machine that will run TudoMesh (`http://<robot>/`).
- MQTT is enabled in Valetudo (Settings → Connectivity → MQTT) and connected to a broker. No broker yet? See [setting up Mosquitto](#appendix-setting-up-mosquitto).
- It has a complete map, from at least one full cleaning or mapping run.

Confirm the robots publish to the broker:

```bash
mosquitto_sub -h <broker> -t 'valetudo/+/StatusStateAttribute/status' -v
# valetudo/robo7/StatusStateAttribute/status {"value":"docked",...}
```

`valetudo` is Valetudo's default topic prefix; TudoMesh reads each robot's prefix from the robot, so a custom one works too.

Robots on different floors share no map. Set up one TudoMesh per floor (step 2 for each), with its own data directory and port. A robot alone on a floor works fine: it becomes its own reference.

## 2. Install and configure

Make a data directory with a `config.yaml` that names the broker and each robot's Valetudo URL:

```bash
mkdir -p /srv/tudomesh
cat > /srv/tudomesh/config.yaml <<'EOF'
mqtt:
  broker: mqtt://<broker>:1883
vacuums:
  - url: http://<robot1>
  - url: http://<robot2>
EOF
sudo chown -R 65532:65532 /srv/tudomesh   # the container's user
```

Start it:

```bash
docker run -d --name tudomesh -p 8080:8080 -v /srv/tudomesh:/data kwv4/tudomesh
```

Everything else is optional; [`config.example.yaml`](../config.example.yaml) lists it. To build from source instead: `make build`.

## 3. Check every robot is found

```bash
docker logs tudomesh | grep Discovered
# Discovered http://<robot1>: id=TidyCalmPanda name=robo7 topic=valetudo/robo7/MapData/map-data
curl http://<host>:8080/vacuums
```

Each robot is listed with its id (Valetudo's `systemId`) and name (its MQTT identifier, e.g. `robo7`). Either one works in `/vacuum/{id}` URLs. A robot that is missing or listed with a guessed name was unreachable: check its URL from the TudoMesh host.

## 4. Load and align the maps

TudoMesh needs each robot's full map. It fetches it, and aligns the robot, every time the robot docks. Either wait for each robot to finish a run, or fetch the maps now and align them yourself:

```bash
# once per robot; <id> is the id from /vacuums
curl -H "Accept: application/json" -o /srv/tudomesh/ValetudoMapExport-<id>.json http://<robot>/api/v2/robot/state/map
sudo chown 65532:65532 /srv/tudomesh/*.json

docker run --rm -v /srv/tudomesh:/data kwv4/tudomesh --data-dir=/data --calibrate
docker restart tudomesh
```

`--calibrate` prints one result per robot:

```
TidyCalmPanda      :
  Method: grid, rotation 178.0°, confidence 1.55 (>= 1.25 is unambiguous)
```

Then open `http://<host>:8080/composite-map.svg`. Each robot's map is drawn in its own color; their walls should lie on top of each other.

If a robot's map sits rotated or in the wrong place (its method says `grid-ambiguous`, or the walls clearly don't overlap), give it a rotation hint and calibrate again:

```yaml
vacuums:
  - url: http://<robot2>
    rotation: 180   # 0, 90, 180 or 270; --compare-rotation=<id> renders all four
```

```bash
docker run --rm -v /srv/tudomesh:/data kwv4/tudomesh --data-dir=/data --calibrate
docker restart tudomesh
```

Alignment uses the shape of the maps only, so robots with different room names, or none, align fine. With a single robot there is nothing to align; skip to step 5.

## 5. Tidy the live map

Open `http://<host>:8080/`: the live map with every robot on it.

- **Rooms come from one robot**, the reference: the one with the largest map unless `reference: <id>` says otherwise. Pick the robot whose rooms are split and named the way you like.
- **Hide areas** that aren't really rooms, like a porch seen through glass, with `ignoreRooms: [Porch]`.
- **Dark map:** `render.theme: dark`, or `?theme=dark` on any map URL.
- **Other alphabets:** the bundled PNG font covers Latin, Greek and Cyrillic. For others (e.g. CJK), set `render.fontPath` to a font file mounted into the container.

Restart TudoMesh after editing `config.yaml`.

## 6. Look up a robot

Each robot has its own JSON and map image:

```bash
curl http://<host>:8080/vacuum/robo7
# {"id":"TidyCalmPanda","name":"robo7","state":"docked","room":{"name":"Kitchen",...},...}
curl -o robo7.png 'http://<host>:8080/vacuum/robo7.png?crop=room'
```

The image shows the robot circled, its room hatched in red, and a caption. `?crop=room` zooms in on its room, `?crop=4m` on a radius around it, and `?scale=` sets the size.

## 7. Optional: give the robots the same rooms

If something picks rooms by name across robots, for example a scheduler that sends whichever robot is free to "Kitchen", make each robot's rooms match the reference's with `--plan-rooms`. It previews the Valetudo room edits and changes nothing. See [Planning room edits](plan-rooms.md).

## Problems?

See [Troubleshooting](../README.md#troubleshooting).

## Appendix: setting up Mosquitto

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
