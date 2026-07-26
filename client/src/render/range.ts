import { Container, Graphics } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { drawHexTile, HEX_SIZE } from "./hex";

const MOVE_TILE_COLOR = 0x8fd0ff; // matches my dot / the destination ring
const MELEE_TILE_COLOR = 0xd6544f; // matches hostiles / the attack flash

/**
 * The tactical combat overlay: while my entity is in a combat bubble, the
 * hexes I can act on are tinted — blue for open moves, strong red for
 * melee attacks (a hostile stands adjacent; stepping in swings), and a
 * lighter red wash for my equipped ranged weapon's reach (clicking there
 * shoots when an enemy is on the hex — or anywhere in it, for AoE). Empty
 * outside combat: in WeGo exploration, click-anywhere pathing stays the
 * right interaction, so the overlay would only be noise. main.ts computes
 * the three sets (it owns walkability, occupancy, and the equipped weapon);
 * this layer only draws them.
 */
export class MoveRangeLayer {
  readonly container = new Container();
  private readonly gfx = new Graphics();

  constructor() {
    this.container.addChild(this.gfx);
  }

  update(moves: Hex[], melees: Hex[], ranged: Hex[]): void {
    this.gfx.clear();

    // Lightest first: the ranged wash sits under the move/melee tints where
    // they overlap conceptually (they never overlap in the lists — main.ts
    // excludes move/melee tiles from ranged — but draw order keeps it honest).
    for (const h of ranged) {
      this.tile(h, MELEE_TILE_COLOR, 0.08, 0.3);
    }

    for (const h of moves) {
      this.tile(h, MOVE_TILE_COLOR, 0.16, 0.6);
    }

    for (const h of melees) {
      this.tile(h, MELEE_TILE_COLOR, 0.2, 0.75);
    }
  }

  private tile(h: Hex, color: number, fillAlpha: number, strokeAlpha: number): void {
    drawHexTile(this.gfx, h, { size: HEX_SIZE - 2, color, fillAlpha, strokeWidth: 1.5, strokeAlpha });
  }
}

// The action bar's own arming green (#action-bar .aslot.arming, index.html).
// Deliberately NOT the move-tile blue: an armed skill and a walkable tile are
// different questions, and the bar already turns this colour when you arm one —
// so the tiles it enables light up in the same colour that lit up the button.
const SKILL_TILE_COLOR = 0x7cb342;

/**
 * The armed-skill target overlay (#300): while an active is armed, every tile
 * it can be aimed at is tinted, so pressing Blink shows its reach instead of
 * leaving the player to guess and eat a rejection.
 *
 * Separate from MoveRangeLayer rather than a fourth set on it, for two
 * reasons: that layer is empty outside combat by design (click-anywhere
 * pathing makes it noise out there) while an active can be used anywhere, and
 * its question is "where can I move", not "where can this skill go".
 *
 * The set is a SUPERSET (tactics.ts's skillTargetTiles) — it models neither
 * line of sight nor occupancy — so this shows where a skill REACHES, not a
 * promise that every tile will be accepted.
 */
export class SkillRangeLayer {
  readonly container = new Container();
  private readonly gfx = new Graphics();

  constructor() {
    this.container.addChild(this.gfx);
  }

  update(tiles: Hex[]): void {
    this.gfx.clear();

    for (const h of tiles) {
      drawHexTile(this.gfx, h, {
        size: HEX_SIZE - 3,
        color: SKILL_TILE_COLOR,
        fillAlpha: 0.14,
        strokeWidth: 1.5,
        strokeAlpha: 0.55,
      });
    }
  }
}
