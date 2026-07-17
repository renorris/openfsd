package sweatbox

import (
	"fmt"
	"strings"

	"github.com/renorris/openfsd/internal/geo"
)

// pathStitchEpsM is the only distance used when collapsing adjacent path samples
// at leg joints. Intersection snap (~100 ft) must not drop real polyline vertices.
const pathStitchEpsM = 1.0

// TaxiPlan is a validated taxi route: ordered surface steps, hold-shorts, an
// optional final parking, and an expanded waypoint polyline for the engine.
//
// Waypoints start at the entry onto the first route surface (intersection with
// current, or the first turn point when already on that surface) and end at
// the destination runway intersection or parking coordinates.
type TaxiPlan struct {
	// Steps are upper-case taxiway/runway names (parking stripped to Parking).
	Steps []string
	// Holds are upper-case hold-short targets from the command (not auto dest).
	Holds []string
	// Parking is the destination parking name without "@", or empty.
	Parking string
	// Waypoints is the ordered ground path (lat/lon) along surface polylines.
	Waypoints []Point
	// HoldAt lists hold-short positions resolved along the route.
	// Every entry has WaypointIndex >= 0 (off-route holds are rejected).
	HoldAt []TaxiHold
}

// TaxiHold is one hold-short location on a planned taxi path.
type TaxiHold struct {
	// Name is the hold-short target (runway end, taxiway, or HOLD name).
	Name string
	// Point is where the aircraft should stop.
	Point Point
	// WaypointIndex is the index into TaxiPlan.Waypoints of Point.
	WaypointIndex int
}

// PlanTaxi validates and plans a taxi route on g.
//
// current is the aircraft's current surface (taxiway/runway/parking name), or
// empty if off-surface. steps are route tokens (surface names; final may be
// "@parking"). holds are hold-short tokens from the taxi "hs …" list.
//
// On success errMsg is empty. On failure errMsg is a TWRTrainer-themed string
// (not a Go error) suitable for instructor feedback.
func (g *Graph) PlanTaxi(current string, steps, holds []string) (TaxiPlan, string) {
	var zero TaxiPlan
	if g == nil || g.apt == nil {
		return zero, "Runway/taxiway not found in airport file."
	}

	if len(steps) == 0 {
		return zero, "Must specify at least one taxiway, runway or parking space to taxi to."
	}
	if len(steps) > MaxTaxiSteps {
		return zero, "Too many taxi steps. Maximum of 100."
	}
	if len(holds) > MaxHoldPoints {
		return zero, "Too many hold points. Maximum of 10 including the destination runway."
	}

	// Normalize steps; detect parking (@name) — only last may be parking.
	normSteps := make([]string, 0, len(steps))
	var parking string
	for i, raw := range steps {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return zero, "Unknown step in taxi route: " + raw
		}
		if strings.HasPrefix(tok, "@") {
			name := strings.ToUpper(strings.TrimSpace(tok[1:]))
			if name == "" || !isWordName(name) {
				return zero, "Unknown step in taxi route: " + raw
			}
			if i != len(steps)-1 {
				return zero, "Only the last step can be a parking space."
			}
			if g.Surface(name) == nil || g.Surface(name).Kind != SurfaceParking {
				return zero, "Unknown parking space."
			}
			parking = name
			continue
		}
		name := strings.ToUpper(tok)
		s := g.Surface(name)
		if s == nil {
			return zero, "Unknown step in taxi route: " + name
		}
		if s.Kind == SurfaceParking {
			// Bare parking name without @ is still only valid as last step.
			if i != len(steps)-1 {
				return zero, "Only the last step can be a parking space."
			}
			parking = strings.ToUpper(s.Name)
			continue
		}
		// HOLD surfaces are not taxi route steps (they are hold-short targets).
		if s.Kind == SurfaceHold {
			return zero, "Unknown step in taxi route: " + name
		}
		// Canonical runway name for path work.
		if s.Kind == SurfaceRunway {
			name = s.Name
		} else {
			name = strings.ToUpper(s.Name)
		}
		normSteps = append(normSteps, name)
	}

	if len(normSteps) == 0 && parking == "" {
		return zero, "Must specify at least one taxiway, runway or parking space to taxi to."
	}

	// Consecutive duplicates among surface steps (before parking).
	for i := 1; i < len(normSteps); i++ {
		if sameSurface(g, normSteps[i-1], normSteps[i]) {
			return zero, fmt.Sprintf("%s occurs twice in a row in the taxi route.", normSteps[i])
		}
	}

	// Normalize / validate holds (existence + kind only; on-route check after path).
	normHolds := make([]string, 0, len(holds))
	for _, raw := range holds {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return zero, "Runway/taxiway not found in airport file."
		}
		name := strings.ToUpper(tok)
		s := g.Surface(name)
		if s == nil {
			return zero, "Runway/taxiway not found in airport file."
		}
		if s.Kind == SurfaceParking {
			return zero, "Can't hold short of a parking space."
		}
		if s.Kind == SurfaceRunway {
			// Keep the designator the instructor typed when it is an end name.
			up := strings.ToUpper(tok)
			if up == s.RwyA || up == s.RwyB {
				name = up
			} else {
				name = s.Name
			}
		} else {
			name = strings.ToUpper(s.Name)
		}
		normHolds = append(normHolds, name)
	}

	current = strings.TrimSpace(current)
	curName := strings.ToUpper(current)

	// Already there: single surface step equal to current (no parking move).
	if parking == "" && len(normSteps) == 1 && curName != "" && sameSurface(g, curName, normSteps[0]) {
		return zero, "Already there!"
	}
	// Already at parking destination with no taxi surfaces.
	if parking != "" && len(normSteps) == 0 && curName != "" {
		if s := g.Surface(curName); s != nil && s.Kind == SurfaceParking && strings.EqualFold(s.Name, parking) {
			return zero, "Already there!"
		}
	}

	// First step must intersect current taxiway/runway (when current is set).
	if curName != "" {
		first := firstRouteSurface(normSteps, parking)
		if first == "" || (!g.Intersect(curName, first) && !sameSurface(g, curName, first)) {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
	}

	// Consecutive surface steps must intersect.
	for i := 1; i < len(normSteps); i++ {
		a, b := normSteps[i-1], normSteps[i]
		if !g.Intersect(a, b) {
			return zero, fmt.Sprintf("%s and %s do not intersect.", displayName(a), displayName(b))
		}
	}

	// Parking-only: need a current surface to leave from.
	if parking != "" && len(normSteps) == 0 {
		if curName == "" || g.Surface(curName) == nil {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
	}

	wps := g.buildTaxiPath(curName, normSteps, parking)
	wps, holdAt, errMsg := g.resolveHolds(normSteps, curName, normHolds, wps)
	if errMsg != "" {
		return zero, errMsg
	}

	return TaxiPlan{
		Steps:     append([]string(nil), normSteps...),
		Holds:     append([]string(nil), normHolds...),
		Parking:   parking,
		Waypoints: wps,
		HoldAt:    holdAt,
	}, ""
}

// firstRouteSurface is the first taxiway/runway on the route, or parking name.
func firstRouteSurface(normSteps []string, parking string) string {
	if len(normSteps) > 0 {
		return normSteps[0]
	}
	return parking
}

func sameSurface(g *Graph, a, b string) bool {
	ia := g.surfaceIndex(a)
	ib := g.surfaceIndex(b)
	if ia < 0 || ib < 0 {
		return strings.EqualFold(a, b)
	}
	return ia == ib
}

func displayName(name string) string {
	return strings.ToUpper(name)
}

// buildTaxiPath expands surface polylines into a waypoint list.
func (g *Graph) buildTaxiPath(current string, steps []string, parking string) []Point {
	var wps []Point
	if len(steps) == 0 && parking == "" {
		return nil
	}

	type leg struct {
		surface string
		from    Point
		fromSet bool
		to      Point
		toSet   bool
	}

	var legs []leg

	// Entry onto first step from a different current surface (point on first step).
	var entry Point
	entrySet := false
	if len(steps) > 0 {
		if current != "" && !sameSurface(g, current, steps[0]) {
			if p, _, idxB, ok := g.FindIntersection(current, steps[0]); ok {
				sb := g.Surface(steps[0])
				if sb != nil && idxB >= 0 && idxB < len(sb.Points) {
					entry = sb.Points[idxB]
				} else {
					entry = p
				}
				entrySet = true
			}
		}
	}

	// Build legs between consecutive steps.
	for i := 0; i < len(steps); i++ {
		sName := steps[i]
		lg := leg{surface: sName}
		if i == 0 {
			if entrySet {
				lg.from = entry
				lg.fromSet = true
			}
		} else {
			// From previous intersection (point on this surface).
			if p, _, idxB, ok := g.FindIntersection(steps[i-1], sName); ok {
				sb := g.Surface(sName)
				if sb != nil && idxB >= 0 && idxB < len(sb.Points) {
					lg.from = sb.Points[idxB]
				} else {
					lg.from = p
				}
				lg.fromSet = true
			}
		}
		if i+1 < len(steps) {
			if p, idxA, _, ok := g.FindIntersection(sName, steps[i+1]); ok {
				sa := g.Surface(sName)
				if sa != nil && idxA >= 0 && idxA < len(sa.Points) {
					lg.to = sa.Points[idxA]
				} else {
					lg.to = p
				}
				lg.toSet = true
			}
		} else if parking != "" {
			// Last surface before parking: to = closest waypoint to parking.
			ps := g.Surface(parking)
			if ps != nil && len(ps.Points) > 0 {
				if pt, _, _, ok := g.ClosestWaypoint(sName, ps.Points[0].Lat, ps.Points[0].Lon); ok {
					lg.to = pt
					lg.toSet = true
				}
			}
		} else {
			// Destination is this surface (typically a runway). Path ends at entry
			// from the previous surface (or from current when single-step).
			if !lg.fromSet && current != "" && !sameSurface(g, current, sName) {
				if p, _, idxB, ok := g.FindIntersection(current, sName); ok {
					sb := g.Surface(sName)
					if sb != nil && idxB >= 0 && idxB < len(sb.Points) {
						lg.from = sb.Points[idxB]
					} else {
						lg.from = p
					}
					lg.fromSet = true
				}
			}
		}
		legs = append(legs, lg)
	}

	// Parking-only leg: leave current at closest waypoint toward parking.
	if len(steps) == 0 && parking != "" && current != "" {
		ps := g.Surface(parking)
		lg := leg{surface: current}
		if ps != nil && len(ps.Points) > 0 {
			if pt, _, _, ok := g.ClosestWaypoint(current, ps.Points[0].Lat, ps.Points[0].Lon); ok {
				lg.from = pt
				lg.fromSet = true
				lg.to = pt
				lg.toSet = true
			}
		}
		legs = append(legs, lg)
	}

	// Expand legs into waypoints (stitch with tiny epsilon only).
	for _, lg := range legs {
		s := g.Surface(lg.surface)
		if s == nil || len(s.Points) == 0 {
			continue
		}
		switch {
		case lg.fromSet && lg.toSet:
			seg := walkPolyline(s, lg.from, lg.to)
			wps = appendUniquePoints(wps, seg, pathStitchEpsM)
		case lg.fromSet:
			wps = appendUniquePoints(wps, []Point{lg.from}, pathStitchEpsM)
		case lg.toSet:
			wps = appendUniquePoints(wps, []Point{lg.to}, pathStitchEpsM)
		}
	}

	// Append parking coordinate (always, even if near last taxiway point).
	if parking != "" {
		if ps := g.Surface(parking); ps != nil && len(ps.Points) > 0 {
			p := ps.Points[0]
			if len(wps) == 0 || wps[len(wps)-1] != p {
				wps = append(wps, p)
			}
		}
	}

	return wps
}

// resolveHolds places each hold on the route path. Holds must map onto Waypoints
// (within intersection snap). Off-route holds are rejected with an instructor string.
// May insert HOLD vertices into wps when they lie on a route surface but were
// outside the expanded turn-to-turn segment (e.g. already on first surface).
//
// Placement prefers the latest path position that intersects the hold target:
// for each route step (reverse order), the step∩hold point is a candidate; when
// the hold is the step itself (e.g. destination runway), the entry from the
// previous surface (or current) is used. HOLD surfaces use their single point.
func (g *Graph) resolveHolds(steps []string, current string, holds []string, wps []Point) ([]Point, []TaxiHold, string) {
	if len(holds) == 0 {
		return wps, nil, ""
	}

	out := make([]TaxiHold, 0, len(holds))
	for _, h := range holds {
		hs := g.Surface(h)
		var bestPoint Point
		bestIdx := -1

		if hs != nil && hs.Kind == SurfaceHold && len(hs.Points) == 1 {
			hp := hs.Points[0]
			// HOLD is on-route if it snaps to any route-step (or current) surface vertex.
			if !g.holdNearRouteSurface(hp, steps, current) {
				return wps, nil, fmt.Sprintf("%s is not on the taxi route.", displayName(h))
			}
			idx := findWaypointIndex(wps, hp, g.tolM)
			if idx < 0 {
				// Inject so the engine sees the hold on the path (common when
				// already on the first surface: expansion starts at the next turn).
				wps, idx = insertWaypoint(wps, hp, pathStitchEpsM)
			}
			out = append(out, TaxiHold{Name: h, Point: wps[idx], WaypointIndex: idx})
			continue
		}

		// Collect candidate intersections with route steps (reverse: prefer last crossing).
		for i := len(steps) - 1; i >= 0; i-- {
			sName := steps[i]
			var cand Point
			var ok bool
			if sameSurface(g, sName, h) {
				// Holding short of a surface that is itself a step (e.g. dest runway):
				// stop at the entry onto that surface.
				if i > 0 {
					cand, ok = intersectionOnSurface(g, steps[i-1], sName)
				} else if current != "" && !sameSurface(g, current, sName) {
					cand, ok = intersectionOnSurface(g, current, sName)
				} else {
					// Already on the hold surface as first step — use first path point
					// that lies on this surface.
					cand, ok = firstPathPointOnSurface(g, wps, h)
				}
			} else {
				cand, ok = intersectionOnSurface(g, sName, h)
			}
			if !ok {
				continue
			}
			idx := findWaypointIndex(wps, cand, g.tolM)
			if idx < 0 {
				continue
			}
			// Keep the candidate furthest along the path.
			if idx > bestIdx {
				bestIdx = idx
				bestPoint = wps[idx]
			}
		}

		// Fallback: hold ∩ current only when no step candidate mapped onto the path
		// (avoids Issue 1: A∩19 beating B∩19 when A is current but route leaves via B).
		if bestIdx < 0 && current != "" && !sameSurface(g, current, h) {
			if cand, ok := intersectionOnSurface(g, current, h); ok {
				if idx := findWaypointIndex(wps, cand, g.tolM); idx >= 0 {
					bestIdx = idx
					bestPoint = wps[idx]
				}
			}
		}

		if bestIdx < 0 {
			return wps, nil, fmt.Sprintf("%s is not on the taxi route.", displayName(h))
		}
		out = append(out, TaxiHold{Name: h, Point: bestPoint, WaypointIndex: bestIdx})
	}
	// Re-snap indices after possible HOLD insertions shifted the path.
	for i := range out {
		idx := findWaypointIndex(wps, out[i].Point, g.tolM)
		if idx < 0 {
			return wps, nil, fmt.Sprintf("%s is not on the taxi route.", displayName(out[i].Name))
		}
		out[i].WaypointIndex = idx
		out[i].Point = wps[idx]
	}
	return wps, out, ""
}

// holdNearRouteSurface reports whether p is within intersection snap of any
// vertex on a route step (or current) surface.
func (g *Graph) holdNearRouteSurface(p Point, steps []string, current string) bool {
	if current != "" && g.PointIndexWithin(current, p) >= 0 {
		return true
	}
	for _, sName := range steps {
		if g.PointIndexWithin(sName, p) >= 0 {
			return true
		}
	}
	// Also allow closest-within-tol when the hold is not exactly a stored vertex.
	tolSq := g.tolM * g.tolM
	check := func(name string) bool {
		s := g.Surface(name)
		if s == nil {
			return false
		}
		for _, q := range s.Points {
			if geo.DistanceSq(p.Lat, p.Lon, q.Lat, q.Lon) <= tolSq {
				return true
			}
		}
		return false
	}
	if current != "" && check(current) {
		return true
	}
	for _, sName := range steps {
		if check(sName) {
			return true
		}
	}
	return false
}

// insertWaypoint inserts p into wps if not already present within epsM.
// Insertion is before the closest existing waypoint (or append if empty).
// Returns the updated slice and the index of p.
func insertWaypoint(wps []Point, p Point, epsM float64) ([]Point, int) {
	if idx := findWaypointIndex(wps, p, epsM); idx >= 0 {
		return wps, idx
	}
	if len(wps) == 0 {
		return []Point{p}, 0
	}
	// Insert before the closest path vertex so holds ahead of the first turn
	// appear earlier on the path.
	best := 0
	bestD := geo.DistanceSq(p.Lat, p.Lon, wps[0].Lat, wps[0].Lon)
	for i := 1; i < len(wps); i++ {
		d := geo.DistanceSq(p.Lat, p.Lon, wps[i].Lat, wps[i].Lon)
		if d < bestD {
			bestD = d
			best = i
		}
	}
	out := make([]Point, 0, len(wps)+1)
	out = append(out, wps[:best]...)
	out = append(out, p)
	out = append(out, wps[best:]...)
	return out, best
}

// intersectionOnSurface returns the intersection point preferred on surface B
// (the second name), falling back to the point on A.
func intersectionOnSurface(g *Graph, nameA, nameB string) (Point, bool) {
	p, idxA, idxB, ok := g.FindIntersection(nameA, nameB)
	if !ok {
		return Point{}, false
	}
	if sb := g.Surface(nameB); sb != nil && idxB >= 0 && idxB < len(sb.Points) {
		return sb.Points[idxB], true
	}
	if sa := g.Surface(nameA); sa != nil && idxA >= 0 && idxA < len(sa.Points) {
		return sa.Points[idxA], true
	}
	return p, true
}

// firstPathPointOnSurface finds the first waypoint within snap of any point on surface h.
func firstPathPointOnSurface(g *Graph, wps []Point, h string) (Point, bool) {
	s := g.Surface(h)
	if s == nil || len(s.Points) == 0 || len(wps) == 0 {
		return Point{}, false
	}
	tolSq := g.tolM * g.tolM
	for _, wp := range wps {
		for _, sp := range s.Points {
			if geo.DistanceSq(wp.Lat, wp.Lon, sp.Lat, sp.Lon) <= tolSq {
				return wp, true
			}
		}
	}
	return Point{}, false
}

// walkPolyline returns points along s from the vertex nearest "from" to the
// vertex nearest "to" (inclusive), walking index-forward or index-reverse along
// the open polyline (no closed-loop shorter-arc logic).
func walkPolyline(s *Surface, from, to Point) []Point {
	if s == nil || len(s.Points) == 0 {
		return nil
	}
	iFrom := closestIndex(s.Points, from)
	iTo := closestIndex(s.Points, to)
	if iFrom < 0 || iTo < 0 {
		return []Point{from, to}
	}
	if iFrom == iTo {
		return []Point{s.Points[iFrom]}
	}
	if iFrom < iTo {
		return append([]Point(nil), s.Points[iFrom:iTo+1]...)
	}
	out := make([]Point, 0, iFrom-iTo+1)
	for i := iFrom; i >= iTo; i-- {
		out = append(out, s.Points[i])
	}
	return out
}

func closestIndex(pts []Point, p Point) int {
	if len(pts) == 0 {
		return -1
	}
	best := 0
	bestD := geo.DistanceSq(p.Lat, p.Lon, pts[0].Lat, pts[0].Lon)
	for i := 1; i < len(pts); i++ {
		d := geo.DistanceSq(p.Lat, p.Lon, pts[i].Lat, pts[i].Lon)
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}

// appendUniquePoints appends src onto dst, skipping a point only when it is
// within epsM of the current last point (stitch joint dedup — not intersection snap).
func appendUniquePoints(dst []Point, src []Point, epsM float64) []Point {
	epsSq := epsM * epsM
	for _, p := range src {
		if len(dst) > 0 {
			last := dst[len(dst)-1]
			if geo.DistanceSq(last.Lat, last.Lon, p.Lat, p.Lon) <= epsSq {
				continue
			}
		}
		dst = append(dst, p)
	}
	return dst
}

func findWaypointIndex(wps []Point, p Point, tolM float64) int {
	tolSq := tolM * tolM
	// Prefer closest within tol so a hold snaps to the best path vertex.
	best := -1
	bestD := tolSq + 1
	for i, q := range wps {
		d := geo.DistanceSq(q.Lat, q.Lon, p.Lat, p.Lon)
		if d <= tolSq && d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}
