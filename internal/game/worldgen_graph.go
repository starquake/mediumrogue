package game

import (
	"math"
	"os"
	"strconv"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// worldgen_graph.go (#458, EXPERIMENT) — graph-first world generation.
//
// THIS BRANCH REPLACES THE NOISE GENERATOR OUTRIGHT. It is not flag-gated:
// staging runs the noise world and is the control, development runs this and
// is the experiment, and comparing the two by walking them is the point.
// Nothing here is meant to merge as-is.
//
// The shape (#458): place nodes per difficulty ring, connect each to a node
// nearer the origin (a spanning tree, so the world is connected by
// construction), add a few long loops and dead-end spurs, then carve walkable
// corridors along every edge and blobs at every node. Everything not carved is
// impassable — which is the whole point, since the noise generator's world is
// 82.8% walkable and has no negative space doing structural work.
//
// Every knob is an env var so a world can be retuned by restarting the
// container rather than rebuilding the image.

// graphTuning holds the knobs. Defaults are the mockup's "target" panel.
type graphTuning struct {
	nodesRing0, nodesRing1, nodesRing2 int
	nodeRadius, pathRadius, spurRadius int
	loops, deadEnds, spurLen           int
	minLoopDist                        int
	lakes                              int
}

func graphTuningFromEnv() graphTuning {
	return graphTuning{
		// Node counts scale with RING AREA, not flat. At radius 120 the bands
		// hold roughly 4.9k / 14.5k / 24.1k hexes — a 1:3:5 ratio — so flat
		// counts leave the frontier ring almost entirely impassable, which is
		// where the hardest monsters are meant to live.
		nodesRing0:  envInt("WORLDGEN_NODES_R0", 5),
		nodesRing1:  envInt("WORLDGEN_NODES_R1", 15),
		nodesRing2:  envInt("WORLDGEN_NODES_R2", 26),
		nodeRadius:  envInt("WORLDGEN_NODE_RADIUS", 10),
		pathRadius:  envInt("WORLDGEN_PATH_RADIUS", 4),
		spurRadius:  envInt("WORLDGEN_SPUR_RADIUS", 6),
		loops:       envInt("WORLDGEN_LOOPS", 6),
		deadEnds:    envInt("WORLDGEN_DEAD_ENDS", 10),
		spurLen:     envInt("WORLDGEN_SPUR_LEN", 16),
		minLoopDist: envInt("WORLDGEN_MIN_LOOP_DIST", 30),
		lakes:       envInt("WORLDGEN_LAKES", 5),
	}
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}

	return fallback
}

type graphNode struct {
	hex  protocol.Hex
	spur bool
}

// generateGraphMap builds the world. Same signature contract as the noise
// generator: same (seed, radius) always produces the same map.
func generateGraphMap(seed uint64, radius int) protocol.MapResponse {
	t := graphTuningFromEnv()
	rng := newGraphRand(seed)

	nodes := placeGraphNodes(rng, radius, t)
	edges := connectGraphNodes(rng, nodes, t)
	nodes, edges = addGraphSpurs(rng, nodes, edges, radius, t)
	walk := carveGraph(rng, nodes, edges, radius, t)

	return paintGraphTerrain(seed, radius, walk, lakeHexes(rng, walk, radius, t))
}

// newGraphRand is a small deterministic PCG-alike; worldgen must not consume
// the world's own rng streams.
func newGraphRand(seed uint64) func() float64 {
	state := seed*6364136223846793005 + 1442695040888963407

	return func() float64 {
		state = state*6364136223846793005 + 1442695040888963407
		x := state
		x ^= x >> 33
		x *= 0xff51afd7ed558ccd
		x ^= x >> 33

		return float64(x>>11) / float64(uint64(1)<<53)
	}
}

// axialFromPolar converts a PIXEL distance and angle to the nearest axial hex,
// using the flat-top layout's standard inverse.
func axialFromPolar(pixels, angle float64) protocol.Hex {
	x, y := pixels*math.Cos(angle), pixels*math.Sin(angle)

	//nolint:revive // add-constant: the 2/3 and sqrt(3)/3 are the axial conversion itself.
	return protocol.Hex{Q: int(math.Round(x * 2 / 3)), R: int(math.Round(-x/3 + y*math.Sqrt(3)/3))}
}

// polarHex places a hex at approximately the given HEX distance from the
// origin along an angle.
//
// The correction step is load-bearing. Pixel distance and hex distance differ
// by a direction-dependent factor (1.5 along q, sqrt(3) along r), so feeding a
// hex distance straight into the pixel conversion places nodes at roughly 2/3
// of the intended range — which put every ring-2 node inside ring 1 and left
// the frontier all but impassable. Measuring the first guess and rescaling
// lands within a hex or two at every angle.
func polarHex(hexes, angle float64) protocol.Hex {
	origin := protocol.Hex{Q: 0, R: 0}

	//nolint:revive // add-constant: 1.5 is the flat-top q-axis pixel pitch.
	guess := axialFromPolar(hexes*1.5, angle)

	actual := HexDistance(origin, guess)
	if actual == 0 {
		return guess
	}

	//nolint:revive // add-constant: 1.5 again, same pitch.
	return axialFromPolar(hexes*1.5*hexes/float64(actual), angle)
}

// placeGraphNodes seeds the origin plus n nodes in each difficulty ring, so
// paths lead outward through the existing difficulty bands.
func placeGraphNodes(rng func() float64, radius int, t graphTuning) []graphNode {
	origin := protocol.Hex{Q: 0, R: 0}
	nodes := []graphNode{{hex: origin}}
	counts := [protocol.RingCount]int{t.nodesRing0, t.nodesRing1, t.nodesRing2}

	for ring := range protocol.RingCount {
		inner := float64(ring) * float64(radius) / float64(protocol.RingCount)
		outer := float64(ring+1) * float64(radius) / float64(protocol.RingCount)

		for range counts[ring] {
			h := polarHex(inner+rng()*(outer-inner), rng()*2*math.Pi)
			if HexDistance(origin, h) <= radius-3 {
				nodes = append(nodes, graphNode{hex: h})
			}
		}
	}

	return nodes
}

// connectGraphNodes links every node to the nearest node CLOSER to the origin
// — a spanning tree rooted at the origin, so the world is connected by
// construction — then adds long loops. Loops are deliberately long: a link
// between adjacent nodes is a shortcut that makes travel more direct, which is
// the opposite of what this generator is for.
func connectGraphNodes(rng func() float64, nodes []graphNode, t graphTuning) [][2]int {
	origin := protocol.Hex{Q: 0, R: 0}
	edges := make([][2]int, 0, len(nodes)+t.loops)

	for i := 1; i < len(nodes); i++ {
		best, bestDist := -1, 1<<30

		for j := range nodes {
			if i == j || HexDistance(origin, nodes[j].hex) >= HexDistance(origin, nodes[i].hex) {
				continue
			}

			if d := HexDistance(nodes[i].hex, nodes[j].hex); d < bestDist {
				bestDist, best = d, j
			}
		}

		if best >= 0 {
			edges = append(edges, [2]int{i, best})
		}
	}

	for range t.loops {
		i := 1 + int(rng()*float64(len(nodes)-1))
		best, bestDist := -1, -1

		for j := 1; j < len(nodes); j++ {
			if i == j {
				continue
			}

			if d := HexDistance(nodes[i].hex, nodes[j].hex); d >= t.minLoopDist && d > bestDist {
				bestDist, best = d, j
			}
		}

		if best >= 0 {
			edges = append(edges, [2]int{i, best})
		}
	}

	return edges
}

// addGraphSpurs hangs dead ends off existing nodes — the places a reward would
// live, and the reason exploration is a choice rather than a route.
func addGraphSpurs(
	rng func() float64, nodes []graphNode, edges [][2]int, radius int, t graphTuning,
) ([]graphNode, [][2]int) {
	origin := protocol.Hex{Q: 0, R: 0}

	for range t.deadEnds {
		from := 1 + int(rng()*float64(len(nodes)-1))
		off := polarHex(float64(t.spurLen), rng()*2*math.Pi)
		tip := protocol.Hex{Q: nodes[from].hex.Q + off.Q, R: nodes[from].hex.R + off.R}

		if HexDistance(origin, tip) > radius-3 {
			continue
		}

		nodes = append(nodes, graphNode{hex: tip, spur: true})
		edges = append(edges, [2]int{len(nodes) - 1, from})
	}

	return nodes, edges
}

// carveGraph opens the walkable set: a blob at every node, and a corridor
// along every edge. Everything else stays impassable.
func carveGraph(rng func() float64, nodes []graphNode, edges [][2]int, radius int, t graphTuning) map[protocol.Hex]bool {
	origin := protocol.Hex{Q: 0, R: 0}
	walk := make(map[protocol.Hex]bool)

	carve := func(centre protocol.Hex, r int) {
		for dq := -r; dq <= r; dq++ {
			for dr := -r; dr <= r; dr++ {
				h := protocol.Hex{Q: centre.Q + dq, R: centre.R + dr}
				if HexDistance(centre, h) <= r && HexDistance(origin, h) <= radius-1 {
					walk[h] = true
				}
			}
		}
	}

	for _, n := range nodes {
		r := t.nodeRadius
		if n.spur {
			r = t.spurRadius
		}

		carve(n.hex, r)
	}

	for _, e := range edges {
		a, b := nodes[e[0]].hex, nodes[e[1]].hex

		steps := max(1, HexDistance(a, b))
		for s := 0; s <= steps; s++ {
			f := float64(s) / float64(steps)
			// A little jitter so a corridor is not a drawn line. Kept small:
			// large perpendicular displacement disconnected the corridor from
			// its endpoints when this was tried in the mockup.
			jitter := (rng() - 0.5) * 2
			carve(protocol.Hex{
				Q: a.Q + int(math.Round(float64(b.Q-a.Q)*f+jitter)),
				R: a.R + int(math.Round(float64(b.R-a.R)*f+jitter)),
			}, t.pathRadius)
		}
	}

	// The origin clearing must always be walkable — players spawn there.
	carve(origin, clearingRadius)

	return walk
}

// lakeHexes places a few solid lakes in the void. Solid regions, never
// per-hex randomness: speckled water destroys the read of the map entirely.
func lakeHexes(rng func() float64, walk map[protocol.Hex]bool, radius int, t graphTuning) map[protocol.Hex]bool {
	origin := protocol.Hex{Q: 0, R: 0}
	lakes := make(map[protocol.Hex]bool)

	for range t.lakes {
		centre := polarHex(rng()*float64(radius-10), rng()*2*math.Pi)
		if walk[centre] {
			continue
		}

		//nolint:revive // add-constant: lake radius range, tuned by eye.
		r := 6 + int(rng()*7)

		for dq := -r; dq <= r; dq++ {
			for dr := -r; dr <= r; dr++ {
				h := protocol.Hex{Q: centre.Q + dq, R: centre.R + dr}
				if HexDistance(centre, h) <= r && HexDistance(origin, h) <= radius-1 && !walk[h] {
					lakes[h] = true
				}
			}
		}
	}

	return lakes
}

// paintGraphTerrain assigns biomes to the carved space and fills the void.
//
// Biome choice reuses the noise fields so grass/forest/mud form coherent
// patches rather than per-hex speckle — and, critically, so that EVERY RING
// GETS EVERY BIOME. ValidateSpawnTerrainCoverage (#436/#438) refuses to boot a
// world whose rings lack the terrain a confined kind spawns on, so a graph
// world that painted, say, all-grass corridors would not start at all.
func paintGraphTerrain(
	seed uint64, radius int, walk, lakes map[protocol.Hex]bool,
) protocol.MapResponse {
	origin := protocol.Hex{Q: 0, R: 0}
	tiles := make([]protocol.Tile, 0, tileCount(radius))

	for q := -radius; q <= radius; q++ {
		for r := -radius; r <= radius; r++ {
			h := protocol.Hex{Q: q, R: r}

			d := HexDistance(origin, h)
			if d > radius {
				continue
			}

			tiles = append(tiles, protocol.Tile{Hex: h, Terrain: graphTerrainAt(seed, h, d, radius, walk, lakes)})
		}
	}

	return protocol.MapResponse{Radius: radius, Tiles: tiles}
}

func graphTerrainAt(
	seed uint64, h protocol.Hex, dist, radius int, walk, lakes map[protocol.Hex]bool,
) protocol.Terrain {
	switch {
	case dist == radius:
		return protocol.TerrainRock
	case !walk[h]:
		if lakes[h] {
			return protocol.TerrainWater
		}

		return protocol.TerrainRock
	case dist <= clearingRadius:
		return protocol.TerrainGrass
	}

	fx := float64(h.Q) * noiseScale
	fy := (float64(h.R) + float64(h.Q)*0.5) * noiseScale

	// Two independent fields, same as the noise generator, so biomes form
	// patches. Thresholds are deliberately generous: the carved space is a
	// fraction of the map, so a biome that is rare across the whole world can
	// be absent from a ring entirely and fail the coverage guard.
	moisture := fbm(seed^moistureSalt, fx, fy)

	switch {
	case moisture > 0.58:
		return protocol.TerrainForest
	case moisture > 0.46:
		return protocol.TerrainMud
	default:
		return protocol.TerrainGrass
	}
}

// WorldShape reports the generated world's shape, for a startup log line.
// Tuning by restarting the container is only useful if you can see what the
// last restart produced. Deliberately cheap — walkable share and connectivity,
// no detour BFS.
func (w *World) WorldShape() (walkablePct, connectedPct float64, byRing [protocol.RingCount]int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	walkable := 0

	for _, t := range w.worldMap.Tiles {
		if terrainWalkable(t.Terrain) {
			walkable++
			byRing[ringOf(t.Hex, w.radius)]++
		}
	}

	if walkable == 0 {
		return 0, 0, byRing
	}

	//nolint:revive // add-constant: percentage conversion.
	return 100 * float64(walkable) / float64(len(w.worldMap.Tiles)),
		100 * float64(len(w.spawnable)) / float64(walkable), byRing
}
