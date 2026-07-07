package game

import "github.com/starquake/medium-rogue/internal/protocol"

// Hex math on axial coordinates, straight from Red Blob Games' hex guide
// (flat-top orientation). The cube s-coordinate is implicit: s = -q-r.

// HexDistance is the number of hex steps between a and b: half the cube-
// coordinate L1 norm (each step changes two of the three cube axes by one).
func HexDistance(a, b protocol.Hex) int {
	dq := a.Q - b.Q
	dr := a.R - b.R
	ds := -dq - dr

	return (abs(dq) + abs(dr) + abs(ds)) / 2 //nolint:mnd // the /2 is the cube-distance formula itself.
}

// HexNeighbors returns the six adjacent hexes of h, in the flat-top
// direction order N, NE, SE, S, SW, NW (matching the W/E/D/X/A/Q keys).
func HexNeighbors(h protocol.Hex) [6]protocol.Hex {
	return [6]protocol.Hex{
		{Q: h.Q, R: h.R - 1},     // N
		{Q: h.Q + 1, R: h.R - 1}, // NE
		{Q: h.Q + 1, R: h.R},     // SE
		{Q: h.Q, R: h.R + 1},     // S
		{Q: h.Q - 1, R: h.R + 1}, // SW
		{Q: h.Q - 1, R: h.R},     // NW
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}
