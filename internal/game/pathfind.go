package game

import "github.com/starquake/mediumrogue/internal/protocol"

// Pathfind returns the shortest walkable route from `from` to `to` on the
// flat-top hex grid, as the ordered list of steps excluding `from` and
// including `to`. Movement is uniform-cost, so breadth-first search yields a
// shortest path; the deterministic HexNeighbors order makes the result
// reproducible. Returns an empty (non-nil) slice when from == to, and nil
// when `to` is not walkable or is unreachable.
//
// The walkable predicate gates which hexes the search may enter, so callers
// decide the terrain rules (the World passes walkableLocked).
func Pathfind(from, to protocol.Hex, walkable func(protocol.Hex) bool) []protocol.Hex {
	if from == to {
		return []protocol.Hex{}
	}

	if !walkable(to) {
		return nil
	}

	cameFrom := map[protocol.Hex]protocol.Hex{from: from}
	queue := []protocol.Hex{from}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur == to {
			return reconstruct(cameFrom, from, to)
		}

		for _, n := range HexNeighbors(cur) {
			if _, seen := cameFrom[n]; seen || !walkable(n) {
				continue
			}

			cameFrom[n] = cur
			queue = append(queue, n)
		}
	}

	return nil
}

// reconstruct walks the cameFrom chain from `to` back to `from`, then reverses
// it into forward order (excluding `from`).
func reconstruct(cameFrom map[protocol.Hex]protocol.Hex, from, to protocol.Hex) []protocol.Hex {
	var reversed []protocol.Hex
	for cur := to; cur != from; cur = cameFrom[cur] {
		reversed = append(reversed, cur)
	}

	path := make([]protocol.Hex, len(reversed))
	for i, h := range reversed {
		path[len(reversed)-1-i] = h
	}

	return path
}
