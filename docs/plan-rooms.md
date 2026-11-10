# Planning room edits

Robots split and name rooms differently. `--plan-rooms` works out the Valetudo room edits that would make each robot's rooms match the reference robot's rooms, in shape and name. It only simulates them: nothing is sent to the robots. Room names can be anything, in any language; they are copied from the reference as written.

Matching rooms is optional: alignment and alerts work without it. It helps when something picks rooms by name across robots, like a scheduler that sends whichever robot is free to "Kitchen".

How-to:

1. List every robot's `url` in `config.yaml` and start TudoMesh once, so the maps are fetched and aligned.
2. Open `/composite-map.svg` and check the maps line up. If one is rotated wrongly, set its `rotation` (see [Troubleshooting](../README.md#troubleshooting)); a plan built on a wrong alignment is wrong too.
3. Optionally name the reference robot's rooms in Valetudo the way you want them everywhere. The reference is `reference` in config, else the robot with the largest map.
4. Run the planner:

   ```bash
   ./tudomesh --data-dir ./tudomesh-data --plan-rooms
   # Docker: docker exec <container> /app/tudomesh --data-dir /data --plan-rooms
   ```

5. Read the plan and open `rooms-<id>-after.svg`. Apply only the **first** edit, in the Valetudo UI or with the printed `PUT`.
6. Run `--plan-rooms` again and repeat until each robot shows "Nothing to change". Valetudo renumbers rooms on a split or join and can clear room names, so later steps of an old plan may point at the wrong room.

Example output:

```
== rocky7 (aligned by grid, rotated 179°)
   Rooms match the reference on 63.6% of their area now, 100.0% after 2 edit(s):
   1. Split segment 10 along the line (198,-10)-(198,205)
      why: segment 10 ("Raum 1") is reference room Küche (1) plus reference room Wohnzimmer (2); the cut makes new segment 12
      PUT http://rocky7.home/api/v2/robot/capabilities/MapSegmentEditCapability
      {"action":"split_segment","segment_id":"10","pA":{"x":198,"y":-10},"pB":{"x":198,"y":205}}
   2. Rename segment 10 to "Küche"
      ...
```

The edits:

- `split_segment`: cut a room along a straight line. Used where one robot room covers two reference rooms, and to cut off an area of at least 2.5 m² outside every reference room (for example a lidar artifact only that robot maps).
- `join_segments`: join two rooms that are the same reference room.
- `rename_segment`: give the room the reference's name. Not planned when the reference room has no name.

Rooms with no reference room at that place (a deck only one robot reaches) are listed as left as is. With a single robot there is nothing to match, and the planner says so.

It writes to the data dir:

| File | Content |
|------|---------|
| `rooms-<ref>.svg` | The reference rooms |
| `rooms-<id>-before.svg` | The robot's rooms today, in the reference frame, colored by the reference room they match; red where they disagree |
| `rooms-<id>-after.svg` | The same after the edits, with the cut lines dashed |
| `rooms-<id>-plan.json` | The edits as request bodies |

Splits and joins renumber a robot's rooms, which breaks anything that cleans rooms by id (Valetudo timers, Home Assistant or Node-RED schedules); update those afterwards.
