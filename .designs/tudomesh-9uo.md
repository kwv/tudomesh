# tudomesh Data Pipeline & Coordinate Systems

## Overview

tudomesh ingests vacuum robot map data from two sources (MQTT and HTTP API),
aligns multiple vacuums via ICP, and renders composite maps. This document
describes each processing stage and the coordinate system in use at each point.

## Coordinate System Definitions

| Term | Units | Description |
|------|-------|-------------|
| **raw pixels** | grid indices (unitless) | Indices into the Valetudo pixel grid. `layer.Pixels = [x1,y1,x2,y2,...]` |
| **local-mm** | millimeters | Per-vacuum coordinate space. `entity.Points` are natively in local-mm. Pixel indices convert via `mm = index * pixelSize`. Not yet aligned with other vacuums. |
| **world-mm** | millimeters | Shared coordinate space after ICP alignment. `world = transform(local)` where transform is the ICP-computed AffineMatrix. |

## Current-State Pipeline

```mermaid
flowchart TD
    subgraph Ingest
        MQTT["MQTT Broker<br/><i>PNG+zTXt or JSON</i>"]
        HTTP["HTTP API<br/><i>/api/v2/robot/state/map</i>"]
        DISK["Disk Cache<br/><i>ValetudoMapExport-*.json</i>"]
    end

    subgraph Decode
        DEC["decoder.go<br/>DecodeMapData()"]
        PARSE["parser.go<br/>ParseMapJSON()"]
    end

    subgraph Store["In-Memory Store"]
        ST["StateTracker<br/>.maps[]<br/>.positions[]"]
    end

    subgraph Validate
        VAL["parser.go<br/>ValidateMapForCalibration()<br/>HasDrawablePixels()"]
    end

    subgraph "Feature Extraction"
        FEAT["features.go<br/>ExtractFeatures()"]
        VEC["vectorizer.go<br/>VectorizeLayer()"]
    end

    subgraph "ICP Alignment"
        ICP["icp.go<br/>AlignMaps()"]
        CAL["calibration.go<br/>CalibrationData"]
    end

    subgraph "GeoJSON Conversion"
        GEO["geojson.go<br/>MapToFeatureCollection()<br/>LayerToFeature()"]
    end

    subgraph "Map Unification"
        UNI["state.go / unifier.go<br/>UpdateUnifiedMap()<br/>UnifyWalls() / UnifyFloors()"]
    end

    subgraph Render
        RSVG["vector_renderer.go<br/>RenderToSVG()"]
        RPNG["renderer.go<br/>RenderToPNG()"]
        LIVE["vector_renderer.go<br/>RenderLiveToSVG()"]
    end

    subgraph Publish
        PUB["publisher.go<br/>PublishPosition()"]
        MQOUT["MQTT Out<br/>tudomesh/positions/*"]
    end

    MQTT -->|"raw bytes"| DEC
    HTTP -->|"JSON bytes"| PARSE
    DISK -->|"JSON file"| PARSE
    DEC -->|"ValetudoMap"| ST
    PARSE -->|"ValetudoMap"| ST

    ST -->|"on dock event"| VAL
    VAL -->|"valid map"| FEAT
    FEAT -->|"FeatureSet"| ICP
    ICP -->|"AffineMatrix"| CAL

    ST -->|"ValetudoMap per vacuum"| VEC
    VEC -->|"Path[] (pixel coords)"| GEO
    CAL -->|"transform"| GEO
    GEO -->|"FeatureCollection (world-mm)"| UNI

    UNI -->|"UnifiedMap"| RSVG
    ST -->|"maps + transforms"| RPNG
    ST -->|"maps + positions"| LIVE

    ST -->|"position (grid coords)"| PUB
    PUB --> MQOUT
```

## Coordinate Flow Detail

```mermaid
flowchart LR
    subgraph "Valetudo Source Data"
        LP["layer.Pixels<br/><b>raw pixels</b><br/>(grid indices)"]
        EP["entity.Points<br/><b>local-mm</b><br/>(millimeters)"]
        META["metaData.area<br/><b>mm²</b>"]
    end

    subgraph "Current Processing"
        P2P["PixelsToPoints()<br/>raw pixels → Point"]
        EF["ExtractFeatures()<br/><b>MIXED:</b><br/>pixels as grid indices<br/>charger as mm"]
        ICPa["ICP AlignMaps()<br/>operates at<br/><b>pixel scale</b>"]
        VL["VectorizeLayer()<br/>outputs<br/><b>pixel coords</b>"]
        LTF["LayerToFeature()<br/>transform at pixel scale<br/>then × pixelSize →<br/><b>world-mm</b>"]
    end

    subgraph "Position Pipeline"
        RP["ExtractRobotPosition()<br/><b>local-mm</b>"]
        DIV["÷ pixelSize<br/><b>grid coords</b>"]
        TP["TransformPoint()<br/><b>grid coords (aligned)</b>"]
        UP["UpdatePosition()<br/>stored as<br/><b>grid coords</b>"]
    end

    LP --> P2P --> EF
    EP -->|"charger"| EF
    EF --> ICPa
    ICPa -->|"AffineMatrix<br/>(pixel-scale)"| LTF
    LP --> VL --> LTF

    EP -->|"robot_position"| RP
    RP --> DIV --> TP --> UP

    style EF fill:#f96,stroke:#333
    style DIV fill:#f96,stroke:#333
```

> **Red nodes** indicate coordinate mismatches or unnecessary conversions in the current implementation.

### Current Issues

1. **Mixed coordinates in ExtractFeatures()**: `PixelsToPoints()` returns grid
   indices while `ExtractChargerPosition()` returns mm. Both are stored in the
   same `FeatureSet` and fed to ICP together.

2. **Position converted to grid then back**: Robot position arrives in mm
   (`entity.Points`), gets divided by `pixelSize` to grid coords, ICP
   transforms at grid scale, then the renderer multiplies back by `pixelSize`
   for display.

3. **ICP transform is pixel-scale**: The `AffineMatrix` from ICP encodes
   translation in grid units. Every consumer must remember to scale by
   `pixelSize` after applying the transform.

4. **VectorizeLayer outputs pixel coords**: Contour tracing works in grid
   space. `LayerToFeature` applies the ICP transform at pixel scale, then
   scales to mm — a two-step conversion at every render.

## Target-State Pipeline (after tudomesh-9uo)

```mermaid
flowchart LR
    subgraph "Valetudo Source Data"
        LP2["layer.Pixels<br/><b>raw pixels</b>"]
        EP2["entity.Points<br/><b>local-mm</b>"]
    end

    subgraph "Normalization (new)"
        NORM["NormalizeToMM()<br/>pixels × pixelSize → mm<br/>entities pass through"]
    end

    subgraph "Canonical Representation"
        CAN["All data in<br/><b>local-mm</b>"]
    end

    subgraph "Processing (simplified)"
        EF2["ExtractFeatures()<br/><b>local-mm only</b>"]
        ICP2["ICP AlignMaps()<br/>operates in<br/><b>mm</b>"]
        VL2["VectorizeLayer()<br/>outputs<br/><b>local-mm</b>"]
        LTF2["LayerToFeature()<br/>transform directly →<br/><b>world-mm</b>"]
    end

    subgraph "Position Pipeline (simplified)"
        RP2["ExtractRobotPosition()<br/><b>local-mm</b>"]
        TP2["TransformPoint()<br/><b>world-mm</b>"]
        UP2["UpdatePosition()<br/>stored as<br/><b>world-mm</b>"]
    end

    LP2 --> NORM
    EP2 --> NORM
    NORM --> CAN
    CAN --> EF2 --> ICP2
    ICP2 -->|"AffineMatrix<br/>(mm-scale)"| LTF2
    CAN --> VL2 --> LTF2

    CAN -->|"robot_position"| RP2
    RP2 --> TP2 --> UP2

    style NORM fill:#6f6,stroke:#333
    style CAN fill:#6f6,stroke:#333
```

### Key Changes

1. **Single normalization point**: All data converted to local-mm immediately
   on ingest. No mixed coordinate systems downstream.

2. **ICP in mm**: The `AffineMatrix` from ICP encodes translation in mm.
   Consumers apply transforms directly without scaling.

3. **Entity-first validation**: `ValidateMapForCalibration()` accepts maps
   with entity path points even when `layer.Pixels` is empty (Valetudo API
   now returns empty pixels in some states).

4. **Positions stay in mm**: No grid conversion. `local-mm → ICP transform → world-mm` is the only path.

## Pipeline Relationship: ICP vs GeoJSON

ICP and GeoJSON are **independent pipelines** that both consume pixel data separately.
GeoJSON is NOT used to drive ICP alignment — they serve different purposes:

- **ICP/features** (`features.go`): Point cloud alignment — finds the transform.
  `layer.Pixels → PixelsToPoints() → ExtractFeatures() → AlignMaps() → AffineMatrix`

- **GeoJSON/vectorizer** (`vectorizer.go`, `geojson.go`): Geometry conversion —
  applies the transform to produce polygons/linestrings for unified map rendering.
  `layer.Pixels → VectorizeLayer() → LayerToFeature(transform) → FeatureCollection`

Both independently process `layer.Pixels`, which means both break when pixels are
empty. After the mm refactor, entity path points (already vector data in mm) may
reduce the vectorizer's role for maps that lack pixel data.

## File Reference

| File | Role | Current Coords | Target Coords |
|------|------|---------------|---------------|
| `mesh/decoder.go` | MQTT payload decode | raw bytes → ValetudoMap | unchanged |
| `mesh/parser.go` | JSON parse, validation | grid indices + mm (mixed) | local-mm only |
| `mesh/types.go` | Data structures | Pixels: grid indices | Pixels: mm (or add CompressedPixels) |
| `mesh/features.go` | ICP feature extraction | grid indices (+ charger mm) | local-mm |
| `mesh/icp.go` | ICP alignment | pixel scale | mm scale |
| `mesh/vectorizer.go` | Contour tracing | grid → pixel coords | grid → local-mm |
| `mesh/geojson.go` | GeoJSON conversion | pixel → transform → ×pixelSize → world-mm | local-mm → transform → world-mm |
| `mesh/vector_renderer.go` | SVG rendering | pixel → ×pixelSize → world-mm | world-mm direct |
| `mesh/renderer.go` | PNG rendering | grid coords | world-mm |
| `mesh/state.go` | Map + position store | positions in grid coords | positions in mm |
| `mesh/publisher.go` | MQTT position publish | grid coords | world-mm |
| `mesh/calibration.go` | Transform persistence | pixel-scale AffineMatrix | mm-scale AffineMatrix |
| `app.go` | Orchestration | mm → ÷pixelSize → grid → transform | mm → transform → world-mm |
