# SVG structure

The SVG maps are structured so they can be restyled with CSS or scripted.

## Live views

`/live.svg`, `/floorplan.svg` and `/vacuum/{id}.svg`:

```text
<svg>
  <style>…</style>                          theme variables (--tm-*) and rules; override freely
  <g id="map" transform="…">                map geometry in grid cells
    <g id="rooms">
      <path id="room-16" class="room" data-room="Kitchen" data-segment-id="16"/>
      <path class="floor"/>                 unsegmented floor
    </g>
    <path id="alert-room" class="alert-room"  /vacuum/{id}.svg only: the robot's room outline
          data-room="Kitchen" data-segment-id="4" data-vacuum="TidyCalmPanda"/>
    <g id="walls"><path class="wall"/></g>
    <g id="room-outlines">
      <path class="room-outline" data-room="Kitchen" data-segment-id="16"/>
    </g>
  </g>
  <g id="grid"><path class="grid-line"/></g>
  <g id="room-labels">                     upright text, only for named rooms it fits in
    <text class="room-label" data-room="Kitchen" data-segment-id="16">Kitchen</text>
  </g>
  <g id="vacuums">
    <g id="vacuum-tidycalmpanda" class="vacuum state-error highlight"
       data-id="TidyCalmPanda" data-name="robo7" data-room="Kitchen" data-state="error">
      <circle class="ring"/>                only when highlighted
      <circle class="body"/> <line class="heading"/> <text class="label">robo7</text>
    </g>
  </g>
</svg>
```

`#alert-room` is drawn over the rooms and under the walls. Its `data-segment-id` is the segment in the map it came from (the robot's own map, or the reference map as a fallback). The default style is a red fill with a dashed dark red outline, so it does not rely on color alone; restyle it with `.alert-room { … }` (stroke widths are in grid cells).

Example: embed `/live.svg` inline and flag any vacuum in an error state, hide one robot, or tint a room:

```css
.vacuum.state-error .body { fill: #e61e1e; }
#vacuum-fairboldfalcon { display: none; }
.room[data-room="Kitchen"] { fill: #fff3c4; }
.room-outline { stroke: #446; stroke-opacity: 1; }
.room-label { font-family: "Noto Sans", sans-serif; fill: #124; }
#room-labels { display: none; }                      /* hide all room names */
#room-outlines { display: none; }                    /* hide all outlines */
.room-label[data-room="UtilityCloset"] { display: none; }  /* hide one room's name */
```

## Composite view

`/composite-map.svg` uses the same room, floor and wall paths, but one group per vacuum (reference first). Each vacuum's colors are rules in the `<style>` block (`#map-<id> .rooms`, `#map-<id> .walls`, `.charger[data-id=…]`), so restyling means editing or overriding CSS:

```text
<g id="maps">
  <g id="map-fairboldfalcon" class="map" data-id="FairBoldFalcon" transform="…">
    <g class="rooms"><path class="room" data-room="Kitchen" …/></g>
    <g class="walls"><path class="wall"/></g>
  </g>
  …
</g>
<g id="grid">…</g>
<g id="chargers"><circle class="charger" data-id="FairBoldFalcon"/>…</g>
```

## Theme variables

The theme (`render.theme`, `?theme=`) is a set of CSS variables on the `<svg>` element, so a page that embeds the SVG inline can override them:

```css
#map-panel svg { --tm-bg: transparent; --tm-wall: #888; --tm-room-0: #234; }
```

`--tm-bg`, `--tm-floor`, `--tm-wall`, `--tm-text`, `--tm-halo`, `--tm-alert`, `--tm-alert-border`, and `--tm-room-0`, `--tm-room-1`, … (one per palette color in use).
