package main

import (
	"strconv"
	"strings"
)

// rawAirport is one airport block from apt.dat (header + body lines).
type rawAirport struct {
	Kind     int // 1 land, 16 seaplane, 17 heliport
	ElevFt   float64
	ID       string
	Name     string
	ICAOMeta string // from 1302 icao_code if present
	Lines    []string
	Codes    map[string]int
}

func parseAirportHeader(fields []string, line string) *rawAirport {
	// 1 <elev_ft> <deprecated> <deprecated> <ID> <name...>
	a := &rawAirport{
		Codes: make(map[string]int),
	}
	if n, err := strconv.Atoi(fields[0]); err == nil {
		a.Kind = n
	}
	if len(fields) >= 2 {
		a.ElevFt, _ = strconv.ParseFloat(fields[1], 64)
	}
	if len(fields) >= 5 {
		a.ID = strings.ToUpper(fields[4])
	}
	if len(fields) >= 6 {
		// Name is everything after ID; recover from original line for spaces.
		// fields[4] is ID — find it in the line and take the remainder.
		idx := strings.Index(line, fields[4])
		if idx >= 0 {
			rest := strings.TrimSpace(line[idx+len(fields[4]):])
			a.Name = rest
		}
	}
	return a
}

// latLon is a geographic point.
type latLon struct {
	Lat, Lon float64
}

// taxiNode is a 1201 taxi-route network node.
type taxiNode struct {
	ID       int
	Lat, Lon float64
}

// taxiEdge is a 1202 taxi-route network edge.
type taxiEdge struct {
	From, To int
	Kind     string // runway, taxiway, taxilane, ...
	Name     string // raw name (may be empty)
	OneWay   bool
}

// landRunway is a code-100 land runway.
type landRunway struct {
	WidthM  float64
	EndA    string
	LatA    float64
	LonA    float64
	DispA_M float64 // displaced threshold meters
	EndB    string
	LatB    float64
	LonB    float64
	DispB_M float64
}

// rampStart is a code-1300 (or legacy 15) parking / startup location.
type rampStart struct {
	Lat, Lon float64
	Heading  float64
	Type     string // gate, hangar, tie_down, misc
	Ops      string // jets|turboprops|...
	Name     string
}

// parsedGeometry is the convertible subset of an airport.
type parsedGeometry struct {
	ICAO    string
	ElevFt  float64
	Runways []landRunway
	Nodes   map[int]taxiNode
	Edges   []taxiEdge
	Ramps   []rampStart
	// hold candidate nodes: nodes that appear on a 1204-marked edge (runway active zone)
	HoldNodeIDs map[int]string // node id → suggested hold name
}

func parseGeometry(a *rawAirport) parsedGeometry {
	g := parsedGeometry{
		ICAO:        pickICAO(a),
		ElevFt:      a.ElevFt,
		Nodes:       make(map[int]taxiNode),
		HoldNodeIDs: make(map[int]string),
	}

	// First pass: metadata + structural rows.
	for _, line := range a.Lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "1302":
			// 1302 icao_code KBTV
			if len(fields) >= 3 && fields[1] == "icao_code" {
				a.ICAOMeta = strings.ToUpper(fields[2])
				g.ICAO = pickICAO(a)
			}
		case "100":
			if rw, ok := parseLandRunway(fields); ok {
				g.Runways = append(g.Runways, rw)
			}
		case "1201":
			if n, ok := parseTaxiNode(fields); ok {
				g.Nodes[n.ID] = n
			}
		case "1202":
			if e, ok := parseTaxiEdge(fields); ok {
				g.Edges = append(g.Edges, e)
			}
		case "1204":
			// 1204 <arrival|departure|ils> <runway ends...>
			// Attach to the previous edge's nodes as hold candidates.
			// We handle holds in a second pass below.
		case "1300":
			if r, ok := parseRamp1300(fields); ok {
				g.Ramps = append(g.Ramps, r)
			}
		case "15":
			if r, ok := parseRamp15(fields); ok {
				g.Ramps = append(g.Ramps, r)
			}
		}
	}

	// Second pass: 1204 active zones → hold points at edge endpoints.
	// Track last 1202 edge for association.
	var lastEdge *taxiEdge
	edgeIdx := -1
	for _, line := range a.Lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "1202":
			edgeIdx++
			if edgeIdx >= 0 && edgeIdx < len(g.Edges) {
				lastEdge = &g.Edges[edgeIdx]
			}
		case "1204":
			if lastEdge == nil {
				continue
			}
			// Only mark holds for taxiway edges that conflict with runways.
			if isRunwayKind(lastEdge.Kind) {
				continue
			}
			rwyHint := ""
			if len(fields) >= 3 {
				rwyHint = fields[2]
			}
			name := holdNameFromHint(rwyHint, lastEdge.Name)
			g.HoldNodeIDs[lastEdge.From] = name
			g.HoldNodeIDs[lastEdge.To] = name
		}
	}

	return g
}

func pickICAO(a *rawAirport) string {
	if a.ICAOMeta != "" {
		return strings.ToUpper(a.ICAOMeta)
	}
	return strings.ToUpper(a.ID)
}

func parseLandRunway(fields []string) (landRunway, bool) {
	// apt.dat 1000–1200 land runway (X-Plane 12 uses the same column layout;
	// surface type codes expanded in 1200 — still a single integer field):
	//
	// 100 width surf shoulder smooth centerlights edgelights autosigns
	//     endA lat lon disp_m blast_m mark app tdze reil
	//     endB lat lon disp_m blast_m mark app tdze reil
	//
	// Field indices (0-based):
	//   0=100 1=width 2=surf 3=shoulder 4=smooth 5=center 6=edge 7=autosigns
	//   8=endA 9=latA 10=lonA 11=dispA 12=blastA 13=markA 14=appA 15=tdzA 16=reilA
	//   17=endB 18=latB 19=lonB 20=dispB ...
	// Minimum field count: 1+7 + 8 + 8 = 24
	//
	// XP12 shoulder may be "surface + 100×width_m" (e.g. 206) — still one token.
	if len(fields) < 24 {
		return landRunway{}, false
	}
	width, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return landRunway{}, false
	}
	endA := fields[8]
	latA, err1 := strconv.ParseFloat(fields[9], 64)
	lonA, err2 := strconv.ParseFloat(fields[10], 64)
	dispA, err3 := strconv.ParseFloat(fields[11], 64)
	endB := fields[17]
	latB, err4 := strconv.ParseFloat(fields[18], 64)
	lonB, err5 := strconv.ParseFloat(fields[19], 64)
	dispB, err6 := strconv.ParseFloat(fields[20], 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
		return landRunway{}, false
	}
	return landRunway{
		WidthM:  width,
		EndA:    endA,
		LatA:    latA,
		LonA:    lonA,
		DispA_M: dispA,
		EndB:    endB,
		LatB:    latB,
		LonB:    lonB,
		DispB_M: dispB,
	}, true
}

func parseTaxiNode(fields []string) (taxiNode, bool) {
	// 1201 lat lon usage id [name...]
	if len(fields) < 5 {
		return taxiNode{}, false
	}
	lat, err1 := strconv.ParseFloat(fields[1], 64)
	lon, err2 := strconv.ParseFloat(fields[2], 64)
	id, err3 := strconv.Atoi(fields[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return taxiNode{}, false
	}
	return taxiNode{ID: id, Lat: lat, Lon: lon}, true
}

func parseTaxiEdge(fields []string) (taxiEdge, bool) {
	// 1202 from to twoway|oneway <kind>[_size] [name...]
	// XP11/12 examples:
	//   1202 5258 5266 twoway taxiway B
	//   1202 260 284 twoway taxiway_E B          (XP12 ICAO width class on kind)
	//   1202 1 2 twoway taxilane_C A1
	//   1202 1 2 twoway runway 16L/34R
	if len(fields) < 5 {
		return taxiEdge{}, false
	}
	from, err1 := strconv.Atoi(fields[1])
	to, err2 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil {
		return taxiEdge{}, false
	}
	dir := strings.ToLower(fields[3])
	oneWay := dir == "oneway"
	kindRaw := fields[4]
	kind := strings.ToLower(kindRaw)
	// Strip ICAO width suffix: taxiway_E → taxiway, taxilane_C → taxilane
	if i := strings.IndexByte(kind, '_'); i >= 0 {
		kind = kind[:i]
	}
	name := ""
	if len(fields) >= 6 {
		name = strings.Join(fields[5:], " ")
		name = strings.TrimSpace(name)
	}
	return taxiEdge{
		From:   from,
		To:     to,
		Kind:   kind,
		Name:   name,
		OneWay: oneWay,
	}, true
}

func parseRamp1300(fields []string) (rampStart, bool) {
	// 1300 lat lon heading loc_type airplane_types name...
	// loc_type: gate|hangar|misc|tie_down
	// XP12 airplane_types examples:
	//   jets|turboprops
	//   heavy|jets|turboprops|props|helos
	//   none
	if len(fields) < 6 {
		return rampStart{}, false
	}
	lat, err1 := strconv.ParseFloat(fields[1], 64)
	lon, err2 := strconv.ParseFloat(fields[2], 64)
	hdg, err3 := strconv.ParseFloat(fields[3], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return rampStart{}, false
	}
	typ := fields[4]
	ops := ""
	nameParts := fields[5:]
	// airplane_types is one token, often pipe-separated equipment classes.
	if len(nameParts) >= 1 && looksLikeOpsToken(nameParts[0]) {
		ops = nameParts[0]
		nameParts = nameParts[1:]
	}
	name := strings.TrimSpace(strings.Join(nameParts, " "))
	if name == "" {
		name = typ
	}
	return rampStart{
		Lat:     lat,
		Lon:     lon,
		Heading: hdg,
		Type:    typ,
		Ops:     ops,
		Name:    name,
	}, true
}

// looksLikeOpsToken reports XP ramp "airplane types" fields (often pipe lists).
func looksLikeOpsToken(s string) bool {
	low := strings.ToLower(s)
	if low == "heavy" || low == "fighters" || low == "none" || low == "all" ||
		low == "cargo" || low == "military" {
		return true
	}
	if strings.Contains(low, "jet") || strings.Contains(low, "prop") ||
		strings.Contains(low, "helo") || strings.Contains(low, "heavy") {
		return true
	}
	// pipe-separated class list without spaces: heavy|jets|turboprops
	if strings.Contains(s, "|") {
		return true
	}
	return false
}

func parseRamp15(fields []string) (rampStart, bool) {
	// 15 lat lon heading name...
	if len(fields) < 5 {
		return rampStart{}, false
	}
	lat, err1 := strconv.ParseFloat(fields[1], 64)
	lon, err2 := strconv.ParseFloat(fields[2], 64)
	hdg, err3 := strconv.ParseFloat(fields[3], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return rampStart{}, false
	}
	name := strings.TrimSpace(strings.Join(fields[4:], " "))
	if name == "" {
		name = "START"
	}
	return rampStart{Lat: lat, Lon: lon, Heading: hdg, Type: "misc", Name: name}, true
}

func isRunwayKind(kind string) bool {
	return kind == "runway"
}

func holdNameFromHint(rwyHint, edgeName string) string {
	// Prefer runway end designator; fall back to edge name.
	// XP12 1204 may list several ends: "16L,34R" or "16L 34R".
	// TWRTrainer hold names are [A-Z]+\d* — runway "16L" becomes "H16".
	h := strings.ToUpper(strings.TrimSpace(rwyHint))
	if i := strings.IndexByte(h, ','); i >= 0 {
		h = h[:i]
	}
	if parts := strings.Fields(h); len(parts) > 0 {
		h = parts[0]
	}
	h = strings.ReplaceAll(h, "/", "")
	if s := sanitizeTaxiHoldName(h); s != "" {
		return s
	}
	if s := runwayHoldToken(h); s != "" {
		return s
	}
	if s := sanitizeTaxiHoldName(edgeName); s != "" {
		return s
	}
	return "HS"
}

// runwayHoldToken maps "16L"/"34R"/"9" → "H16"/"H34"/"H9" for HOLD names.
func runwayHoldToken(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if c := s[len(s)-1]; c == 'L' || c == 'R' || c == 'C' {
		s = s[:len(s)-1]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 36 {
		return ""
	}
	return "H" + strconv.Itoa(n)
}
