package sweatbox

import (
	"strings"

	"github.com/renorris/openfsd/internal/geo"
)

// Intersection / taxi limits matching TWRTrainer constants.
const (
	// DefaultIntersectionTolM is ~100 feet (TWRTrainer INTERSECT_TOL_FT).
	DefaultIntersectionTolM = 30.48

	// MaxTaxiSteps is the maximum number of taxi route steps (surfaces + parking).
	MaxTaxiSteps = 100

	// MaxHoldPoints is the maximum number of hold-short entries including the
	// destination runway (TWRTrainer MAX_HOLD_POINTS).
	MaxHoldPoints = 10
)

// Graph is an airport surface intersection graph built from a parsed Airport.
// It supports 100 ft snap queries and taxi route planning (see PlanTaxi).
//
// The graph is immutable after construction; the underlying Airport must not
// be mutated.
type Graph struct {
	apt  *Airport
	tolM float64

	// byName maps upper-case surface name → index into apt.Surfaces.
	// Runways are indexed under "A/B", RwyA, and RwyB.
	byName map[string]int

	// adj[i] is the set of surface indices that intersect surface i within tol.
	// Self is never included.
	adj []map[int]struct{}
}

// NewGraph builds an intersection graph from apt using DefaultIntersectionTolM.
// apt must be non-nil; surfaces with no points are ignored for intersections.
func NewGraph(apt *Airport) *Graph {
	return NewGraphTol(apt, DefaultIntersectionTolM)
}

// NewGraphTol is like NewGraph but with a custom intersection tolerance in meters.
// tolM ≤ 0 falls back to DefaultIntersectionTolM.
func NewGraphTol(apt *Airport, tolM float64) *Graph {
	if apt == nil {
		apt = &Airport{}
	}
	if tolM <= 0 {
		tolM = DefaultIntersectionTolM
	}
	g := &Graph{
		apt:    apt,
		tolM:   tolM,
		byName: make(map[string]int, len(apt.Surfaces)*2),
		adj:    make([]map[int]struct{}, len(apt.Surfaces)),
	}
	for i := range apt.Surfaces {
		g.adj[i] = make(map[int]struct{})
		s := &apt.Surfaces[i]
		key := strings.ToUpper(s.Name)
		if _, exists := g.byName[key]; !exists {
			g.byName[key] = i
		}
		if s.Kind == SurfaceRunway {
			if s.RwyA != "" {
				g.byName[strings.ToUpper(s.RwyA)] = i
			}
			if s.RwyB != "" {
				g.byName[strings.ToUpper(s.RwyB)] = i
			}
		}
	}
	g.buildAdjacency()
	return g
}

// Airport returns the airport the graph was built from.
func (g *Graph) Airport() *Airport {
	if g == nil {
		return nil
	}
	return g.apt
}

// TolM returns the intersection snap tolerance in meters.
func (g *Graph) TolM() float64 {
	if g == nil {
		return DefaultIntersectionTolM
	}
	return g.tolM
}

// Surface looks up a surface by name (case-insensitive). For runways, matches
// either end designator or the combined "A/B" name. Returns nil if missing.
func (g *Graph) Surface(name string) *Surface {
	if g == nil || g.apt == nil {
		return nil
	}
	idx, ok := g.byName[strings.ToUpper(name)]
	if !ok {
		return nil
	}
	return &g.apt.Surfaces[idx]
}

// surfaceIndex returns the surface index for name, or -1.
func (g *Graph) surfaceIndex(name string) int {
	if g == nil {
		return -1
	}
	idx, ok := g.byName[strings.ToUpper(name)]
	if !ok {
		return -1
	}
	return idx
}

// Intersect reports whether two named surfaces share a waypoint within tol.
// A surface intersects itself if it has at least one point.
func (g *Graph) Intersect(nameA, nameB string) bool {
	_, _, _, ok := g.FindIntersection(nameA, nameB)
	return ok
}

// FindIntersection finds waypoints of A and B within tol and returns the
// closest pair (ties broken by scan order of A then B). The returned point is
// on surface A. ok is false if either surface is missing or no pair is within
// tolerance.
//
// Closest-within-tol (rather than first-within-tol) keeps junctions correct on
// dense polylines where several vertices of A lie within snap of a shared
// vertex on B.
//
// When nameA and nameB refer to the same surface, returns the first waypoint.
func (g *Graph) FindIntersection(nameA, nameB string) (pt Point, idxA, idxB int, ok bool) {
	if g == nil || g.apt == nil {
		return Point{}, -1, -1, false
	}
	ia := g.surfaceIndex(nameA)
	ib := g.surfaceIndex(nameB)
	if ia < 0 || ib < 0 {
		return Point{}, -1, -1, false
	}
	sa := &g.apt.Surfaces[ia]
	sb := &g.apt.Surfaces[ib]
	if len(sa.Points) == 0 || len(sb.Points) == 0 {
		return Point{}, -1, -1, false
	}
	if ia == ib {
		return sa.Points[0], 0, 0, true
	}
	tolSq := g.tolM * g.tolM
	bestD := tolSq + 1
	bestA, bestB := -1, -1
	for a := range sa.Points {
		pa := sa.Points[a]
		for b := range sb.Points {
			pb := sb.Points[b]
			d := geo.DistanceSq(pa.Lat, pa.Lon, pb.Lat, pb.Lon)
			if d <= tolSq && d < bestD {
				bestD = d
				bestA, bestB = a, b
			}
		}
	}
	if bestA < 0 {
		return Point{}, -1, -1, false
	}
	return sa.Points[bestA], bestA, bestB, true
}

// Neighbors returns names of surfaces that intersect name (excluding itself),
// sorted by surface order in the airport file. Missing name yields nil.
func (g *Graph) Neighbors(name string) []string {
	if g == nil || g.apt == nil {
		return nil
	}
	idx := g.surfaceIndex(name)
	if idx < 0 {
		return nil
	}
	out := make([]string, 0, len(g.adj[idx]))
	// Stable order: airport surface index order.
	for j := 0; j < len(g.apt.Surfaces); j++ {
		if _, ok := g.adj[idx][j]; ok {
			out = append(out, g.apt.Surfaces[j].Name)
		}
	}
	return out
}

// ClosestWaypoint finds the closest waypoint on the named surface to (lat, lon).
// Returns the point, its index, distance in meters, and ok=false if surface
// missing or has no points.
func (g *Graph) ClosestWaypoint(name string, lat, lon float64) (pt Point, idx int, distM float64, ok bool) {
	if g == nil || g.apt == nil {
		return Point{}, -1, 0, false
	}
	sidx := g.surfaceIndex(name)
	if sidx < 0 {
		return Point{}, -1, 0, false
	}
	s := &g.apt.Surfaces[sidx]
	if len(s.Points) == 0 {
		return Point{}, -1, 0, false
	}
	best := 0
	bestD := geo.Distance(lat, lon, s.Points[0].Lat, s.Points[0].Lon)
	for i := 1; i < len(s.Points); i++ {
		d := geo.Distance(lat, lon, s.Points[i].Lat, s.Points[i].Lon)
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return s.Points[best], best, bestD, true
}

// PointIndexWithin returns the index of the first point on the named surface
// within tol of p, or -1 if none.
func (g *Graph) PointIndexWithin(name string, p Point) int {
	if g == nil || g.apt == nil {
		return -1
	}
	sidx := g.surfaceIndex(name)
	if sidx < 0 {
		return -1
	}
	s := &g.apt.Surfaces[sidx]
	tolSq := g.tolM * g.tolM
	for i, q := range s.Points {
		if geo.DistanceSq(p.Lat, p.Lon, q.Lat, q.Lon) <= tolSq {
			return i
		}
	}
	return -1
}

func (g *Graph) buildAdjacency() {
	n := len(g.apt.Surfaces)
	tolSq := g.tolM * g.tolM
	for i := 0; i < n; i++ {
		si := &g.apt.Surfaces[i]
		if len(si.Points) == 0 {
			continue
		}
		for j := i + 1; j < n; j++ {
			sj := &g.apt.Surfaces[j]
			if len(sj.Points) == 0 {
				continue
			}
			if surfacesIntersect(si, sj, tolSq) {
				g.adj[i][j] = struct{}{}
				g.adj[j][i] = struct{}{}
			}
		}
	}
}

func surfacesIntersect(a, b *Surface, tolSq float64) bool {
	for _, pa := range a.Points {
		for _, pb := range b.Points {
			if geo.DistanceSq(pa.Lat, pa.Lon, pb.Lat, pb.Lon) <= tolSq {
				return true
			}
		}
	}
	return false
}
