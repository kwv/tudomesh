package main

import (
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kwv/tudomesh/mesh"
)

// consensusID is the map key for the consensus floorplan in renderers.
const consensusID = "consensus"

// writeFloorplan builds the consensus floorplan and writes
// <output>-floorplan.png and/or .svg (per format), printing per-vacuum and
// consensus cell counts.
func (a *App) writeFloorplan(maps map[string]*mesh.ValetudoMap, transforms map[string]mesh.AffineMatrix, refID string, config *mesh.Config, format string) {
	cm := printConsensus(maps, transforms, refID)
	if cm == nil {
		log.Printf("Warning: no map geometry; floorplan not written")
		return
	}

	base := strings.TrimSuffix(a.OutputFile, filepath.Ext(a.OutputFile)) + "-floorplan"
	single := map[string]*mesh.ValetudoMap{consensusID: cm}
	identity := map[string]mesh.AffineMatrix{consensusID: mesh.Identity()}

	if format == "raster" || format == "both" {
		r := mesh.NewCompositeRenderer(single, identity, consensusID)
		r.GlobalRotation = a.RotateAll
		if err := writePNGFile(base+".png", r); err != nil {
			log.Fatalf("Error writing floorplan PNG: %v", err)
		}
		fmt.Printf("Created floorplan raster: %s.png\n", base)
	}
	if format == "vector" || format == "both" {
		vr := mesh.NewVectorRenderer(single, identity, consensusID)
		vr.GlobalRotation = a.RotateAll
		spacing := a.GridSpacing
		if config != nil && config.GridSpacing > 0 {
			spacing = config.GridSpacing
		}
		if spacing > 0 {
			vr.GridSpacing = spacing
			vr.Padding = spacing / 2
		}
		f, err := os.Create(base + ".svg")
		if err != nil {
			log.Fatalf("Error creating %s.svg: %v", base, err)
		}
		if err := vr.RenderLiveToSVG(f, nil); err != nil {
			log.Fatalf("Error rendering floorplan SVG: %v", err)
		}
		if err := f.Close(); err != nil {
			log.Fatalf("Error closing %s.svg: %v", base, err)
		}
		fmt.Printf("Created floorplan SVG: %s.svg\n", base)
	}
}

// printConsensus builds the consensus floorplan and prints floor/wall cell
// counts and rooms for each vacuum and for the result. Returns nil when no map
// has geometry.
func printConsensus(maps map[string]*mesh.ValetudoMap, transforms map[string]mesh.AffineMatrix, refID string) *mesh.ValetudoMap {
	cm := mesh.BuildConsensusMap(maps, transforms, refID)
	if cm == nil {
		return nil
	}
	fmt.Printf("\nConsensus floorplan (reference %s):\n", refID)
	fmt.Printf("  %-26s %8s %8s  %s\n", "", "floor", "wall", "rooms")
	ids := make([]string, 0, len(maps))
	for id := range maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		printMapCounts(id, maps[id])
	}
	printMapCounts(consensusID, cm)
	return cm
}

func writePNGFile(path string, r *mesh.CompositeRenderer) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, r.RenderLive(nil)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// printMapCounts prints floor (floor + segment) and wall cell counts and the
// named rooms of one map.
func printMapCounts(id string, m *mesh.ValetudoMap) {
	floor, wall := 0, 0
	var rooms []string
	for _, l := range m.Layers {
		n := len(l.Pixels) / 2
		switch l.Type {
		case "wall":
			wall += n
		case "floor":
			floor += n
		case "segment":
			floor += n
			name := l.MetaData.Name
			if name == "" {
				name = "#" + l.MetaData.SegmentID
			}
			rooms = append(rooms, fmt.Sprintf("%s(%d)", name, n))
		}
	}
	fmt.Printf("  %-26s %8d %8d  %d: %s\n", id, floor, wall, len(rooms), strings.Join(rooms, " "))
}
