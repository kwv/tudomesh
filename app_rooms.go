package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/kwv/tudomesh/mesh"
)

// RunPlanRooms plans, for each non-reference robot, the Valetudo segment
// splits, joins and renames that would make its rooms match the reference
// robot's (#119). Maps come from each robot's API, else its export in the
// data dir. Nothing is sent to the robots. For each robot it prints the plan
// and writes to the data dir:
//
//	rooms-<id>-plan.json    the API request bodies, in order
//	rooms-<id>-before.svg   its rooms today, in the reference frame
//	rooms-<id>-after.svg    its rooms after the plan, with the cut lines
//	rooms-<ref>.svg         the reference rooms, for comparison
func (a *App) RunPlanRooms() {
	config, err := mesh.LoadConfig(a.ConfigFile)
	if err != nil {
		log.Fatalf("Error loading config %s: %v", a.ConfigFile, err)
	}
	mesh.ResolveVacuums(config, filepath.Join(a.DataDir, vacuumIdentityCache), discoveryTimeout)

	maps := map[string]*mesh.ValetudoMap{}
	for _, vc := range config.Vacuums {
		if vc.ApiURL != nil {
			if m, err := mesh.FetchMapFromAPI(*vc.ApiURL); err == nil {
				maps[vc.ID] = m
				fmt.Printf("%s: live map from %s\n", vc.ID, *vc.ApiURL)
				continue
			} else {
				log.Printf("%s: %v; trying the export", vc.ID, err)
			}
		}
		export := filepath.Join(a.DataDir, "ValetudoMapExport-"+vc.ID+".json")
		if m, err := mesh.ParseMapFile(export); err == nil {
			maps[vc.ID] = m
			fmt.Printf("%s: map from %s\n", vc.ID, export)
		} else {
			log.Printf("%s: no map (%v); skipped", vc.ID, err)
		}
	}

	ref := a.ReferenceVacuum
	if ref == "" {
		cache, _ := mesh.LoadCalibration(a.CalibrationCache)
		ref = mesh.GetEffectiveReference(config, cache, maps)
	}
	if maps[ref] == nil {
		log.Fatalf("No map for reference vacuum %q", ref)
	}
	refColors := mesh.RoomColors(maps[ref])
	fmt.Printf("Reference: %s\n\n", ref)

	refAssigned := map[string]string{}
	for seg := range refColors {
		refAssigned[seg] = seg
	}
	a.writeRoomSVG(ref, "", maps[ref], mesh.Identity(), refColors, refAssigned, nil, nil, ref+" (reference)")

	ids := make([]string, 0, len(maps))
	for id := range maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if id == ref {
			continue
		}
		var align mesh.Alignment
		if vc := config.GetVacuumByID(id); vc != nil && vc.Rotation != nil {
			align, _ = mesh.AlignMapsWithRotationHint(maps[id], maps[ref], *vc.Rotation)
		} else {
			align, _ = mesh.AlignMaps(maps[id], maps[ref])
		}
		plan := mesh.PlanRoomSync(maps[id], maps[ref], align.Transform)

		fmt.Printf("== %s (aligned %s, %.0f°)\n", id, align.Method, align.RotationDeg)
		fmt.Printf("   rooms match the reference: %.1f%% now, %.1f%% after %d edit(s)\n",
			plan.Before*100, plan.After*100, len(plan.Ops))
		for i, op := range plan.Ops {
			body, _ := json.Marshal(op)
			fmt.Printf("   %d. %s\n      %s\n", i+1, body, op.Reason)
		}
		if len(plan.Unmatched) > 0 {
			fmt.Printf("   left as is (no reference room): segments %v\n", plan.Unmatched)
		}
		fmt.Println()

		data, _ := json.MarshalIndent(plan.Ops, "", "  ")
		if err := os.WriteFile(filepath.Join(a.DataDir, "rooms-"+id+"-plan.json"), data, 0644); err != nil {
			log.Printf("Warning: %v", err)
		}
		a.writeRoomSVG(id, "-before", maps[id], align.Transform, refColors, plan.BeforeAssigned, plan.BeforeMismatch, nil,
			fmt.Sprintf("%s today: %.1f%% match", id, plan.Before*100))
		a.writeRoomSVG(id, "-after", plan.Result, align.Transform, refColors, plan.Assigned, plan.Mismatch, plan.Ops,
			fmt.Sprintf("%s after %d edit(s): %.1f%% match", id, len(plan.Ops), plan.After*100))
	}
	fmt.Printf("Previews written to %s/rooms-*.svg\n", a.DataDir)
}

func (a *App) writeRoomSVG(id, suffix string, m *mesh.ValetudoMap, toRef mesh.AffineMatrix, refColors map[string]color.NRGBA,
	assigned map[string]string, mismatch []int, ops []mesh.RoomOp, title string) {
	var buf bytes.Buffer
	if err := mesh.RenderRoomSyncSVG(&buf, m, toRef, refColors, assigned, mismatch, ops, title); err != nil {
		log.Printf("Warning: %s: %v", id, err)
		return
	}
	path := filepath.Join(a.DataDir, "rooms-"+id+suffix+".svg")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		log.Printf("Warning: %v", err)
	}
}
