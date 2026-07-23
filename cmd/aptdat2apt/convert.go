package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ConvertResult is the outcome of converting one airport.
type ConvertResult struct {
	ID, ICAOOut, FileName string
	Text                  string
	Written               bool
	SkipReason            string

	RunwaysIn, RunwaysOut    int
	TaxiNodesIn, TaxiEdgesIn int
	TaxiwaysOut              int
	UnnamedTaxiEdges         int
	TaxiNameSanitized        int
	ParkingIn, ParkingOut    int
	HoldsOut                 int
}

func convertAirport(a *rawAirport) ConvertResult {
	res := ConvertResult{
		ID: a.ID,
	}
	if a.Kind != 1 && a.Kind != 16 {
		// Heliports rarely have useful ground networks for sweatbox.
		if a.Kind == 17 {
			res.SkipReason = "heliport"
			return res
		}
	}

	g := parseGeometry(a)
	res.ICAOOut = g.ICAO
	res.RunwaysIn = len(g.Runways)
	res.TaxiNodesIn = len(g.Nodes)
	res.TaxiEdgesIn = len(g.Edges)
	res.ParkingIn = len(g.Ramps)

	icao := g.ICAO
	if icao == "" {
		icao = a.ID
	}
	res.ICAOOut = icao
	// Prefer 4-char codes for TWRTrainer; still write longer/shorter identifiers.
	fileBase := sanitizeFileBase(icao)
	if fileBase == "" {
		res.SkipReason = "no_id"
		return res
	}
	res.FileName = fileBase + ".apt"

	var b strings.Builder
	// Header
	fmt.Fprintf(&b, "icao=%s\n", icao)
	fmt.Fprintf(&b, "magnetic variation=0\n") // not present in apt.dat; WMM left to caller
	fmt.Fprintf(&b, "field elevation=%s\n", formatNum(g.ElevFt))
	fmt.Fprintf(&b, "pattern elevation=%s\n", formatNum(g.ElevFt+1000))
	fmt.Fprintf(&b, "pattern size=1\n")
	fmt.Fprintf(&b, "initial climb props=3000\n")
	fmt.Fprintf(&b, "initial climb jets=5000\n")
	fmt.Fprintf(&b, "registration=N\n")
	b.WriteByte('\n')

	// Runways
	usedRwyNames := make(map[string]int)
	for _, rw := range g.Runways {
		aEnd := normalizeRunwayDesignator(rw.EndA)
		bEnd := normalizeRunwayDesignator(rw.EndB)
		if aEnd == "" || bEnd == "" {
			continue
		}
		name := aEnd + "/" + bEnd
		if _, dup := usedRwyNames[name]; dup {
			continue
		}
		usedRwyNames[name] = 1
		dispA := int(math.Round(rw.DispA_M * 3.280839895))
		dispB := int(math.Round(rw.DispB_M * 3.280839895))
		if dispA < 0 {
			dispA = 0
		}
		if dispB < 0 {
			dispB = 0
		}
		fmt.Fprintf(&b, "[RUNWAY %s]\n", name)
		fmt.Fprintf(&b, "displaced threshold=%d/%d\n", dispA, dispB)
		fmt.Fprintf(&b, "turnoff=left\n")
		// TWRTrainer wants decimal points on both coords.
		fmt.Fprintf(&b, "%s %s\n", formatCoord(rw.LatA), formatCoord(rw.LonA))
		fmt.Fprintf(&b, "%s %s\n", formatCoord(rw.LatB), formatCoord(rw.LonB))
		b.WriteByte('\n')
		res.RunwaysOut++
	}

	// Taxiways from route network (prefer named taxiway/taxilane edges).
	taxiPolys, unnamed, renamed := buildTaxiwayPolylines(g)
	res.UnnamedTaxiEdges = unnamed
	res.TaxiNameSanitized = renamed
	// Sort for stable output
	names := make([]string, 0, len(taxiPolys))
	for n := range taxiPolys {
		names = append(names, n)
	}
	sort.Strings(names)
	usedPathNames := make(map[string]struct{})
	for _, name := range names {
		poly := taxiPolys[name]
		if len(poly) < 2 {
			continue
		}
		outName := uniquePathName(name, usedPathNames)
		fmt.Fprintf(&b, "[TAXIWAY %s]\n", outName)
		for _, p := range poly {
			fmt.Fprintf(&b, "%s %s\n", formatCoord(p.Lat), formatCoord(p.Lon))
		}
		b.WriteByte('\n')
		res.TaxiwaysOut++
	}

	// Parking
	usedPark := make(map[string]struct{})
	for i, r := range g.Ramps {
		name := sanitizeParkingName(r.Name, i)
		if name == "" {
			continue
		}
		base := name
		for n := 2; ; n++ {
			if _, ok := usedPark[name]; !ok {
				break
			}
			name = base + strconv.Itoa(n)
			if !isWordName(name) {
				name = fmt.Sprintf("P%d", i+1)
				break
			}
		}
		usedPark[name] = struct{}{}
		fmt.Fprintf(&b, "[PARKING %s]\n", name)
		fmt.Fprintf(&b, "%s %s\n\n", formatCoord(r.Lat), formatCoord(r.Lon))
		res.ParkingOut++
	}

	// Holds: one point per unique hold node (cap to avoid noise).
	holdIDs := make([]int, 0, len(g.HoldNodeIDs))
	for id := range g.HoldNodeIDs {
		holdIDs = append(holdIDs, id)
	}
	sort.Ints(holdIDs)
	holdCount := 0
	for _, id := range holdIDs {
		if holdCount >= 40 {
			break
		}
		n, ok := g.Nodes[id]
		if !ok {
			continue
		}
		base := sanitizeTaxiHoldName(g.HoldNodeIDs[id])
		if base == "" {
			base = "HS"
		}
		name := uniquePathName(base, usedPathNames)
		fmt.Fprintf(&b, "[HOLD %s]\n", name)
		fmt.Fprintf(&b, "%s %s\n\n", formatCoord(n.Lat), formatCoord(n.Lon))
		res.HoldsOut++
		holdCount++
	}

	res.Text = b.String()
	if res.RunwaysOut == 0 && res.TaxiwaysOut == 0 && res.ParkingOut == 0 {
		res.SkipReason = "no_geometry"
		return res
	}
	// Always write if we have any geometry (runway preferred but parking-only ok).
	res.Written = true
	return res
}

func sanitizeFileBase(id string) string {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeRunwayDesignator maps X-Plane "01"/"15L" to TWRTrainer "1"/"15L"
// (no leading zeros; optional L/R/C).
func normalizeRunwayDesignator(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Drop water/special suffixes that TWRTrainer does not accept.
	for _, suf := range []string{"W", "S", "T"} {
		if strings.HasSuffix(s, suf) && len(s) > 1 {
			// Only strip if remaining looks like a runway number.
			core := s[:len(s)-1]
			if isRunwayDesignator(core) || looksLikeRwyNum(core) {
				s = core
			}
		}
	}
	suf := ""
	if n := len(s); n > 0 {
		last := s[n-1]
		if last == 'L' || last == 'R' || last == 'C' {
			suf = string(last)
			s = s[:n-1]
		}
	}
	if s == "" {
		return ""
	}
	num, err := strconv.Atoi(s)
	if err != nil || num < 1 || num > 36 {
		return ""
	}
	return strconv.Itoa(num) + suf
}

func looksLikeRwyNum(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 36
}

func isRunwayDesignator(s string) bool {
	s = strings.ToUpper(s)
	suf := ""
	if n := len(s); n > 0 {
		last := s[n-1]
		if last == 'L' || last == 'R' || last == 'C' {
			suf = string(last)
			s = s[:n-1]
		}
	}
	_ = suf
	num, err := strconv.Atoi(s)
	if err != nil || num < 1 || num > 36 {
		return false
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	return true
}

// buildTaxiwayPolylines groups non-runway edges by sanitized name and extracts
// a representative polyline per name (longest path in each component; extra
// components get numeric suffixes).
func buildTaxiwayPolylines(g parsedGeometry) (polys map[string][]latLon, unnamedEdges, renamed int) {
	type edgeRef struct {
		from, to int
	}
	// name → undirected edges
	byName := make(map[string][]edgeRef)
	for _, e := range g.Edges {
		if isRunwayKind(e.Kind) {
			continue
		}
		// Only taxiway / taxilane / other ground routes.
		raw := strings.TrimSpace(e.Name)
		if raw == "" {
			unnamedEdges++
			// Skip unnamed — without a label they are not useful as TAXIWAY A/B/C.
			continue
		}
		name := sanitizeTaxiHoldName(raw)
		if name == "" {
			// Try harder: take first letter-run from raw.
			name = forceTaxiName(raw)
			if name == "" {
				unnamedEdges++
				continue
			}
			renamed++
		} else if name != strings.ToUpper(raw) {
			renamed++
		}
		byName[name] = append(byName[name], edgeRef{e.From, e.To})
	}

	polys = make(map[string][]latLon)
	used := make(map[string]struct{})

	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		edges := byName[name]
		// Build adjacency.
		adj := make(map[int][]int)
		for _, e := range edges {
			if _, ok := g.Nodes[e.from]; !ok {
				continue
			}
			if _, ok := g.Nodes[e.to]; !ok {
				continue
			}
			adj[e.from] = append(adj[e.from], e.to)
			adj[e.to] = append(adj[e.to], e.from)
		}
		if len(adj) == 0 {
			continue
		}
		// Connected components.
		visited := make(map[int]bool)
		var components [][]int
		for id := range adj {
			if visited[id] {
				continue
			}
			// BFS
			var comp []int
			q := []int{id}
			visited[id] = true
			for len(q) > 0 {
				u := q[0]
				q = q[1:]
				comp = append(comp, u)
				for _, v := range adj[u] {
					if !visited[v] {
						visited[v] = true
						q = append(q, v)
					}
				}
			}
			components = append(components, comp)
		}
		// For each component, extract longest simple path approximation.
		for ci, comp := range components {
			polyIDs := longestPathInComponent(comp, adj)
			if len(polyIDs) < 2 {
				continue
			}
			outName := name
			if ci > 0 {
				outName = nameWithSuffix(name, ci+1)
			}
			outName = uniqueMapKey(outName, used)
			pts := make([]latLon, 0, len(polyIDs))
			for _, id := range polyIDs {
				n := g.Nodes[id]
				pts = append(pts, latLon{Lat: n.Lat, Lon: n.Lon})
			}
			polys[outName] = pts
		}
	}
	return polys, unnamedEdges, renamed
}

// longestPathInComponent finds an approximate longest simple path via two BFS
// sweeps from an endpoint (or arbitrary node if the component is a cycle/branch).
func longestPathInComponent(comp []int, adj map[int][]int) []int {
	if len(comp) == 0 {
		return nil
	}
	// Prefer a degree-1 start.
	start := comp[0]
	for _, id := range comp {
		if len(adj[id]) == 1 {
			start = id
			break
		}
	}
	// Farthest from start.
	far := bfsFarthest(start, adj, comp)
	// Farthest from far → diameter endpoints.
	other := bfsFarthest(far, adj, comp)
	path := bfsPath(far, other, adj)
	if len(path) < 2 {
		// Fall back: DFS walk visiting as many as possible.
		path = greedyWalk(start, adj)
	}
	return path
}

func bfsFarthest(start int, adj map[int][]int, comp []int) int {
	type item struct{ id, d int }
	seen := map[int]bool{start: true}
	q := []item{{start, 0}}
	far := start
	farD := 0
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		if u.d > farD {
			farD = u.d
			far = u.id
		}
		for _, v := range adj[u.id] {
			if !seen[v] {
				seen[v] = true
				q = append(q, item{v, u.d + 1})
			}
		}
	}
	_ = comp
	return far
}

func bfsPath(start, goal int, adj map[int][]int) []int {
	if start == goal {
		return []int{start}
	}
	type item struct{ id int }
	prev := map[int]int{start: -1}
	q := []int{start}
	found := false
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		if u == goal {
			found = true
			break
		}
		for _, v := range adj[u] {
			if _, ok := prev[v]; ok {
				continue
			}
			prev[v] = u
			q = append(q, v)
		}
	}
	if !found {
		return nil
	}
	var rev []int
	for cur := goal; cur != -1; cur = prev[cur] {
		rev = append(rev, cur)
		if cur == start {
			break
		}
	}
	// reverse
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func greedyWalk(start int, adj map[int][]int) []int {
	seen := map[int]bool{start: true}
	path := []int{start}
	cur := start
	for {
		next := -1
		for _, v := range adj[cur] {
			if !seen[v] {
				next = v
				break
			}
		}
		if next < 0 {
			break
		}
		seen[next] = true
		path = append(path, next)
		cur = next
	}
	return path
}

func nameWithSuffix(base string, n int) string {
	// base is [A-Z]+\d*; append more digits carefully.
	// If base ends with digits, insert before them: A → A2, G1 → G12? Better: A2, G1B → force.
	return base + strconv.Itoa(n)
}

func uniqueMapKey(name string, used map[string]struct{}) string {
	if _, ok := used[name]; !ok {
		used[name] = struct{}{}
		return name
	}
	for i := 2; ; i++ {
		cand := nameWithSuffix(name, i)
		// Ensure still valid taxi name.
		if !isTaxiHoldName(cand) {
			cand = forceTaxiName(name + strconv.Itoa(i))
			if cand == "" {
				cand = "TW" + strconv.Itoa(i)
			}
		}
		if _, ok := used[cand]; !ok {
			used[cand] = struct{}{}
			return cand
		}
	}
}

func uniquePathName(name string, used map[string]struct{}) string {
	return uniqueMapKey(name, used)
}

// sanitizeTaxiHoldName matches TWRTrainer [A-Z]+\d* as closely as possible.
func sanitizeTaxiHoldName(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Keep only letters and digits; drop other chars.
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	s = b.String()
	if s == "" {
		return ""
	}
	// Must start with a letter.
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i == 0 {
		// Leading digits only — prefix with T.
		if isDigits(s) {
			return "T" + s
		}
		return ""
	}
	// After letters, only digits allowed; drop trailing junk letters after digits.
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	// If more letters appear after digits (e.g. A1B2), take letter-run + first digit-run only.
	out := s[:j]
	if !isTaxiHoldName(out) {
		return forceTaxiName(out)
	}
	return out
}

func forceTaxiName(s string) string {
	s = strings.ToUpper(s)
	var letters strings.Builder
	var digits strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' && digits.Len() == 0 {
			letters.WriteRune(r)
		} else if r >= '0' && r <= '9' && letters.Len() > 0 {
			digits.WriteRune(r)
		}
	}
	if letters.Len() == 0 {
		return ""
	}
	// Cap letter run length to keep names readable.
	ls := letters.String()
	if len(ls) > 8 {
		ls = ls[:8]
	}
	return ls + digits.String()
}

func sanitizeParkingName(raw string, index int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Sprintf("P%d", index+1)
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return fmt.Sprintf("P%d", index+1)
	}
	// TWRTrainer \w+ — already word chars. Upper-case for consistency.
	return strings.ToUpper(s)
}

func isTaxiHoldName(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isWordName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func formatNum(v float64) string {
	// Prefer integer when whole.
	if v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatCoord ensures a decimal point is present (TWRTrainer requirement).
func formatCoord(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	// Trim trailing zeros but keep at least one decimal digit.
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		if strings.HasSuffix(s, ".") {
			s += "0"
		}
	} else {
		s += ".0"
	}
	return s
}
