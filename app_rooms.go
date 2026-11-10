package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	configPath, cachePath := a.dataDirPaths()
	config, err := mesh.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error loading config %s: %v", configPath, err)
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
		cache, _ := mesh.LoadCalibration(cachePath)
		ref = mesh.GetEffectiveReference(config, cache, maps)
	}
	if err := a.planRooms(os.Stdout, config, maps, ref); err != nil {
		log.Fatal(err)
	}
}

// planRooms prints the plan for every robot in maps other than ref and
// writes the previews and plan files (see RunPlanRooms).
func (a *App) planRooms(w io.Writer, config *mesh.Config, maps map[string]*mesh.ValetudoMap, ref string) error {
	if len(maps) == 0 {
		return fmt.Errorf("no robot maps: check the robots' urls in config, or put ValetudoMapExport-<id>.json files in %s", a.DataDir)
	}
	if maps[ref] == nil {
		return fmt.Errorf("no map for reference robot %q; set reference to one of %v, or check that robot's url", ref, sortedMapIDs(maps))
	}
	refColors := mesh.RoomColors(maps[ref])
	refAssigned := map[string]string{}
	for seg := range refColors {
		refAssigned[seg] = seg
	}
	a.writeRoomSVG(ref, "", maps[ref], mesh.Identity(), refColors, refAssigned, nil, nil, ref+" (reference)")
	_, _ = fmt.Fprintf(w, "Reference: %s (its rooms are the target; preview %s)\n\n", ref, filepath.Join(a.DataDir, "rooms-"+ref+".svg"))

	named := 0
	for _, l := range maps[ref].Layers {
		if l.Type == "segment" && l.MetaData.Name != "" {
			named++
		}
	}
	if named == 0 {
		_, _ = fmt.Fprintf(w, "Note: %s's rooms have no names, so no renames are planned. Name them in Valetudo first if you want the robots' rooms named alike.\n\n", ref)
	}

	if len(maps) == 1 {
		_, _ = fmt.Fprintf(w, "Only %s has a map, so there is nothing to plan: --plan-rooms matches other robots' rooms to the reference robot's.\n", ref)
		return nil
	}

	total := 0
	for _, id := range sortedMapIDs(maps) {
		if id == ref {
			continue
		}
		var align mesh.Alignment
		vc := config.GetVacuumByID(id)
		if vc != nil && vc.Rotation != nil {
			align, _ = mesh.AlignMapsWithRotationHint(maps[id], maps[ref], *vc.Rotation)
		} else {
			align, _ = mesh.AlignMaps(maps[id], maps[ref])
		}
		plan := mesh.PlanRoomSync(maps[id], maps[ref], align.Transform)
		baseURL := ""
		if vc != nil {
			baseURL = vc.BaseURL()
		}
		writePlan(w, id, baseURL, align, plan)
		total += len(plan.Ops)

		if err := os.WriteFile(filepath.Join(a.DataDir, "rooms-"+id+"-plan.json"), planJSON(plan.Ops), 0644); err != nil {
			log.Printf("Warning: %v", err)
		}
		a.writeRoomSVG(id, "-before", maps[id], align.Transform, refColors, plan.BeforeAssigned, plan.BeforeMismatch, nil,
			fmt.Sprintf("%s today: %.1f%% match", id, plan.Before*100))
		a.writeRoomSVG(id, "-after", plan.Result, align.Transform, refColors, plan.Assigned, plan.Mismatch, plan.Ops,
			fmt.Sprintf("%s after %d edit(s): %.1f%% match", id, len(plan.Ops), plan.After*100))
	}
	_, _ = fmt.Fprintf(w, "Previews: %s (before/after per robot). Nothing was sent to the robots.\n", filepath.Join(a.DataDir, "rooms-*.svg"))
	if total > 0 {
		_, _ = fmt.Fprint(w, `
To apply a plan, for one robot at a time:
  1. Check rooms-<id>-after.svg looks right.
  2. Apply only the FIRST edit (Valetudo UI, or the PUT above).
  3. Splits and joins renumber rooms and can clear room names: run --plan-rooms
     again and follow the new plan; repeat until it has no edits left.
  4. Update anything that cleans rooms by segment id (timers, schedules).
`)
	}
	return nil
}

// writePlan prints one robot's plan: the match before and after, then each
// edit in words, its reason, and the request that applies it.
func writePlan(w io.Writer, id, baseURL string, align mesh.Alignment, plan mesh.RoomSyncPlan) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
	p("== %s (aligned by %s, rotated %.0f°)\n", id, align.Method, align.RotationDeg)
	if align.Method == "grid-ambiguous" {
		p("   WARNING: the alignment is uncertain, so this plan may be wrong. Check\n" +
			"   /composite-map.svg and set rotation for this robot in config first.\n")
	}
	if len(plan.Ops) == 0 {
		p("   Rooms already match the reference (%.1f%% of room area). Nothing to change.\n", plan.Before*100)
	} else {
		p("   Rooms match the reference on %.1f%% of their area now, %.1f%% after %d edit(s):\n",
			plan.Before*100, plan.After*100, len(plan.Ops))
	}
	for i, op := range plan.Ops {
		p("   %d. %s\n", i+1, op.Describe())
		if op.Reason != "" {
			p("      why: %s\n", op.Reason)
		}
		p("      PUT %s%s\n      %s\n", baseURL, op.Endpoint(), op.Body())
	}
	if len(plan.Unmatched) > 0 {
		names := map[string]string{}
		if plan.Result != nil {
			for _, l := range plan.Result.Layers {
				if l.Type == "segment" {
					names[l.MetaData.SegmentID] = l.MetaData.Name
				}
			}
		}
		var segs []string
		for _, seg := range plan.Unmatched {
			if n := names[seg]; n != "" {
				seg += fmt.Sprintf(" (%s)", n)
			}
			segs = append(segs, seg)
		}
		p("   Left as is, no reference room there: segment %s\n", strings.Join(segs, ", "))
	}
	p("\n")
}

// planJSON is ops as an indented JSON array of request bodies, names unescaped.
func planJSON(ops []mesh.RoomOp) []byte {
	if ops == nil {
		ops = []mesh.RoomOp{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ops)
	return buf.Bytes()
}

func sortedMapIDs(maps map[string]*mesh.ValetudoMap) []string {
	ids := make([]string, 0, len(maps))
	for id := range maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
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
