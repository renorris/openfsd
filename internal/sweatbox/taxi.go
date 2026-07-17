package sweatbox

import (
	"fmt"
	"strings"

	"github.com/renorris/openfsd/internal/geo"
)

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
	HoldAt []TaxiHold
}

// TaxiHold is one hold-short location on a planned taxi path.
type TaxiHold struct {
	// Name is the hold-short target (runway end, taxiway, or HOLD name).
	Name string
	// Point is where the aircraft should stop.
	Point Point
	// WaypointIndex is the index into TaxiPlan.Waypoints of Point (−1 if the
	// hold could not be placed on the expanded path but validation passed).
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

	// Normalize / validate holds.
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
			// Prefer the token if it matches an end; else combined name.
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
		first := firstRouteSurface(normSteps, parking, g)
		if first == "" {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
		if !g.Intersect(curName, first) && !sameSurface(g, curName, first) {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
	}

	// Consecutive surface steps must intersect.
	routeSurfaces := make([]string, 0, len(normSteps)+1)
	if curName != "" && (len(normSteps) == 0 || !sameSurface(g, curName, normSteps[0])) {
		// Include current only for intersection chaining into first step;
		// not as a plan step.
	}
	routeSurfaces = append(routeSurfaces, normSteps...)
	for i := 1; i < len(routeSurfaces); i++ {
		a, b := routeSurfaces[i-1], routeSurfaces[i]
		if !g.Intersect(a, b) {
			return zero, fmt.Sprintf("%s and %s do not intersect.", displayName(a), displayName(b))
		}
	}

	// When current is set and differs from first step, current must intersect first
	// (already checked). Parking-only from an intersecting surface is ok.
	if len(normSteps) >= 1 && curName != "" && !sameSurface(g, curName, normSteps[0]) {
		if !g.Intersect(curName, normSteps[0]) {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
	}

	// Parking exit: last surface (or current) must be able to leave toward parking.
	// We do not require a 100 ft snap between last surface and parking; TWRTrainer
	// taxis to the closest waypoint on the last surface then direct to parking.
	if parking != "" && len(normSteps) == 0 {
		// Taxi direct from current surface to parking — current required.
		if curName == "" {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
		if g.Surface(curName) == nil {
			return zero, "First step of taxi route must intersect with current taxiway/runway."
		}
	}

	plan := TaxiPlan{
		Steps:   append([]string(nil), normSteps...),
		Holds:   append([]string(nil), normHolds...),
		Parking: parking,
	}

	wps, holdAt := g.buildTaxiPath(curName, normSteps, parking, normHolds)
	plan.Waypoints = wps
	plan.HoldAt = holdAt
	return plan, ""
}

// firstRouteSurface is the first taxiway/runway on the route, or parking name.
func firstRouteSurface(normSteps []string, parking string, g *Graph) string {
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

// buildTaxiPath expands surface polylines into a waypoint list and places holds.
func (g *Graph) buildTaxiPath(current string, steps []string, parking string, holds []string) ([]Point, []TaxiHold) {
	var wps []Point
	if len(steps) == 0 && parking == "" {
		return nil, nil
	}

	// Chain of surfaces we walk (excluding parking).
	// When current differs from first step, entry is their intersection.
	type leg struct {
		surface string
		from    Point
		fromSet bool
		to      Point
		toSet   bool
	}

	var legs []leg

	// Determine starting surface and entry point.
	startSurface := ""
	var entry Point
	entrySet := false
	if len(steps) > 0 {
		startSurface = steps[0]
		if current != "" && !sameSurface(g, current, startSurface) {
			if p, _, _, ok := g.FindIntersection(current, startSurface); ok {
				entry = p
				entrySet = true
			}
		}
	} else {
		// Parking-only: walk on current toward parking.
		startSurface = current
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
				// Prefer the point on this surface (B).
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
			// Destination is this surface (typically a runway): to = entry onto it
			// from previous, already as from; keep to unset so we still include from.
			// For a multi-step route ending on a runway, the end point is the
			// intersection of the previous surface and this runway — already "from".
			// Single-step runway from another current: "from" is entry; path is [entry].
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

	// Parking-only leg: from closest on current to parking point.
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

	// Expand legs into waypoints.
	for _, lg := range legs {
		s := g.Surface(lg.surface)
		if s == nil || len(s.Points) == 0 {
			continue
		}
		switch {
		case lg.fromSet && lg.toSet:
			seg := walkPolyline(s, lg.from, lg.to, g.tolM)
			wps = appendUniquePoints(wps, seg, g.tolM)
		case lg.fromSet:
			wps = appendUniquePoints(wps, []Point{lg.from}, g.tolM)
		case lg.toSet:
			wps = appendUniquePoints(wps, []Point{lg.to}, g.tolM)
		}
	}

	// Append parking coordinate (always, even if within snap of last taxiway point).
	if parking != "" {
		if ps := g.Surface(parking); ps != nil && len(ps.Points) > 0 {
			p := ps.Points[0]
			if len(wps) == 0 || wps[len(wps)-1] != p {
				wps = append(wps, p)
			}
		}
	}

	// Resolve hold-short positions against the route surfaces.
	holdAt := g.resolveHolds(steps, current, holds, wps)
	return wps, holdAt
}

// resolveHolds places each hold at the intersection of the hold target with a
// route surface (first match in route order). Destination runway is not auto-
// added here — the engine holds short of the final runway by status.
func (g *Graph) resolveHolds(steps []string, current string, holds []string, wps []Point) []TaxiHold {
	if len(holds) == 0 {
		return nil
	}
	// Surfaces along the taxi (current + steps) for intersection search.
	chain := make([]string, 0, len(steps)+1)
	if current != "" {
		chain = append(chain, current)
	}
	chain = append(chain, steps...)

	out := make([]TaxiHold, 0, len(holds))
	for _, h := range holds {
		hs := g.Surface(h)
		var hp Point
		found := false
		if hs != nil && hs.Kind == SurfaceHold && len(hs.Points) == 1 {
			hp = hs.Points[0]
			found = true
		} else {
			// Intersection of hold target with any route surface.
			for _, sName := range chain {
				if sameSurface(g, sName, h) {
					// Holding short of a surface we're taxiing on: use first
					// intersection of that surface with a neighboring chain member.
					continue
				}
				if p, _, _, ok := g.FindIntersection(sName, h); ok {
					hp = p
					found = true
					break
				}
			}
			// If hold is the destination runway itself, use entry onto it.
			if !found && len(steps) > 0 && sameSurface(g, steps[len(steps)-1], h) {
				if len(steps) >= 2 {
					if p, _, _, ok := g.FindIntersection(steps[len(steps)-2], steps[len(steps)-1]); ok {
						hp = p
						found = true
					}
				} else if current != "" {
					if p, _, _, ok := g.FindIntersection(current, steps[0]); ok {
						hp = p
						found = true
					}
				}
			}
		}
		th := TaxiHold{Name: h, WaypointIndex: -1}
		if found {
			th.Point = hp
			th.WaypointIndex = findWaypointIndex(wps, hp, g.tolM)
		}
		out = append(out, th)
	}
	return out
}

// walkPolyline returns points along s from near "from" to near "to" (inclusive),
// walking the shorter direction along the polyline when ambiguous.
func walkPolyline(s *Surface, from, to Point, tolM float64) []Point {
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
	// Forward path length vs reverse.
	if iFrom < iTo {
		return append([]Point(nil), s.Points[iFrom:iTo+1]...)
	}
	// Reverse.
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

func appendUniquePoints(dst []Point, src []Point, tolM float64) []Point {
	tolSq := tolM * tolM
	for _, p := range src {
		if len(dst) > 0 {
			last := dst[len(dst)-1]
			if geo.DistanceSq(last.Lat, last.Lon, p.Lat, p.Lon) <= tolSq {
				continue
			}
		}
		dst = append(dst, p)
	}
	return dst
}

func findWaypointIndex(wps []Point, p Point, tolM float64) int {
	tolSq := tolM * tolM
	for i, q := range wps {
		if geo.DistanceSq(q.Lat, q.Lon, p.Lat, p.Lon) <= tolSq {
			return i
		}
	}
	return -1
}
