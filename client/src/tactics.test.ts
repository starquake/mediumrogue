import { describe, expect, it } from "vitest";

import { EntityMonster, EntityPlayer, SkillAimEntity, SkillAimHex, SkillAimSelf } from "./protocol.gen";
import type { TacticsCtx } from "./tactics";
import { skillTargetTiles } from "./tactics";

// tactics.test.ts (#300): the armed-skill target preview.
//
// The tile math is pure, so it is tested here rather than through an e2e —
// and it has to be, because the e2e server grants a fresh join no skill
// points, so no active is ever learnable in a browser test.

// A 5-hex-radius patch of walkable ground around the origin, with nothing
// standing on it. Individual tests add occupants or carve holes.
function ctxWith(over: Partial<TacticsCtx> = {}): TacticsCtx {
  const walkable = new Set<string>();
  for (let q = -6; q <= 6; q++) {
    for (let r = -6; r <= 6; r++) walkable.add(`${q},${r}`);
  }

  return {
    me: { q: 0, r: 0 },
    inCombat: false,
    weapons: [],
    positions: [],
    walkable,
    reach: { moves: [], melees: [] },
    ...over,
  };
}

describe("skillTargetTiles", () => {
  it("previews nothing for a self-cast — it never arms", () => {
    expect(skillTargetTiles(ctxWith(), SkillAimSelf, 0)).toEqual([]);
  });

  it("covers a hex-aimed skill's whole reach and never its own tile", () => {
    const tiles = skillTargetTiles(ctxWith(), SkillAimHex, 3);

    // A hex disc of radius 3 is 37 tiles; the caster's own is excluded.
    expect(tiles).toHaveLength(36);
    expect(tiles.some((t) => t.q === 0 && t.r === 0)).toBe(false);
  });

  it("excludes unwalkable ground from a hex-aimed skill", () => {
    const ctx = ctxWith();
    ctx.walkable.delete("1,0");

    const tiles = skillTargetTiles(ctx, SkillAimHex, 2);

    expect(tiles.some((t) => t.q === 1 && t.r === 0)).toBe(false);
  });

  it("keeps OCCUPIED tiles for a hex-aimed skill", () => {
    // Deliberate: the preview is a superset. A blast wants to land on the
    // occupied hex, and the client cannot tell a blast from a evade — only
    // the aim is on the wire. The server refuses an illegal evade on submit.
    const ctx = ctxWith({ positions: [{ kind: EntityMonster, hex: { q: 1, r: 0 } }] });

    expect(skillTargetTiles(ctx, SkillAimHex, 2).some((t) => t.q === 1 && t.r === 0)).toBe(true);
  });

  it("offers only hostiles in range for an entity-aimed skill", () => {
    const ctx = ctxWith({
      positions: [
        { kind: EntityMonster, hex: { q: 2, r: 0 } }, // in range
        { kind: EntityMonster, hex: { q: 5, r: 0 } }, // too far
        { kind: EntityPlayer, hex: { q: 1, r: 0 } }, // an ally is never a target
      ],
    });

    expect(skillTargetTiles(ctx, SkillAimEntity, 3)).toEqual([{ q: 2, r: 0 }]);
  });

  it("previews nothing before the first bundle places me", () => {
    expect(skillTargetTiles(ctxWith({ me: null }), SkillAimHex, 3)).toEqual([]);
  });
});
