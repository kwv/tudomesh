package mesh

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func seg(id, name string, px []int) MapLayer {
	return MapLayer{Type: "segment", MetaData: LayerMetaData{SegmentID: id, Name: name}, Pixels: px}
}

// refHouse is two 40x40 rooms side by side: A (x 0-39) and B (x 40-79).
func refHouse() *ValetudoMap {
	return &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("1", "A", cells(0, 0, 40, 40)),
		seg("2", "B", cells(40, 0, 40, 40)),
	}}
}

func opsByAction(ops []RoomOp) map[string]int {
	n := map[string]int{}
	for _, op := range ops {
		n[op.Action]++
	}
	return n
}

func TestPlanRoomSync_AlreadyMatching(t *testing.T) {
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("7", "A", cells(0, 0, 40, 40)),
		seg("8", "B", cells(40, 0, 40, 40)),
	}}
	plan := PlanRoomSync(robot, refHouse(), Identity())
	if len(plan.Ops) != 0 || plan.Before != 1 || plan.After != 1 {
		t.Errorf("ops=%+v before=%v after=%v, want no ops and full agreement", plan.Ops, plan.Before, plan.After)
	}
}

// The robot's boundary is 10 cells off: segment 1 covers all of A plus a
// strip of B. Expect one vertical cut at the reference boundary and a join
// of the strip into B.
func TestPlanRoomSync_SplitAndJoin(t *testing.T) {
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("1", "A", cells(0, 0, 50, 40)),
		seg("2", "B", cells(50, 0, 30, 40)),
	}}
	plan := PlanRoomSync(robot, refHouse(), Identity())

	if got := opsByAction(plan.Ops); got["split_segment"] != 1 || got["join_segments"] != 1 {
		t.Fatalf("ops = %v, want 1 split and 1 join: %+v", got, plan.Ops)
	}
	split := plan.Ops[0]
	if split.Action != "split_segment" || split.SegmentID != "1" {
		t.Fatalf("first op = %+v, want split of segment 1", split)
	}
	// Vertical cut at the boundary between grid x 39 and 40 (entity x ~197.5).
	for _, p := range []*Point{split.PA, split.PB} {
		if p.X < 190 || p.X > 205 {
			t.Errorf("cut point %+v, want x near 197.5", *p)
		}
	}
	if join := plan.Ops[1]; join.SegmentAID != "2" || join.SegmentBID != "3" {
		t.Errorf("join = %+v, want new segment 3 joined into 2", join)
	}
	if plan.Before >= 0.9 || plan.After != 1 {
		t.Errorf("agreement before=%.3f after=%.3f, want <0.9 then 1", plan.Before, plan.After)
	}
}

func TestPlanRoomSync_JoinAndRename(t *testing.T) {
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("1", "a", cells(0, 0, 40, 20)),
		seg("2", "", cells(0, 20, 40, 20)),
		seg("3", "B", cells(40, 0, 40, 40)),
	}}
	plan := PlanRoomSync(robot, refHouse(), Identity())
	want := []RoomOp{
		{Action: "join_segments", SegmentAID: "1", SegmentBID: "2"},
		{Action: "rename_segment", SegmentID: "1", Name: "A"},
	}
	if len(plan.Ops) != len(want) {
		t.Fatalf("ops = %+v, want %+v", plan.Ops, want)
	}
	for i, op := range plan.Ops {
		w := want[i]
		if op.Action != w.Action || op.SegmentID != w.SegmentID || op.SegmentAID != w.SegmentAID ||
			op.SegmentBID != w.SegmentBID || op.Name != w.Name {
			t.Errorf("op %d = %+v, want %+v", i, op, w)
		}
	}
}

// A large area beyond the reference rooms is cut off into its own,
// unmatched segment; a thin strip along a wall is left alone.
func TestPlanRoomSync_CutsOffOutsideArea(t *testing.T) {
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		// A plus a 40x30 area below the house (7.5 m² at 5 cm cells).
		seg("1", "A", cells(0, 0, 40, 70)),
		// B plus a 1-cell strip on its right edge.
		seg("2", "B", cells(40, 0, 41, 40)),
	}}
	plan := PlanRoomSync(robot, refHouse(), Identity())
	if got := opsByAction(plan.Ops); got["split_segment"] != 1 || len(plan.Ops) != 1 {
		t.Fatalf("ops = %+v, want one split", plan.Ops)
	}
	if op := plan.Ops[0]; op.SegmentID != "1" || op.PA.Y < 190 || op.PA.Y > 205 {
		t.Errorf("split = %+v, want segment 1 cut near y 197.5", op)
	}
	if len(plan.Unmatched) != 1 || plan.Unmatched[0] != "3" {
		t.Errorf("unmatched = %v, want the cut-off segment 3", plan.Unmatched)
	}
}

// A segment mostly outside the reference rooms matches nothing, so it is
// never joined into a room it merely touches.
func TestPlanRoomSync_OutsideSegmentNotJoined(t *testing.T) {
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("1", "A", cells(0, 0, 40, 40)),
		seg("2", "B", cells(40, 0, 40, 40)),
		seg("3", "", cells(0, 38, 40, 30)), // 2 rows inside A, 28 outside
	}}
	plan := PlanRoomSync(robot, refHouse(), Identity())
	if len(plan.Ops) != 0 {
		t.Errorf("ops = %+v, want none", plan.Ops)
	}
	if len(plan.Unmatched) != 1 || plan.Unmatched[0] != "3" {
		t.Errorf("unmatched = %v, want [3]", plan.Unmatched)
	}
}

// The robot's map is rotated 90° from the reference; toRef undoes it.
func TestPlanRoomSync_Rotated(t *testing.T) {
	// Robot cell (x, y) is reference cell (y, 39-x): robot rows 0-39 are
	// reference A, rows 40-79 reference B.
	toRef := AffineMatrix{A: 0, B: 1, Tx: 0, C: -1, D: 0, Ty: 39}
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("1", "A", cells(0, 0, 40, 50)), // A plus 10 rows of B
		seg("2", "B", cells(0, 50, 40, 30)),
	}}
	plan := PlanRoomSync(robot, refHouse(), toRef)
	if got := opsByAction(plan.Ops); got["split_segment"] != 1 || got["join_segments"] != 1 {
		t.Fatalf("ops = %+v, want 1 split and 1 join", plan.Ops)
	}
	if split := plan.Ops[0]; split.PA.Y < 190 || split.PA.Y > 205 || split.PB.Y < 190 || split.PB.Y > 205 {
		t.Errorf("split = %+v, want a horizontal cut near y 197.5 in robot coordinates", split)
	}
	if plan.After != 1 {
		t.Errorf("after = %v, want 1", plan.After)
	}
}

func TestRenderRoomSyncSVG(t *testing.T) {
	robot := &ValetudoMap{PixelSize: 5, Layers: []MapLayer{
		seg("1", "A", cells(0, 0, 50, 40)),
		seg("2", "B", cells(50, 0, 30, 40)),
		{Type: "wall", Pixels: []int{80, 0, 80, 1}},
	}}
	ref := refHouse()
	plan := PlanRoomSync(robot, ref, Identity())

	var buf bytes.Buffer
	if err := RenderRoomSyncSVG(&buf, plan.Result, Identity(), RoomColors(ref), plan.Assigned, plan.Mismatch, plan.Ops, "test"); err != nil {
		t.Fatal(err)
	}
	var root svgNode
	if err := xml.Unmarshal(buf.Bytes(), &root); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if n := len(root.find(func(n svgNode) bool { return n.attr("class") == "cut" })); n != 1 {
		t.Errorf("cut lines = %d, want 1", n)
	}
	if n := len(root.find(func(n svgNode) bool { return n.attr("class") == "label" })); n != 2 {
		t.Errorf("labels = %d, want 2 (one per segment after the join)", n)
	}
}
