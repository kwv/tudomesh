package mesh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Room sync planning (#119): which Valetudo segment edits would make a
// robot's rooms match the reference robot's rooms. Planning only simulates
// the edits on a copy of the robot's map; nothing is sent to the robot.

// RoomOp is one Valetudo segment edit, as the robot's API takes it:
//   - split_segment and join_segments: PUT .../MapSegmentEditCapability
//   - rename_segment: PUT .../MapSegmentRenameCapability
//
// Points are in the robot's own map (entity units). Segment ids are the ids
// the simulation assigns; the robot may number new segments differently, so
// a real run must re-plan after each edit.
type RoomOp struct {
	Action     string `json:"action"`
	SegmentID  string `json:"segment_id,omitempty"`
	SegmentAID string `json:"segment_a_id,omitempty"`
	SegmentBID string `json:"segment_b_id,omitempty"`
	PA         *Point `json:"pA,omitempty"`
	PB         *Point `json:"pB,omitempty"`
	Name       string `json:"name,omitempty"`
	// Reason says why the edit is planned, for people reading the plan.
	Reason string `json:"-"`
}

// Endpoint is the Valetudo API path op is sent to with PUT.
func (op RoomOp) Endpoint() string {
	if op.Action == "rename_segment" {
		return "/api/v2/robot/capabilities/MapSegmentRenameCapability"
	}
	return "/api/v2/robot/capabilities/MapSegmentEditCapability"
}

// Body is op's request body as JSON, with names unescaped (UTF-8, & < >).
func (op RoomOp) Body() []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(op)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// Describe says what op does in words, e.g. `Rename segment 4 to "Kitchen"`.
// Points are in the robot's map (cm).
func (op RoomOp) Describe() string {
	switch op.Action {
	case "split_segment":
		if op.PA != nil && op.PB != nil {
			return fmt.Sprintf("Split segment %s along the line (%.0f,%.0f)-(%.0f,%.0f)",
				op.SegmentID, op.PA.X, op.PA.Y, op.PB.X, op.PB.Y)
		}
		return "Split segment " + op.SegmentID
	case "join_segments":
		return fmt.Sprintf("Join segments %s and %s", op.SegmentAID, op.SegmentBID)
	case "rename_segment":
		return fmt.Sprintf("Rename segment %s to %q", op.SegmentID, op.Name)
	}
	return op.Action
}

// RoomSyncPlan is the planned edits for one robot and their simulated result.
type RoomSyncPlan struct {
	Ops []RoomOp
	// Agreement is the share of the robot's room cells whose room matches
	// the reference room at the same place, before and after the edits.
	Before, After float64
	// Result is the robot's map with the edits applied.
	Result *ValetudoMap
	// Assigned maps each segment id to the reference segment id it matches
	// ("" when it matches none); Mismatch is the robot grid cells [x,y,...]
	// whose reference room differs from their segment's. Before* describe
	// the robot's map as it is, the others Result.
	BeforeAssigned, Assigned map[string]string
	BeforeMismatch, Mismatch []int
	// Unmatched lists Result's segments that match no reference room.
	Unmatched []string
}

// Tunables for PlanRoomSync.
const (
	// A reference room must cover at least this share of a robot segment,
	// and at least roomSyncMinCells cells, before the segment is split.
	roomSyncMinShare = 0.15
	roomSyncMinCells = 200
	// An area outside every reference room is cut off only when it is at
	// least this many square meters; smaller ones are alignment noise along walls.
	roomSyncMinOutsideM2 = 2.5
	// A split must put at least this share of the minority room's cells on
	// their own side, or the boundary isn't straight enough to cut.
	roomSyncMinSplitGain = 0.5
	// Cells within this many cells of the other side count as boundary.
	roomSyncBoundaryReach = 3
	// Segments within this many cells of each other count as adjacent.
	roomSyncAdjacency = 2
	roomSyncMaxOps    = 50
)

type cell struct{ x, y int }

// roomSyncState is the robot's segments as a cell -> segment id labelling,
// plus each cell's reference room.
type roomSyncState struct {
	label    map[cell]string // robot cell -> robot segment id
	ref      map[cell]string // robot cell -> reference segment id ("" if none)
	names    map[string]string
	refNames map[string]string
	ps       float64
}

// PlanRoomSync plans splits, joins and renames that make robot's segments
// match ref's. toRef maps robot grid cells to ref grid cells (the
// calibration transform).
func PlanRoomSync(robot, ref *ValetudoMap, toRef AffineMatrix) RoomSyncPlan {
	s := newRoomSyncState(robot, ref, toRef)

	plan := RoomSyncPlan{Before: s.agreement()}
	plan.BeforeAssigned, plan.BeforeMismatch = s.match()
	for len(plan.Ops) < roomSyncMaxOps {
		op, ok := s.nextSplit()
		if !ok {
			op, ok = s.nextJoin()
		}
		if !ok {
			break
		}
		plan.Ops = append(plan.Ops, op)
	}

	plan.Assigned = map[string]string{}
	for _, seg := range s.segments() {
		r := s.majority(seg)
		plan.Assigned[seg] = r
		if r == "" {
			plan.Unmatched = append(plan.Unmatched, seg)
			continue
		}
		if want := s.refNames[r]; want != "" && s.names[seg] != want && s.isLargestFor(seg, r) {
			plan.Ops = append(plan.Ops, RoomOp{
				Action: "rename_segment", SegmentID: seg, Name: want,
				Reason: fmt.Sprintf("segment %s (%s) is the reference's %q", seg, nameOrUnnamed(s.names[seg]), want),
			})
			s.names[seg] = want
		}
	}
	plan.After = s.agreement()
	_, plan.Mismatch = s.match()
	plan.Result = s.toMap(robot)
	return plan
}

func newRoomSyncState(robot, ref *ValetudoMap, toRef AffineMatrix) *roomSyncState {
	refLabel, refNames := map[cell]string{}, map[string]string{}
	for _, l := range ref.Layers {
		if l.Type == "segment" {
			refNames[l.MetaData.SegmentID] = l.MetaData.Name
			for i := 0; i+1 < len(l.Pixels); i += 2 {
				refLabel[cell{l.Pixels[i], l.Pixels[i+1]}] = l.MetaData.SegmentID
			}
		}
	}
	refAt := func(c cell) string {
		p := TransformPoint(Point{X: float64(c.x), Y: float64(c.y)}, toRef)
		rc := cell{int(math.Round(p.X)), int(math.Round(p.Y))}
		if r, ok := refLabel[rc]; ok {
			return r
		}
		// Rotation and rounding leave 1-cell gaps along room edges.
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if r, ok := refLabel[cell{rc.x + dx, rc.y + dy}]; ok {
					return r
				}
			}
		}
		return ""
	}

	s := &roomSyncState{label: map[cell]string{}, ref: map[cell]string{}, names: map[string]string{},
		refNames: refNames, ps: pixelSizeOf(robot)}
	for _, l := range robot.Layers {
		if l.Type != "segment" {
			continue
		}
		id := l.MetaData.SegmentID
		s.names[id] = l.MetaData.Name
		for i := 0; i+1 < len(l.Pixels); i += 2 {
			c := cell{l.Pixels[i], l.Pixels[i+1]}
			s.label[c] = id
			s.ref[c] = refAt(c)
		}
	}
	return s
}

// segments returns the segment ids in numeric order.
func (s *roomSyncState) segments() []string {
	seen := map[string]bool{}
	for _, id := range s.label {
		seen[id] = true
	}
	ids := sortedKeys(seen)
	sort.SliceStable(ids, func(i, j int) bool { return segLess(ids[i], ids[j]) })
	return ids
}

func segLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// counts returns, for segment seg, its cell count per reference room.
func (s *roomSyncState) counts(seg string) (map[string]int, int) {
	counts, total := map[string]int{}, 0
	for c, id := range s.label {
		if id == seg {
			counts[s.ref[c]]++
			total++
		}
	}
	return counts, total
}

// majority is the reference room covering at least half of seg's cells
// that fall in any reference room, or "". A segment mostly outside the
// reference rooms (an area only this robot maps) matches nothing.
func (s *roomSyncState) majority(seg string) string {
	counts, total := s.counts(seg)
	if counts[""]*2 > total {
		return ""
	}
	best, bestN, inRooms := "", 0, 0
	for r, n := range counts {
		if r == "" {
			continue
		}
		inRooms += n
		if n > bestN || (n == bestN && segLess(r, best)) {
			best, bestN = r, n
		}
	}
	if inRooms == 0 || bestN*2 < inRooms {
		return ""
	}
	return best
}

// isLargestFor reports whether seg is the largest segment matching ref room r.
func (s *roomSyncState) isLargestFor(seg, r string) bool {
	sizes := map[string]int{}
	for _, id := range s.label {
		sizes[id]++
	}
	for _, other := range s.segments() {
		if other != seg && s.majority(other) == r && sizes[other] > sizes[seg] {
			return false
		}
	}
	return true
}

// match returns each segment's reference room and the cells that disagree.
func (s *roomSyncState) match() (map[string]string, []int) {
	assigned := map[string]string{}
	for _, seg := range s.segments() {
		assigned[seg] = s.majority(seg)
	}
	var mismatch []int
	for c, id := range s.label {
		if r := s.ref[c]; r != "" && r != assigned[id] {
			mismatch = append(mismatch, c.x, c.y)
		}
	}
	return assigned, sortPixels(mismatch)
}

func (s *roomSyncState) agreement() float64 {
	assigned := map[string]string{}
	for _, seg := range s.segments() {
		assigned[seg] = s.majority(seg)
	}
	match, total := 0, 0
	for c, id := range s.label {
		if s.ref[c] == "" {
			continue
		}
		total++
		if assigned[id] == s.ref[c] {
			match++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(match) / float64(total)
}

func (s *roomSyncState) nextID() string {
	maxID := 0
	for _, id := range s.segments() {
		if n, err := strconv.Atoi(id); err == nil && n > maxID {
			maxID = n
		}
	}
	return strconv.Itoa(maxID + 1)
}

// nextSplit finds the segment that most needs splitting (largest second
// reference room) and the straight cut between its two largest reference
// rooms. It applies the split to s.
func (s *roomSyncState) nextSplit() (RoomOp, bool) {
	type candidate struct {
		seg, a   string
		inB      func(cell) bool
		what     string
		minority int
	}
	var cands []candidate
	for _, seg := range s.segments() {
		counts, total := s.counts(seg)
		var rooms []string
		for r, n := range counts {
			if r != "" && n >= roomSyncMinCells && float64(n) >= roomSyncMinShare*float64(total) {
				rooms = append(rooms, r)
			}
		}
		sort.Slice(rooms, func(i, j int) bool {
			if counts[rooms[i]] != counts[rooms[j]] {
				return counts[rooms[i]] > counts[rooms[j]]
			}
			return segLess(rooms[i], rooms[j])
		})
		if len(rooms) >= 2 {
			b := rooms[1]
			cands = append(cands, candidate{seg, rooms[0], func(c cell) bool { return s.ref[c] == b },
				s.refRoom(b), counts[b]})
			continue
		}
		// An area of the segment outside every reference room, such as a
		// lidar artifact only this robot maps: cut it off into its own segment.
		if len(rooms) == 1 {
			if out := s.outsideArea(seg); s.m2(len(out)) >= roomSyncMinOutsideM2 {
				cands = append(cands, candidate{seg, rooms[0], func(c cell) bool { return out[c] },
					fmt.Sprintf("%.1f m² outside the reference rooms", s.m2(len(out))), len(out)})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].minority > cands[j].minority })

	for _, c := range cands {
		if op, ok := s.trySplit(c.seg, c.a, c.inB, c.what); ok {
			return op, true
		}
	}
	return RoomOp{}, false
}

// nameOrUnnamed quotes a segment name for plan reasons, or says "unnamed".
func nameOrUnnamed(name string) string {
	if name == "" {
		return "unnamed"
	}
	return strconv.Quote(name)
}

// refRoom describes reference segment r for plan reasons.
func (s *roomSyncState) refRoom(r string) string {
	if n := s.refNames[r]; n != "" {
		return fmt.Sprintf("reference room %s (%s)", n, r)
	}
	return "reference room " + r
}

// m2 converts a cell count to square meters (pixel size is in cm).
func (s *roomSyncState) m2(cells int) float64 {
	return float64(cells) * s.ps * s.ps / 10000
}

// outsideArea returns the largest connected set of seg's cells that lie in
// no reference room.
func (s *roomSyncState) outsideArea(seg string) map[cell]bool {
	seen := map[cell]bool{}
	var best map[cell]bool
	for c, id := range s.label {
		if id != seg || s.ref[c] != "" || seen[c] {
			continue
		}
		comp := map[cell]bool{}
		stack := []cell{c}
		seen[c] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp[p] = true
			for _, d := range []cell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				n := cell{p.x + d.x, p.y + d.y}
				if !seen[n] && s.label[n] == seg && s.ref[n] == "" {
					seen[n] = true
					stack = append(stack, n)
				}
			}
		}
		if len(comp) > len(best) {
			best = comp
		}
	}
	return best
}

// trySplit cuts seg along a line fitted to the boundary between reference
// room a and the cells inB (what describes them) inside it. It applies and
// returns the split when the line puts most inB cells on their own side.
func (s *roomSyncState) trySplit(seg, a string, inB func(cell) bool, what string) (RoomOp, bool) {
	// Boundary: cells of either side with the other side within
	// roomSyncBoundaryReach cells. The reach spans a wall, for areas that only
	// connect to the room through a doorway.
	inA := func(c cell) bool { return s.label[c] == seg && s.ref[c] == a && !inB(c) }
	isB := func(c cell) bool { return s.label[c] == seg && inB(c) }
	near := func(c cell, other func(cell) bool) bool {
		for dy := -roomSyncBoundaryReach; dy <= roomSyncBoundaryReach; dy++ {
			for dx := -roomSyncBoundaryReach; dx <= roomSyncBoundaryReach; dx++ {
				if other(cell{c.x + dx, c.y + dy}) {
					return true
				}
			}
		}
		return false
	}
	var boundary []Point
	var cells []cell
	for c, id := range s.label {
		if id != seg {
			continue
		}
		cells = append(cells, c)
		if (inA(c) && near(c, isB)) || (isB(c) && near(c, inA)) {
			boundary = append(boundary, Point{X: float64(c.x), Y: float64(c.y)})
		}
	}
	if len(boundary) < 5 {
		return RoomOp{}, false
	}
	center, dir := fitLine(boundary)
	side := func(c cell) bool {
		return (float64(c.x)-center.X)*dir.Y-(float64(c.y)-center.Y)*dir.X > 0
	}

	// Which side holds b? Most of b must land there, and that side must
	// hold more b than a, or the cut takes a chunk of a with it.
	aOn, bOn := map[bool]int{}, map[bool]int{}
	for _, c := range cells {
		switch {
		case inB(c):
			bOn[side(c)]++
		case s.ref[c] == a:
			aOn[side(c)]++
		}
	}
	bSide := bOn[true] > bOn[false]
	if float64(bOn[bSide]) < roomSyncMinSplitGain*float64(bOn[true]+bOn[false]) ||
		aOn[bSide] >= bOn[bSide] || aOn[!bSide] <= bOn[!bSide] {
		return RoomOp{}, false
	}

	// Endpoints: the cut line across the whole segment, plus a margin.
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, c := range cells {
		t := (float64(c.x)-center.X)*dir.X + (float64(c.y)-center.Y)*dir.Y
		lo, hi = math.Min(lo, t), math.Max(hi, t)
	}
	lo, hi = lo-2, hi+2
	// Whole cm: the map's own unit. The simulation cuts along side, not these.
	pa := Point{X: math.Round((center.X + lo*dir.X) * s.ps), Y: math.Round((center.Y + lo*dir.Y) * s.ps)}
	pb := Point{X: math.Round((center.X + hi*dir.X) * s.ps), Y: math.Round((center.Y + hi*dir.Y) * s.ps)}

	newID := s.nextID()
	for _, c := range cells {
		if side(c) == bSide {
			s.label[c] = newID
		}
	}
	s.names[newID] = ""
	return RoomOp{
		Action: "split_segment", SegmentID: seg, PA: &pa, PB: &pb,
		Reason: fmt.Sprintf("segment %s (%s) is %s plus %s; the cut makes new segment %s",
			seg, nameOrUnnamed(s.names[seg]), s.refRoom(a), what, newID),
	}, true
}

// fitLine returns the centroid and unit direction of the best-fit line
// through pts (principal axis).
func fitLine(pts []Point) (Point, Point) {
	var cx, cy float64
	for _, p := range pts {
		cx += p.X
		cy += p.Y
	}
	n := float64(len(pts))
	cx, cy = cx/n, cy/n
	var sxx, syy, sxy float64
	for _, p := range pts {
		dx, dy := p.X-cx, p.Y-cy
		sxx += dx * dx
		syy += dy * dy
		sxy += dx * dy
	}
	theta := 0.5 * math.Atan2(2*sxy, sxx-syy)
	return Point{X: cx, Y: cy}, Point{X: math.Cos(theta), Y: math.Sin(theta)}
}

// nextJoin joins two adjacent segments that match the same reference room,
// keeping the larger one's id. It applies the join to s.
func (s *roomSyncState) nextJoin() (RoomOp, bool) {
	byRoom := map[string][]string{}
	sizes := map[string]int{}
	for _, id := range s.label {
		sizes[id]++
	}
	for _, seg := range s.segments() {
		if r := s.majority(seg); r != "" {
			byRoom[r] = append(byRoom[r], seg)
		}
	}
	for _, r := range sortedKeys(byRoom) {
		segs := byRoom[r]
		if len(segs) < 2 {
			continue
		}
		sort.SliceStable(segs, func(i, j int) bool { return sizes[segs[i]] > sizes[segs[j]] })
		keep := segs[0]
		for _, other := range segs[1:] {
			if !s.adjacent(keep, other) {
				continue
			}
			for c, id := range s.label {
				if id == other {
					s.label[c] = keep
				}
			}
			return RoomOp{
				Action: "join_segments", SegmentAID: keep, SegmentBID: other,
				Reason: fmt.Sprintf("segments %s (%s) and %s (%s) are both %s",
					keep, nameOrUnnamed(s.names[keep]), other, nameOrUnnamed(s.names[other]), s.refRoom(r)),
			}, true
		}
	}
	return RoomOp{}, false
}

func (s *roomSyncState) adjacent(a, b string) bool {
	for c, id := range s.label {
		if id != a {
			continue
		}
		for dy := -roomSyncAdjacency; dy <= roomSyncAdjacency; dy++ {
			for dx := -roomSyncAdjacency; dx <= roomSyncAdjacency; dx++ {
				if s.label[cell{c.x + dx, c.y + dy}] == b {
					return true
				}
			}
		}
	}
	return false
}

// toMap returns a copy of robot with its segment layers replaced by s.
func (s *roomSyncState) toMap(robot *ValetudoMap) *ValetudoMap {
	out := *robot
	out.Layers = nil
	for _, l := range robot.Layers {
		if l.Type != "segment" {
			out.Layers = append(out.Layers, l)
		}
	}
	pixels := map[string][]int{}
	for c, id := range s.label {
		pixels[id] = append(pixels[id], c.x, c.y)
	}
	for _, id := range s.segments() {
		out.Layers = append(out.Layers, MapLayer{
			Type:     "segment",
			MetaData: LayerMetaData{SegmentID: id, Name: s.names[id]},
			Pixels:   sortPixels(pixels[id]),
		})
	}
	return &out
}

// sortPixels orders [x,y,...] pairs by row then column, so output is stable.
func sortPixels(px []int) []int {
	pts := make([][2]int, 0, len(px)/2)
	for i := 0; i+1 < len(px); i += 2 {
		pts = append(pts, [2]int{px[i], px[i+1]})
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i][1] != pts[j][1] {
			return pts[i][1] < pts[j][1]
		}
		return pts[i][0] < pts[j][0]
	})
	out := make([]int, 0, len(px))
	for _, p := range pts {
		out = append(out, p[0], p[1])
	}
	return out
}
