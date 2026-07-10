import { Container, Graphics } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexCorners, hexToPixel, HEX_SIZE } from "./hex";

const MOVE_TILE_COLOR = 0x8fd0ff; // matches my dot / the destination ring
const BUMP_TILE_COLOR = 0xd6544f; // matches hostiles / the attack flash

/**
 * The tactical combat overlay: while my entity is in a combat bubble, the
 * hexes I can act on are tinted — blue for open moves, strong red for
 * bump-attacks (a hostile stands adjacent; stepping in swings), and a
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

  update(moves: Hex[], bumps: Hex[], ranged: Hex[]): void {
    this.gfx.clear();

    // Lightest first: the ranged wash sits under the move/bump tints where
    // they overlap conceptually (they never overlap in the lists — main.ts
    // excludes move/bump tiles from ranged — but draw order keeps it honest).
    for (const h of ranged) {
      this.tile(h, BUMP_TILE_COLOR, 0.08, 0.3);
    }

    for (const h of moves) {
      this.tile(h, MOVE_TILE_COLOR, 0.16, 0.6);
    }

    for (const h of bumps) {
      this.tile(h, BUMP_TILE_COLOR, 0.2, 0.75);
    }
  }

  private tile(h: Hex, color: number, fillAlpha: number, strokeAlpha: number): void {
    const corners = hexCorners(hexToPixel(h), HEX_SIZE - 2);
    this.gfx
      .poly(corners)
      .fill({ color, alpha: fillAlpha })
      .stroke({ width: 1.5, color, alpha: strokeAlpha });
  }
}
