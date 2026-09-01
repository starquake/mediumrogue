package game

import (
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"slices"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// spawn.go (#416): placement — where a new entity goes, and where monsters are
// seeded at startup.
//
// Extracted verbatim from world.go, which had grown to 4,646 lines and was
// absorbing every new mechanic by default. Same package, same unexported
// names, no behaviour change: this is a move, chosen as the first seam because
// placement is self-contained and nothing else in world.go calls into it
// except SpawnMonsters and Join.
//
// The tiered-fallback shape in spawnHexLocked is the pattern worth knowing
// about here — four tiers, each relaxing a guard, ending in one that ignores
// every guard so a crowded map can never fail a join.

// SpawnMonsters adds n monster entities at random walkable hexes, chosen
// with the world seed so a given seed is reproducible: placement is
// distributed across the map's difficulty rings (ringOf, worldgen.go)
// weighted by each ring's candidate-hex count (a proxy for its area that is
// naturally terrain-aware — water/rock reduce a ring's usable area too),
// and each placement picks a kind uniformly among the kinds registered for
// that ring (content.go's monsterDefs' own rings field), capping dragon at
// protocol.DragonCount for the whole call. Skips hexes already at
// StackCap and, when at least one candidate allows it, hexes on/within
// CombatRadius of a living player (tooCloseToPlayerLocked — #36) or within
// protocol.SanctuaryRadius of the origin (tooCloseToSanctuaryLocked — 6c);
// if EVERY walkable hex fails one of those guards, both are dropped
// entirely for this call rather than placing nothing (the pre-#36
// behavior, so a tiny or crowded map never silently spawns fewer monsters
// than requested for lack of a "safe" hex). Intended for **startup, before
// any player joins** (server startup via MONSTER_COUNT, or tests), where
// the player guard is inert today — it exists for a future
// continuous/respawn spawner called mid-run.
func (w *World) SpawnMonsters(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	//nolint:gosec // deterministic seeded placement, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), spawnStream))

	byRing, ringWeights := w.spawnCandidatesByRingLocked(rng)
	kindsByRing := kindsPerRing()

	// The dragon cap is per WORLD, not per call: start from the dragons
	// already alive (a previous SpawnMonsters call, or a SpawnMonsterKindAt-
	// seeded one), so the future continuous/density spawner calling this
	// again mid-run can never stack a second dragon past DragonCount.
	dragonsPlaced := w.livingDragonsLocked()
	placed := 0

	for placed < n {
		h, r, ok := nextSpawnHexLocked(rng, byRing, ringWeights)
		if !ok {
			break // every ring is out of both weight and candidates
		}

		if w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		// Terrain-confined kinds (#436's skeleton in mud, #438's Woodwose in
		// forest) are filtered out of the candidate pool by the hex's terrain
		// rather than the placement being retried: dropping the kind here lets
		// ordinary ground still spawn something, where skipping the hex would
		// silently thin the world everywhere those terrains are not. The
		// consequence — a confined kind's frequency becomes a function of its
		// terrain's coverage — is stated on both tickets.
		//
		// Order-preserving, because pickSpawnKind draws from it with the
		// seeded rng and determinism is load-bearing.
		ringKinds := kindsForTerrain(kindsByRing[r], w.terrain[h])

		kindID, ok := pickSpawnKind(rng, ringKinds, dragonsPlaced)
		if !ok {
			continue // ring exhausted of spawnable kinds (dragon-only ring, cap reached)
		}

		if kindID == idKindDragon {
			dragonsPlaced++
		}

		k := monsterDefByID[kindID]

		w.nextID++
		e := newMonsterEntity(w.nextID, h, k)
		// Set HERE and nowhere else: burial is a SPAWN state, so a monster
		// raised mid-fight by a summoner (summon.go) or placed by a test must
		// never inherit it. newMonsterEntity is shared with both.
		e.buried = k.buriesOnSpawn
		w.entities[w.nextID] = e
		placed++
	}
}

// kindsForTerrain returns the kinds that may spawn on terrain t, order
// preserved: every unconfined kind, plus the confined ones whose spawnTerrain
// is t. Mirrors excludeKind, which does the same job for the dragon cap.
func kindsForTerrain(kinds []string, t protocol.Terrain) []string {
	out := make([]string, 0, len(kinds))

	for _, id := range kinds {
		if st := monsterDefByID[id].spawnTerrain; st == "" || st == t {
			out = append(out, id)
		}
	}

	return out
}

// ErrRingLacksSpawnTerrain is returned by ValidateSpawnTerrainCoverage when a
// world's terrain cannot support a terrain-confined monster kind somewhere it
// is meant to spawn.
var ErrRingLacksSpawnTerrain = errors.New("difficulty ring lacks the terrain a confined monster kind spawns on")

// ValidateSpawnTerrainCoverage reports whether this world's terrain can
// actually support every terrain-confined kind in every ring that kind spawns
// in — the skeleton's mud (#436), the Woodwose's forest (#438).
//
// WHY THIS EXISTS. A confined kind spawns only on its own terrain, terrain is
// generated from noise, and nothing ties where that noise puts a biome to the
// ring bands. So a seed can leave a ring without it — and that ring then has
// none of that kind at all, silently, with nothing failing. Measured over 500
// seeds while tuning #437, roughly 1 in 200-500 does exactly that for mud, and
// no threshold removes the risk without making mud stop reading as occasional
// patches. The maintainer's call (2026-08-29) was to fail loudly rather than
// let a world quietly lose a third of a kind's range.
//
// The risk scales with how rare the terrain is, so it is far milder for forest
// (~27% of land) than for mud (~9%) — but the check is the same, because
// "which biome is rare enough to worry about" is not something to encode.
//
// An ERROR, not a panic, and that is deliberate. mustValidateContent panics
// because a bad content table is a defect that must never reach a running
// process; an unlucky WORLD_SEED is operator input, which this repo already
// treats as a clean error (config.ErrNonPositiveRadius). Panicking here would
// also take down the balance harness, which builds worlds across many seeds.
//
// The candidate set mirrors spawnCandidatesByRingLocked exactly — walkable
// terrain minus the sanctuary — rather than approximating it. A guard that
// checks a different set from the one the spawner draws from is a guard that
// can pass while the thing it guards fails.
func (w *World) ValidateSpawnTerrainCoverage() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// terrain -> the rings it appears in, among hexes the spawner can use.
	ringsWith := make(map[protocol.Terrain]map[int]bool)

	for _, t := range w.worldMap.Tiles {
		if !w.walkableLocked(t.Hex) || w.tooCloseToSanctuaryLocked(t.Hex) {
			continue
		}

		if ringsWith[t.Terrain] == nil {
			ringsWith[t.Terrain] = make(map[int]bool, protocol.RingCount)
		}

		ringsWith[t.Terrain][ringOf(t.Hex, w.radius)] = true
	}

	for _, def := range monsterDefs {
		if def.spawnTerrain == "" {
			continue
		}

		for _, r := range def.rings {
			if !ringsWith[def.spawnTerrain][r] {
				return fmt.Errorf("%w: kind %q spawns only on %q and ring %d has none at world seed %d (radius %d) — "+
					"choose a different WORLD_SEED", ErrRingLacksSpawnTerrain, def.id, def.spawnTerrain, r, w.worldSeed, w.radius)
			}
		}
	}

	return nil
}

// tooCloseToMonsterLocked reports whether h is occupied by, or within
// CombatRadius of, any living monster — spawning a player there would either
// land them ON a monster or form an instant, faction-blind combat bubble the
// moment they appear (both observed live, #36). Callers hold w.mu.
func (w *World) tooCloseToMonsterLocked(h protocol.Hex) bool {
	for _, e := range w.entities {
		if e.kind == protocol.EntityMonster && e.hp > 0 && HexDistance(h, e.hex) <= protocol.CombatRadius {
			return true
		}
	}

	return false
}

// occupiedByMonsterLocked reports whether h is directly on a living
// monster's hex — the distance-0 case tooCloseToMonsterLocked also covers,
// split out because spawnHexLocked's fallback ladder relaxes the "within
// CombatRadius" preference (a crowded clearing may leave no hex outside it)
// before it EVER relaxes "not literally on top of one": a monster co-located
// with its own target pathfinds itself-to-itself (empty path) and never
// attacks (thinkMonstersLocked's co-location dormancy), so landing a spawn
// there doesn't just risk an instant bubble — it can silently stall combat
// forever. Callers hold w.mu.
func (w *World) occupiedByMonsterLocked(h protocol.Hex) bool {
	for _, e := range w.entities {
		if e.kind == protocol.EntityMonster && e.hp > 0 && e.hex == h {
			return true
		}
	}

	return false
}

// tooCloseToPlayerLocked mirrors tooCloseToMonsterLocked for monster
// placement: h must not be occupied by, or within CombatRadius of, any living
// player, so a spawned monster can't stall a run by landing on top of (or
// instantly bubbling with) someone (#36, the task-6 testing mid-run stall).
// Callers hold w.mu.
func (w *World) tooCloseToPlayerLocked(h protocol.Hex) bool {
	for _, e := range w.entities {
		if e.kind == protocol.EntityPlayer && e.hp > 0 && HexDistance(h, e.hex) <= protocol.CombatRadius {
			return true
		}
	}

	return false
}

// tooCloseToSanctuaryLocked reports whether h is within protocol.SanctuaryRadius
// of the origin — the permanent monster-free zone (milestone 6c, the seed of
// a future trade hub), distinct from tooCloseToPlayerLocked's spawn-moment
// player-proximity guard. Reads no entity state; named -Locked and given a
// receiver for symmetry with the other spawn guards it's always applied
// alongside. Callers hold w.mu.
func (w *World) tooCloseToSanctuaryLocked(h protocol.Hex) bool {
	return HexDistance(protocol.Hex{Q: 0, R: 0}, h) <= w.sanctuaryExclusionLocked()
}

// sanctuaryExclusionLocked is how far from the origin a monster may not spawn,
// clamped to something the world can actually afford.
//
// On a world large enough it is SanctuaryRadius + CombatRadius; see
// sanctuarySpawnExclusion for why that sum and not SanctuaryRadius alone.
//
// The clamp matters because the guard is only a PREFERENCE: if no walkable hex
// satisfies it, spawnCandidatesByRingLocked drops it entirely. On a radius-10
// world an 11-hex exclusion covers the entire map, so the guard would be
// dropped and monsters would spawn ON the origin — strictly worse than the
// narrow protection it replaced. Half the radius keeps a small world's
// protection proportional instead of absent.
func (w *World) sanctuaryExclusionLocked() int {
	return min(sanctuarySpawnExclusion, w.radius/2)
}

// sanctuarySpawnExclusion is how far from the origin a monster may not spawn.
//
// SanctuaryRadius alone is NOT enough, and the arithmetic is the whole bug:
// a player joins anywhere within SanctuaryRadius (5) of the origin, combat
// starts at CombatRadius (6), so a monster sitting at distance 6 — legally
// outside the sanctuary — is one hex from a player who spawned at the
// sanctuary's edge. spawnHexLocked tries to avoid that in its first tier, but
// falls through to a tier that ignores the check when no sanctuary hex is
// clear, which with a realistic MONSTER_COUNT is always.
//
// Measured before the fix, at radius 120 with MONSTER_COUNT=1000: 13 monsters
// inside this radius and 25 of 25 joins landing within CombatRadius, closest
// one hex — on BOTH the graph and the noise generator.
//
// 5 + 6 = 11 is the minimum that guarantees a player spawning anywhere in the
// sanctuary starts out of combat range.
const sanctuarySpawnExclusion = protocol.SanctuaryRadius + protocol.CombatRadius

// spawnCandidatesByRingLocked gathers every walkable candidate hex (the
// safe/unguarded-fallback tiers SpawnMonsters' doc comment describes),
// shuffles each ring's bucket with rng, and returns the per-ring hex
// buckets alongside their initial weights (candidate count — the area
// proxy). Callers hold w.mu.
func (w *World) spawnCandidatesByRingLocked(rng *mrand.Rand) ([][]protocol.Hex, []int) {
	var safe, unguarded []protocol.Hex

	for _, t := range w.worldMap.Tiles {
		if !w.walkableLocked(t.Hex) {
			continue
		}

		unguarded = append(unguarded, t.Hex)

		if !w.tooCloseToPlayerLocked(t.Hex) && !w.tooCloseToSanctuaryLocked(t.Hex) {
			safe = append(safe, t.Hex)
		}
	}

	walkable := safe
	if len(walkable) == 0 {
		walkable = unguarded
	}

	slices.SortFunc(walkable, compareHexQR)

	byRing := make([][]protocol.Hex, protocol.RingCount)

	for _, h := range walkable {
		r := ringOf(h, w.radius)
		byRing[r] = append(byRing[r], h)
	}

	ringWeights := make([]int, protocol.RingCount)

	for r, hexes := range byRing {
		rng.Shuffle(len(hexes), func(i, j int) { hexes[i], hexes[j] = hexes[j], hexes[i] })
		ringWeights[r] = len(hexes)
	}

	return byRing, ringWeights
}

// spawnHexLocked picks a hex for a player join or respawn: a random
// walkable, capacity-available hex anywhere in the sanctuary
// (protocol.SanctuaryRadius of the origin) that is not occupied by, or
// within CombatRadius of, a living monster (tooCloseToMonsterLocked) — so a
// spawn can never land a player ON a monster or form an instant combat
// bubble the moment they appear (both observed live, #36). Random, not the
// old spiral-nearest-to-origin search: players (and respawns) no longer pile
// deterministically onto the same hex. Per Q9, the sanctuary is every join's
// and respawn's shared "home" until beds land as a per-player anchor —
// scattering across the whole sanctuary rather than just the small origin
// clearing is intentional.
//
// Four tiers, each engaged only if the one above yields nothing, so a small
// or crowded map never fails a join outright — but "not literally on top of a
// monster" is relaxed dead last, since that specific case can silently stall
// combat forever (occupiedByMonsterLocked's doc comment), not just risk an
// instant bubble:
//  1. sanctuary hexes clear of monsters entirely (the common case)
//  2. sanctuary hexes not occupied by one, ignoring the CombatRadius
//     preference (a monster-dense sanctuary may leave nothing outside it)
//  3. sanctuary hexes at all, ignoring both monster checks (the sanctuary
//     itself is saturated — every hex in it has a monster standing on it)
//  4. spawnHexSpiralLocked over the WHOLE reachable region, ignoring every
//     guard — the pre-#36 search, kept verbatim as the last resort so "a
//     crowded tiny test map must not break joins" still holds
//
// Callers hold w.mu.
func (w *World) spawnHexLocked() (protocol.Hex, error) {
	origin := protocol.Hex{Q: 0, R: 0}

	var sanctuarySafe, sanctuaryUnoccupied, sanctuaryAny []protocol.Hex

	for h := range w.spawnable {
		if HexDistance(origin, h) > protocol.SanctuaryRadius || w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		sanctuaryAny = append(sanctuaryAny, h)

		if w.occupiedByMonsterLocked(h) {
			continue
		}

		sanctuaryUnoccupied = append(sanctuaryUnoccupied, h)

		if !w.tooCloseToMonsterLocked(h) {
			sanctuarySafe = append(sanctuarySafe, h)
		}
	}

	candidates := sanctuarySafe
	if len(candidates) == 0 {
		candidates = sanctuaryUnoccupied
	}

	if len(candidates) == 0 {
		candidates = sanctuaryAny
	}

	if len(candidates) == 0 {
		return w.spawnHexSpiralLocked()
	}

	slices.SortFunc(candidates, compareHexQR)

	//nolint:gosec // deterministic seeded placement, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), spawnPointStream+uint64(w.nextID)))

	return candidates[rng.IntN(len(candidates))], nil
}

// spawnHexSpiralLocked is the pre-#36 search: the free walkable hex nearest
// the origin, spiraling outward, ignoring the monster guard entirely — the
// tier-4 fallback spawnHexLocked reaches for only when none of the three
// sanctuary tiers above yields a single candidate (an extremely crowded or
// tiny map), so a join never hard-fails just because the sanctuary is
// exhausted.
// Callers hold w.mu.
//
// Faction-blind by design in this fallback path: it can land a player on a
// monster-occupied hex (opposing co-occupancy, a §5 MUST in the rare case it
// is ever reached). It is inert only because a co-located monster's think
// step gets Pathfind(from==to)==∅ and holds (never attacks).
func (w *World) spawnHexSpiralLocked() (protocol.Hex, error) {
	origin := protocol.Hex{Q: 0, R: 0}

	for radius := 0; radius <= w.radius; radius++ {
		for q := -radius; q <= radius; q++ {
			for r := -radius; r <= radius; r++ {
				h := protocol.Hex{Q: q, R: r}
				if HexDistance(origin, h) != radius {
					continue
				}

				// w.spawnable[h] already implies walkable; using it (rather than
				// walkableLocked) keeps spawns off any walkable pocket the origin
				// can't reach.
				if w.spawnable[h] && w.occupancyLocked(h) < protocol.StackCap {
					return h, nil
				}
			}
		}
	}

	return protocol.Hex{}, ErrWorldFull
}
